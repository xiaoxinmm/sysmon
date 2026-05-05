package traffic

import (
	"sync"
	"time"
)

// Aggregator is a memory aggregator for traffic statistics
type Aggregator struct {
	mu       sync.RWMutex
	portData map[TrafficKey]*TrafficStats
	hostData map[HostTrafficKey]*TrafficStats
	Pools    *sync.Pool // exported for capture
}

// NewAggregator creates a new memory aggregator
func NewAggregator() *Aggregator {
	return &Aggregator{
		portData: make(map[TrafficKey]*TrafficStats),
		hostData: make(map[HostTrafficKey]*TrafficStats),
		Pools: &sync.Pool{
			New: func() interface{} {
				return make([]byte, 65536)
			},
		},
	}
}

// Update updates port-level traffic statistics
func (a *Aggregator) Update(key TrafficKey, bytes uint64) {
	a.mu.Lock()
	defer a.mu.Unlock()

	stats, exists := a.portData[key]
	if !exists {
		stats = &TrafficStats{
			LastUpdate: time.Now(),
		}
		a.portData[key] = stats
	}

	stats.Bytes += bytes
	stats.Packets++

	now := time.Now()
	elapsed := now.Sub(stats.LastUpdate).Seconds()
	if elapsed > 0 {
		currentRate := float64(bytes) / elapsed
		if currentRate > stats.PeakRate {
			stats.PeakRate = currentRate
		}
	}
	stats.LastUpdate = now
}

// UpdateHost updates host-level traffic statistics
func (a *Aggregator) UpdateHost(key HostTrafficKey, bytes uint64) {
	a.mu.Lock()
	defer a.mu.Unlock()

	stats, exists := a.hostData[key]
	if !exists {
		stats = &TrafficStats{
			LastUpdate: time.Now(),
		}
		a.hostData[key] = stats
	}

	stats.Bytes += bytes
	stats.Packets++

	now := time.Now()
	elapsed := now.Sub(stats.LastUpdate).Seconds()
	if elapsed > 0 {
		currentRate := float64(bytes) / elapsed
		if currentRate > stats.PeakRate {
			stats.PeakRate = currentRate
		}
	}
	stats.LastUpdate = now
}

// GetSnapshot gets current snapshot and resets port counters
func (a *Aggregator) GetSnapshot() []TrafficSnapshot {
	a.mu.Lock()
	defer a.mu.Unlock()

	now := time.Now()
	snapshots := make([]TrafficSnapshot, 0, len(a.portData))

	for key, stats := range a.portData {
		elapsed := now.Sub(stats.LastUpdate).Seconds()
		if elapsed == 0 {
			elapsed = 60
		}
		avgRate := float64(stats.Bytes) / elapsed

		snapshots = append(snapshots, TrafficSnapshot{
			Timestamp: now,
			Port:      key.Port,
			SourceIP:  key.SourceIP,
			Direction: key.Direction,
			Bytes:     stats.Bytes,
			Packets:   stats.Packets,
			Rate:      avgRate,
		})
	}

	a.portData = make(map[TrafficKey]*TrafficStats)
	return snapshots
}

// GetHostSnapshot gets current host snapshot and resets counters
func (a *Aggregator) GetHostSnapshot() []HostTrafficSnapshot {
	a.mu.Lock()
	defer a.mu.Unlock()

	now := time.Now()
	snapshots := make([]HostTrafficSnapshot, 0, len(a.hostData))

	for key, stats := range a.hostData {
		elapsed := now.Sub(stats.LastUpdate).Seconds()
		if elapsed == 0 {
			elapsed = 60
		}
		avgRate := float64(stats.Bytes) / elapsed

		snapshots = append(snapshots, HostTrafficSnapshot{
			Timestamp: now,
			HostIP:    key.HostIP,
			RemoteIP:  key.RemoteIP,
			Direction: key.Direction,
			Bytes:     stats.Bytes,
			Packets:   stats.Packets,
			Rate:      avgRate,
		})
	}

	a.hostData = make(map[HostTrafficKey]*TrafficStats)
	return snapshots
}

// GetRealTimeStats returns real-time port statistics without resetting
func (a *Aggregator) GetRealTimeStats() map[uint16]map[string]interface{} {
	a.mu.RLock()
	defer a.mu.RUnlock()

	result := make(map[uint16]map[string]interface{})

	for key, stats := range a.portData {
		if _, exists := result[key.Port]; !exists {
			result[key.Port] = map[string]interface{}{
				"port":          key.Port,
				"total_bytes":   uint64(0),
				"total_packets": uint64(0),
				"peak_rate":     float64(0),
				"sources":       make([]map[string]interface{}, 0),
			}
		}

		portData := result[key.Port]
		portData["total_bytes"] = portData["total_bytes"].(uint64) + stats.Bytes
		portData["total_packets"] = portData["total_packets"].(uint64) + stats.Packets

		if stats.PeakRate > portData["peak_rate"].(float64) {
			portData["peak_rate"] = stats.PeakRate
		}

		elapsed := time.Since(stats.LastUpdate).Seconds()
		if elapsed == 0 {
			elapsed = 1
		}
		currentRate := float64(stats.Bytes) / elapsed

		sources := portData["sources"].([]map[string]interface{})
		sources = append(sources, map[string]interface{}{
			"ip":        key.SourceIP,
			"direction": key.Direction,
			"bytes":     stats.Bytes,
			"packets":   stats.Packets,
			"rate":      currentRate,
		})
		portData["sources"] = sources
	}

	return result
}

// GetRealTimeHostStats returns real-time host statistics without resetting
func (a *Aggregator) GetRealTimeHostStats() map[string]map[string]interface{} {
	a.mu.RLock()
	defer a.mu.RUnlock()

	result := make(map[string]map[string]interface{})

	for key, stats := range a.hostData {
		if _, exists := result[key.HostIP]; !exists {
			result[key.HostIP] = map[string]interface{}{
				"host_ip":       key.HostIP,
				"total_bytes":   uint64(0),
				"total_packets": uint64(0),
				"peak_rate":     float64(0),
				"remotes":       make([]map[string]interface{}, 0),
			}
		}

		hostData := result[key.HostIP]
		hostData["total_bytes"] = hostData["total_bytes"].(uint64) + stats.Bytes
		hostData["total_packets"] = hostData["total_packets"].(uint64) + stats.Packets

		if stats.PeakRate > hostData["peak_rate"].(float64) {
			hostData["peak_rate"] = stats.PeakRate
		}

		elapsed := time.Since(stats.LastUpdate).Seconds()
		if elapsed == 0 {
			elapsed = 1
		}
		currentRate := float64(stats.Bytes) / elapsed

		remotes := hostData["remotes"].([]map[string]interface{})
		remotes = append(remotes, map[string]interface{}{
			"ip":        key.RemoteIP,
			"direction": key.Direction,
			"bytes":     stats.Bytes,
			"packets":   stats.Packets,
			"rate":      currentRate,
		})
		hostData["remotes"] = remotes
	}

	return result
}
