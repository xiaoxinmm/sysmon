package traffic

import (
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
)

// localIPCache and related variables for IP detection
var (
	localIPCache     map[string]bool
	localIPCacheMu   sync.RWMutex
	localIPCacheTime time.Time
	localIPCacheTTL  = 5 * time.Minute
)

// StartPacketCapture starts capturing packets on the given interface
func StartPacketCapture(iface string, agg *Aggregator, listenPorts *map[uint16]bool, portsMu *sync.RWMutex) error {
	handle, err := pcap.OpenLive(iface, 65536, true, pcap.BlockForever)
	if err != nil {
		return err
	}
	defer handle.Close()

	slog.Info("started packet capture", "interface", iface)

	// Log the initial listening ports for debugging
	portsMu.RLock()
	slog.Debug("initial listenPorts for capture", "ports", *listenPorts, "count", len(*listenPorts))
	portsMu.RUnlock()

	if err := handle.SetBPFFilter("tcp or udp"); err != nil {
		return err
	}

	packetSource := gopacket.NewPacketSource(handle, handle.LinkType())

	for packet := range packetSource.Packets() {
		processPacket(packet, agg, listenPorts, portsMu)
	}

	return nil
}

func processPacket(packet gopacket.Packet, agg *Aggregator, listenPorts *map[uint16]bool, portsMu *sync.RWMutex) {
	networkLayer := packet.NetworkLayer()
	if networkLayer == nil {
		return
	}

	transportLayer := packet.TransportLayer()
	if transportLayer == nil {
		return
	}

	var srcIP, dstIP string
	var srcPort, dstPort uint16

	if ipv4Layer := packet.Layer(layers.LayerTypeIPv4); ipv4Layer != nil {
		ipv4, _ := ipv4Layer.(*layers.IPv4)
		srcIP = ipv4.SrcIP.String()
		dstIP = ipv4.DstIP.String()
	} else if ipv6Layer := packet.Layer(layers.LayerTypeIPv6); ipv6Layer != nil {
		ipv6, _ := ipv6Layer.(*layers.IPv6)
		srcIP = ipv6.SrcIP.String()
		dstIP = ipv6.DstIP.String()
	} else {
		return
	}

	switch layer := transportLayer.(type) {
	case *layers.TCP:
		srcPort = uint16(layer.SrcPort)
		dstPort = uint16(layer.DstPort)
	case *layers.UDP:
		srcPort = uint16(layer.SrcPort)
		dstPort = uint16(layer.DstPort)
	default:
		return
	}

	packetSize := uint64(len(packet.Data()))

	localIPs := getAllLocalIPsCached()

	isLocalSrc := localIPs[srcIP]
	isLocalDst := localIPs[dstIP]

	// Read the current listenPorts map under read lock
	portsMu.RLock()
	currentPorts := *listenPorts
	portsMu.RUnlock()

	// Port-level traffic tracking
	if currentPorts[dstPort] && isLocalDst {
		key := TrafficKey{
			Port:      dstPort,
			SourceIP:  srcIP,
			Direction: "inbound",
		}
		agg.Update(key, packetSize)
		slog.Debug("port traffic inbound", "port", dstPort, "src", srcIP, "size", packetSize)
	}

	if currentPorts[srcPort] && isLocalSrc {
		key := TrafficKey{
			Port:      srcPort,
			SourceIP:  dstIP,
			Direction: "outbound",
		}
		agg.Update(key, packetSize)
		slog.Debug("port traffic outbound", "port", srcPort, "dst", dstIP, "size", packetSize)
	}

	// Host-level traffic tracking
	if isLocalSrc || isLocalDst {
		var localIP, remoteIP string
		var direction string

		if isLocalSrc {
			localIP = srcIP
			remoteIP = dstIP
			direction = "outbound"
		} else {
			localIP = dstIP
			remoteIP = srcIP
			direction = "inbound"
		}

		hostKey := HostTrafficKey{
			HostIP:    localIP,
			RemoteIP:  remoteIP,
			Direction: direction,
		}
		agg.UpdateHost(hostKey, packetSize)
	}
}

func getAllLocalIPsCached() map[string]bool {
	localIPCacheMu.RLock()
	if time.Since(localIPCacheTime) < localIPCacheTTL && localIPCache != nil {
		defer localIPCacheMu.RUnlock()
		return localIPCache
	}
	localIPCacheMu.RUnlock()

	localIPCacheMu.Lock()
	defer localIPCacheMu.Unlock()

	if time.Since(localIPCacheTime) < localIPCacheTTL && localIPCache != nil {
		return localIPCache
	}

	ips, err := getAllLocalIPs()
	if err != nil {
		slog.Warn("failed to refresh local IP cache", "error", err)
		if localIPCache != nil {
			return localIPCache
		}
		return make(map[string]bool)
	}

	localIPCache = ips
	localIPCacheTime = time.Now()
	slog.Debug("refreshed local IP cache", "count", len(ips))

	return localIPCache
}

func getAllLocalIPs() (map[string]bool, error) {
	localIPs := make(map[string]bool)

	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			if ip == nil || ip.IsLoopback() {
				continue
			}

			if ip.To4() != nil {
				localIPs[ip.String()] = true
			}
		}
	}

	return localIPs, nil
}
