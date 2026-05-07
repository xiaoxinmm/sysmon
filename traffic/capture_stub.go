//go:build !linux

package traffic

import (
	"fmt"
	"sync"
)

// StartPacketCapture is a stub on non-Linux platforms.
// Packet capture is only supported on Linux.
func StartPacketCapture(iface string, agg *Aggregator, listenPorts *map[uint16]bool, portsMu *sync.RWMutex) error {
	return fmt.Errorf("packet capture is not supported on this platform (linux only)")
}

// GetListeningPorts is a stub on non-Linux platforms.
// It reads /proc/net/tcp which is Linux-specific.
func GetListeningPorts() (map[uint16]bool, error) {
	return make(map[uint16]bool), nil
}

// GetLocalIP returns the first non-loopback IPv4 address
func GetLocalIP() string {
	return "127.0.0.1"
}

// GetDefaultInterface is a stub on non-Linux platforms.
func GetDefaultInterface() (string, error) {
	return "", fmt.Errorf("default interface detection is not supported on this platform (linux only)")
}
