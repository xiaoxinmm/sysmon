//go:build linux

package traffic

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

// GetListeningPorts returns all ports in LISTEN state from /proc/net/tcp{,6} and udp{,6}
func GetListeningPorts() (map[uint16]bool, error) {
	ports := make(map[uint16]bool)

	for _, file := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}

		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			if i == 0 || line == "" {
				continue
			}

			fields := strings.Fields(line)
			if len(fields) < 4 {
				continue
			}

			if fields[3] != "0A" { // 0A = LISTEN
				continue
			}

			localAddr := fields[1]
			// Handle both IPv4 (AA.BB.CC.DD:PORT) and IPv6 (....:PORT) formats
			lastColon := strings.LastIndex(localAddr, ":")
			if lastColon < 0 {
				continue
			}
			portHex := localAddr[lastColon+1:]

			portInt, err := strconv.ParseInt(portHex, 16, 32)
			if err != nil {
				continue
			}

			ports[uint16(portInt)] = true
		}
	}

	// Also process UDP
	for _, file := range []string{"/proc/net/udp", "/proc/net/udp6"} {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}

		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			if i == 0 || line == "" {
				continue
			}

			fields := strings.Fields(line)
			if len(fields) < 4 {
				continue
			}

			if fields[3] != "07" { // 07 = UDP LISTEN
				continue
			}

			localAddr := fields[1]
			// Handle both IPv4 and IPv6 formats
			lastColon := strings.LastIndex(localAddr, ":")
			if lastColon < 0 {
				continue
			}
			portHex := localAddr[lastColon+1:]

			portInt, err := strconv.ParseInt(portHex, 16, 32)
			if err != nil {
				continue
			}

			ports[uint16(portInt)] = true
		}
	}

	return ports, nil
}

// GetLocalIP returns the first non-loopback IPv4 address
func GetLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}

	return "127.0.0.1"
}

// GetDefaultInterface returns the first non-loopback network interface with an IPv4 address
func GetDefaultInterface() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.To4() != nil {
				return iface.Name, nil
			}
		}
	}

	return "", fmt.Errorf("no suitable network interface found")
}
