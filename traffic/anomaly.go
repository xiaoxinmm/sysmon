package traffic

import (
	"math"
	"sync"
	"time"
)

// Anomaly represents a detected traffic anomaly
type Anomaly struct {
	Timestamp time.Time `json:"timestamp"`
	Port      uint16    `json:"port"`
	SourceIP  string    `json:"source_ip"`
	Direction string    `json:"direction"`
	Value     float64   `json:"value"`
	Mean      float64   `json:"mean"`
	StdDev    float64   `json:"std_dev"`
	Threshold float64   `json:"threshold"`
	Severity  string    `json:"severity"`
}

// AnomalyDetector detects traffic anomalies using sliding window and 3-sigma rule
type AnomalyDetector struct {
	mu         sync.RWMutex
	windowSize int
	windows    map[string]*Window
	anomalies  []Anomaly
	maxHistory int
}

// Window stores historical data for anomaly detection
type Window struct {
	values []float64
	index  int
	full   bool
}

// NewAnomalyDetector creates a new anomaly detector
func NewAnomalyDetector(windowSize, maxHistory int) *AnomalyDetector {
	if windowSize <= 0 {
		windowSize = 10
	}
	if maxHistory <= 0 {
		maxHistory = 100
	}
	return &AnomalyDetector{
		windowSize: windowSize,
		windows:    make(map[string]*Window),
		anomalies:  make([]Anomaly, 0),
		maxHistory: maxHistory,
	}
}

// DetectPortAnomalies detects anomalies in port traffic snapshots
func (d *AnomalyDetector) DetectPortAnomalies(snapshots []TrafficSnapshot) []Anomaly {
	d.mu.Lock()
	defer d.mu.Unlock()

	detected := make([]Anomaly, 0)

	for _, snap := range snapshots {
		key := d.makeKey(snap.Port, snap.SourceIP, snap.Direction)
		window := d.getOrCreateWindow(key)

		if window.full {
			mean, stdDev := d.calculateStats(window)
			threshold := mean + 3*stdDev

			if snap.Rate > threshold && stdDev > 0 {
				severity := "medium"
				if snap.Rate > mean+5*stdDev {
					severity = "high"
				} else if snap.Rate > mean+4*stdDev {
					severity = "medium"
				} else {
					severity = "low"
				}

				anomaly := Anomaly{
					Timestamp: snap.Timestamp,
					Port:      snap.Port,
					SourceIP:  snap.SourceIP,
					Direction: snap.Direction,
					Value:     snap.Rate,
					Mean:      mean,
					StdDev:    stdDev,
					Threshold: threshold,
					Severity:  severity,
				}
				detected = append(detected, anomaly)
				d.addAnomaly(anomaly)
			}
		}

		d.addValue(window, snap.Rate)
	}

	return detected
}

// DetectHostAnomalies detects anomalies in host traffic snapshots
func (d *AnomalyDetector) DetectHostAnomalies(snapshots []HostTrafficSnapshot) []Anomaly {
	d.mu.Lock()
	defer d.mu.Unlock()

	detected := make([]Anomaly, 0)

	for _, snap := range snapshots {
		key := d.makeHostKey(snap.HostIP, snap.RemoteIP, snap.Direction)
		window := d.getOrCreateWindow(key)

		if window.full {
			mean, stdDev := d.calculateStats(window)
			threshold := mean + 3*stdDev

			if snap.Rate > threshold && stdDev > 0 {
				severity := "medium"
				if snap.Rate > mean+5*stdDev {
					severity = "high"
				} else if snap.Rate > mean+4*stdDev {
					severity = "medium"
				} else {
					severity = "low"
				}

				anomaly := Anomaly{
					Timestamp: snap.Timestamp,
					Port:      0,
					SourceIP:  snap.HostIP + " -> " + snap.RemoteIP,
					Direction: snap.Direction,
					Value:     snap.Rate,
					Mean:      mean,
					StdDev:    stdDev,
					Threshold: threshold,
					Severity:  severity,
				}
				detected = append(detected, anomaly)
				d.addAnomaly(anomaly)
			}
		}

		d.addValue(window, snap.Rate)
	}

	return detected
}

// GetRecentAnomalies returns recent anomalies
func (d *AnomalyDetector) GetRecentAnomalies(limit int) []Anomaly {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if limit <= 0 || limit > len(d.anomalies) {
		limit = len(d.anomalies)
	}

	start := len(d.anomalies) - limit
	if start < 0 {
		start = 0
	}

	result := make([]Anomaly, limit)
	copy(result, d.anomalies[start:])
	return result
}

// makeKey creates a unique key for port traffic
func (d *AnomalyDetector) makeKey(port uint16, sourceIP, direction string) string {
	return string(rune(port)) + ":" + sourceIP + ":" + direction
}

// makeHostKey creates a unique key for host traffic
func (d *AnomalyDetector) makeHostKey(hostIP, remoteIP, direction string) string {
	return hostIP + ":" + remoteIP + ":" + direction
}

// getOrCreateWindow gets or creates a window for the key
func (d *AnomalyDetector) getOrCreateWindow(key string) *Window {
	window, exists := d.windows[key]
	if !exists {
		window = &Window{
			values: make([]float64, d.windowSize),
			index:  0,
			full:   false,
		}
		d.windows[key] = window
	}
	return window
}

// addValue adds a value to the window
func (d *AnomalyDetector) addValue(window *Window, value float64) {
	window.values[window.index] = value
	window.index++
	if window.index >= d.windowSize {
		window.index = 0
		window.full = true
	}
}

// calculateStats calculates mean and standard deviation
func (d *AnomalyDetector) calculateStats(window *Window) (mean, stdDev float64) {
	sum := 0.0
	count := d.windowSize
	if !window.full {
		count = window.index
	}

	for i := 0; i < count; i++ {
		sum += window.values[i]
	}
	mean = sum / float64(count)

	variance := 0.0
	for i := 0; i < count; i++ {
		diff := window.values[i] - mean
		variance += diff * diff
	}
	variance /= float64(count)
	stdDev = math.Sqrt(variance)

	return mean, stdDev
}

// addAnomaly adds an anomaly to the history
func (d *AnomalyDetector) addAnomaly(anomaly Anomaly) {
	d.anomalies = append(d.anomalies, anomaly)
	if len(d.anomalies) > d.maxHistory {
		d.anomalies = d.anomalies[1:]
	}
}
