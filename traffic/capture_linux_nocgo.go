//go:build linux && !cgo

package traffic

import (
	"fmt"
	"sync"
)

// StartPacketCapture is a stub when CGO/libpcap is not available.
// Packet capture requires CGO_ENABLED=1 and libpcap-dev.
func StartPacketCapture(iface string, agg *Aggregator, listenPorts *map[uint16]bool, portsMu *sync.RWMutex) error {
	return fmt.Errorf("packet capture requires CGO_ENABLED=1 and libpcap-dev; rebuild with CGO_ENABLED=1 to enable traffic monitoring")
}
