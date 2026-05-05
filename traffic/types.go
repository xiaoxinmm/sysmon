package traffic

import "time"

// TrafficKey uniquely identifies a traffic record
type TrafficKey struct {
	Port      uint16
	SourceIP  string
	Direction string // "inbound" or "outbound"
}

// TrafficStats stores traffic statistics
type TrafficStats struct {
	Bytes      uint64
	Packets    uint64
	LastUpdate time.Time
	PeakRate   float64 // Bytes/s
}

// TrafficSnapshot is a per-minute snapshot
type TrafficSnapshot struct {
	Timestamp time.Time
	Port      uint16
	SourceIP  string
	Direction string
	Bytes     uint64
	Packets   uint64
	Rate      float64
}

// HostTrafficKey uniquely identifies a host traffic record
type HostTrafficKey struct {
	HostIP    string
	RemoteIP  string
	Direction string // "inbound" or "outbound"
}

// HostTrafficSnapshot stores host traffic snapshot data
type HostTrafficSnapshot struct {
	Timestamp time.Time
	HostIP    string
	RemoteIP  string
	Direction string
	Bytes     uint64
	Packets   uint64
	Rate      float64
}
