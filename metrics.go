package main

import (
	"log/slog"
	"time"

	"sysmon/monitor"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Snapshot holds a full system snapshot for WebSocket broadcast
type Snapshot struct {
	Timestamp int64                 `json:"timestamp"`
	System    monitor.SystemInfo    `json:"system"`
	CPU       monitor.CPUInfo       `json:"cpu"`
	Memory    monitor.MemInfo       `json:"memory"`
	Disks     []monitor.DiskInfo    `json:"disks"`
	Network   []monitor.NetInfo     `json:"network"`
	Load      monitor.LoadInfo      `json:"load"`
	Processes []monitor.ProcessInfo `json:"processes"`
}

// Collect gathers a full system snapshot
func Collect(maxProcesses int) Snapshot {
	return Snapshot{
		Timestamp: timeNowMillis(),
		System:    monitor.GetSystemInfo(),
		CPU:       monitor.GetCPUInfo(),
		Memory:    monitor.GetMemInfo(),
		Disks:     monitor.GetDiskInfo(),
		Network:   monitor.GetNetInfo(),
		Load:      monitor.GetLoadInfo(),
		Processes: monitor.GetProcesses(maxProcesses),
	}
}

func timeNowMillis() int64 {
	return time.Now().UnixMilli()
}

// WsMessage is the JSON protocol for WebSocket messages
type WsMessage struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}

// Custom Prometheus metrics for sysmon
var (
	cpuUsageGauge = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "sysmon",
		Name:      "cpu_usage_percent",
		Help:      "Average CPU usage percentage.",
	})

	memUsageGauge = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "sysmon",
		Name:      "memory_usage_bytes",
		Help:      "Memory usage in bytes.",
	})

	diskUsageGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "sysmon",
		Name:      "disk_usage_bytes",
		Help:      "Disk usage in bytes by device and mountpoint.",
	}, []string{"device", "mountpoint"})

	dockerContainersGauge = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "sysmon",
		Name:      "docker_containers_total",
		Help:      "Total number of Docker containers.",
	})

	wsConnectionsGauge = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "sysmon",
		Name:      "websocket_connections",
		Help:      "Current number of active WebSocket connections.",
	})
)

func init() {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector())
	reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	slog.Debug("prometheus: registered Go and process collectors")
}

// UpdateMetricsFromSnapshot updates CPU, memory, and disk Prometheus gauges
func UpdateMetricsFromSnapshot(snap Snapshot) {
	cpuUsageGauge.Set(snap.CPU.AvgUsage)
	memUsageGauge.Set(float64(snap.Memory.Used))

	diskUsageGauge.Reset()
	for _, d := range snap.Disks {
		diskUsageGauge.WithLabelValues(d.Device, d.Mountpoint).Set(float64(d.Used))
	}
}

// UpdateDockerMetric updates the docker container count gauge
func UpdateDockerMetric(count int) {
	dockerContainersGauge.Set(float64(count))
}

// UpdateWSConnections updates the WebSocket connections gauge
func UpdateWSConnections(count int) {
	wsConnectionsGauge.Set(float64(count))
}
