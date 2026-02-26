// Copyright (C) 2025 Russell Li (xiaoxinmm)
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.

package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
	psnet "github.com/shirou/gopsutil/v3/net"
	"github.com/shirou/gopsutil/v3/process"
)

type SystemInfo struct {
	Hostname string `json:"hostname"`
	OS       string `json:"os"`
	Platform string `json:"platform"`
	Kernel   string `json:"kernel"`
	Arch     string `json:"arch"`
	Uptime   uint64 `json:"uptime"`
	GoVer    string `json:"goVersion"`
}

type CPUInfo struct {
	Model   string    `json:"model"`
	Cores   int       `json:"cores"`
	Threads int       `json:"threads"`
	Usage   []float64 `json:"usage"`
	AvgUsage float64  `json:"avgUsage"`
}

type MemInfo struct {
	Total       uint64  `json:"total"`
	Used        uint64  `json:"used"`
	Available   uint64  `json:"available"`
	UsedPercent float64 `json:"usedPercent"`
	SwapTotal   uint64  `json:"swapTotal"`
	SwapUsed    uint64  `json:"swapUsed"`
	SwapPercent float64 `json:"swapPercent"`
}

type DiskInfo struct {
	Device     string  `json:"device"`
	Mountpoint string  `json:"mountpoint"`
	Fstype     string  `json:"fstype"`
	Total      uint64  `json:"total"`
	Used       uint64  `json:"used"`
	Free       uint64  `json:"free"`
	UsedPercent float64 `json:"usedPercent"`
}

type NetInfo struct {
	Name      string  `json:"name"`
	BytesSent uint64  `json:"bytesSent"`
	BytesRecv uint64  `json:"bytesRecv"`
	Addrs     string  `json:"addrs"`
	SendRate  float64 `json:"sendRate"`
	RecvRate  float64 `json:"recvRate"`
}

type LoadInfo struct {
	Load1  float64 `json:"load1"`
	Load5  float64 `json:"load5"`
	Load15 float64 `json:"load15"`
}

type ProcessInfo struct {
	PID    int32   `json:"pid"`
	Name   string  `json:"name"`
	CPU    float64 `json:"cpu"`
	Mem    float32 `json:"mem"`
	Status string  `json:"status"`
}

type HistoryPoint struct {
	Timestamp  int64   `json:"t"`
	CPUAvg     float64 `json:"c"`
	MemPercent float64 `json:"m"`
}

// network rate tracking
var (
	prevNetMu   sync.Mutex
	prevNetData map[string]netSample
	prevNetTime time.Time
)

type netSample struct {
	bytesSent uint64
	bytesRecv uint64
}

func init() {
	prevNetData = make(map[string]netSample)
}

// history ring buffer
var (
	histMu       sync.Mutex
	histBuf      []HistoryPoint
	histCapacity int = 3600
)

func SetHistoryCapacity(n int) {
	histMu.Lock()
	defer histMu.Unlock()
	histCapacity = n
	if len(histBuf) > histCapacity {
		histBuf = histBuf[len(histBuf)-histCapacity:]
	}
}

func RecordHistory(cpuAvg, memPct float64) {
	histMu.Lock()
	defer histMu.Unlock()
	pt := HistoryPoint{
		Timestamp:  time.Now().Unix(),
		CPUAvg:     cpuAvg,
		MemPercent: memPct,
	}
	histBuf = append(histBuf, pt)
	if len(histBuf) > histCapacity {
		histBuf = histBuf[len(histBuf)-histCapacity:]
	}
}

func GetHistory() []HistoryPoint {
	histMu.Lock()
	defer histMu.Unlock()
	cp := make([]HistoryPoint, len(histBuf))
	copy(cp, histBuf)
	return cp
}

func GetSystemInfo() SystemInfo {
	h, _ := host.Info()
	info := SystemInfo{
		Arch:  runtime.GOARCH,
		GoVer: runtime.Version(),
	}
	if h != nil {
		info.Hostname = h.Hostname
		info.OS = h.OS
		info.Platform = fmt.Sprintf("%s %s", h.Platform, h.PlatformVersion)
		info.Kernel = h.KernelVersion
		info.Uptime = h.Uptime
	}
	return info
}

func GetCPUInfo() CPUInfo {
	info := CPUInfo{}
	cpuInfos, err := cpu.Info()
	if err == nil && len(cpuInfos) > 0 {
		info.Model = cpuInfos[0].ModelName
	}
	info.Cores, _ = cpu.Counts(false)
	info.Threads, _ = cpu.Counts(true)

	percents, err := cpu.Percent(0, true)
	if err == nil {
		info.Usage = percents
		var sum float64
		for _, p := range percents {
			sum += p
		}
		if len(percents) > 0 {
			info.AvgUsage = sum / float64(len(percents))
		}
	}
	return info
}

func GetMemInfo() MemInfo {
	info := MemInfo{}
	v, err := mem.VirtualMemory()
	if err == nil && v != nil {
		info.Total = v.Total
		info.Used = v.Used
		info.Available = v.Available
		info.UsedPercent = v.UsedPercent
	}
	s, err := mem.SwapMemory()
	if err == nil && s != nil {
		info.SwapTotal = s.Total
		info.SwapUsed = s.Used
		info.SwapPercent = s.UsedPercent
	}
	return info
}

func GetDiskInfo() []DiskInfo {
	var disks []DiskInfo
	partitions, err := disk.Partitions(false)
	if err != nil {
		return disks
	}
	seen := make(map[string]bool)
	for _, p := range partitions {
		if seen[p.Device] {
			continue
		}
		seen[p.Device] = true
		usage, err := disk.Usage(p.Mountpoint)
		if err != nil || usage == nil {
			continue
		}
		if usage.Total == 0 {
			continue
		}
		disks = append(disks, DiskInfo{
			Device:      p.Device,
			Mountpoint:  p.Mountpoint,
			Fstype:      p.Fstype,
			Total:       usage.Total,
			Used:        usage.Used,
			Free:        usage.Free,
			UsedPercent: usage.UsedPercent,
		})
	}
	return disks
}

func GetNetInfo() []NetInfo {
	var nets []NetInfo
	counters, err := psnet.IOCounters(true)
	if err != nil {
		return nets
	}
	interfaces, _ := psnet.Interfaces()
	addrMap := make(map[string]string)
	if interfaces != nil {
		for _, iface := range interfaces {
			var addrs []string
			for _, a := range iface.Addrs {
				addrs = append(addrs, a.Addr)
			}
			addrMap[iface.Name] = strings.Join(addrs, ", ")
		}
	}

	now := time.Now()
	prevNetMu.Lock()
	defer prevNetMu.Unlock()
	elapsed := now.Sub(prevNetTime).Seconds()
	if elapsed <= 0 {
		elapsed = 1
	}
	for _, c := range counters {
		if c.Name == "lo" {
			continue
		}
		var sendRate, recvRate float64
		if prev, ok := prevNetData[c.Name]; ok && elapsed > 0 {
			sendRate = float64(c.BytesSent-prev.bytesSent) / elapsed
			recvRate = float64(c.BytesRecv-prev.bytesRecv) / elapsed
			if sendRate < 0 {
				sendRate = 0
			}
			if recvRate < 0 {
				recvRate = 0
			}
		}
		nets = append(nets, NetInfo{
			Name:      c.Name,
			BytesSent: c.BytesSent,
			BytesRecv: c.BytesRecv,
			Addrs:     addrMap[c.Name],
			SendRate:  sendRate,
			RecvRate:  recvRate,
		})
		prevNetData[c.Name] = netSample{bytesSent: c.BytesSent, bytesRecv: c.BytesRecv}
	}
	prevNetTime = now

	return nets
}

func GetLoadInfo() LoadInfo {
	info := LoadInfo{}
	l, err := load.Avg()
	if err == nil && l != nil {
		info.Load1 = l.Load1
		info.Load5 = l.Load5
		info.Load15 = l.Load15
	}
	return info
}

type DockerContainer struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Image    string  `json:"image"`
	State    string  `json:"state"`
	Status   string  `json:"status"`
	CPUPct   float64 `json:"cpuPct"`
	MemUsage uint64  `json:"memUsage"`
	MemLimit uint64 `json:"memLimit"`
	Created  string `json:"created"`
}

type dockerAPIContainer struct {
	ID      string   `json:"Id"`
	Names   []string `json:"Names"`
	Image   string   `json:"Image"`
	State   string   `json:"State"`
	Status  string   `json:"Status"`
	Created int64    `json:"Created"`
}

type dockerStatsMemory struct {
	Usage uint64 `json:"usage"`
	Limit uint64 `json:"limit"`
}

type dockerStatsCPU struct {
	TotalUsage  uint64 `json:"total_usage"`
	SystemUsage uint64 `json:"system_cpu_usage"`
}

type dockerStatsResponse struct {
	CPUStats struct {
		CPUUsage    dockerStatsCPU `json:"cpu_usage"`
		SystemUsage uint64         `json:"system_cpu_usage"`
		OnlineCPUs  int            `json:"online_cpus"`
	} `json:"cpu_stats"`
	PreCPUStats struct {
		CPUUsage    dockerStatsCPU `json:"cpu_usage"`
		SystemUsage uint64         `json:"system_cpu_usage"`
	} `json:"precpu_stats"`
	MemoryStats dockerStatsMemory `json:"memory_stats"`
}

var (
	dockerHTTPClient *http.Client
	dockerAvailable  bool
	dockerCheckOnce  sync.Once
)

func initDockerClient() {
	dockerHTTPClient = &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", "/var/run/docker.sock")
			},
		},
		Timeout: 5 * time.Second,
	}
	// quick check
	resp, err := dockerHTTPClient.Get("http://localhost/version")
	if err != nil {
		return
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		dockerAvailable = true
	}
}

func GetDockerContainers() []DockerContainer {
	dockerCheckOnce.Do(initDockerClient)
	if !dockerAvailable {
		return nil
	}

	resp, err := dockerHTTPClient.Get("http://localhost/containers/json?all=1")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	var apiContainers []dockerAPIContainer
	if err := json.Unmarshal(body, &apiContainers); err != nil {
		return nil
	}

	containers := make([]DockerContainer, 0, len(apiContainers))
	for _, c := range apiContainers {
		name := ""
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}
		dc := DockerContainer{
			ID:      c.ID,
			Name:    name,
			Image:   c.Image,
			State:   c.State,
			Status:  c.Status,
			Created: time.Unix(c.Created, 0).Format(time.RFC3339),
		}

		if c.State == "running" {
			statsURL := fmt.Sprintf("http://localhost/containers/%s/stats?stream=false&one-shot=true", c.ID)
			sr, err := dockerHTTPClient.Get(statsURL)
			if err != nil {
				log.Printf("docker: failed to get stats for %s: %v", c.ID[:12], err)
				containers = append(containers, dc)
				continue
			}
			statsBody, _ := io.ReadAll(sr.Body)
			sr.Body.Close()

			var stats dockerStatsResponse
			if json.Unmarshal(statsBody, &stats) == nil {
				cpuDelta := float64(stats.CPUStats.CPUUsage.TotalUsage - stats.PreCPUStats.CPUUsage.TotalUsage)
				sysDelta := float64(stats.CPUStats.SystemUsage - stats.PreCPUStats.SystemUsage)
				if sysDelta > 0 && stats.CPUStats.OnlineCPUs > 0 {
					dc.CPUPct = (cpuDelta / sysDelta) * float64(stats.CPUStats.OnlineCPUs) * 100.0
				}
				dc.MemUsage = stats.MemoryStats.Usage
				dc.MemLimit = stats.MemoryStats.Limit
			}
		}

		containers = append(containers, dc)
	}

	return containers
}

func GetProcesses(limit int) []ProcessInfo {
	var procs []ProcessInfo
	pids, err := process.Processes()
	if err != nil {
		return procs
	}
	for _, p := range pids {
		name, _ := p.Name()
		cpuPct, _ := p.CPUPercent()
		memPct, _ := p.MemoryPercent()
		statusSlice, _ := p.Status()
		status := ""
		if len(statusSlice) > 0 {
			status = statusSlice[0]
		}
		procs = append(procs, ProcessInfo{
			PID:    p.Pid,
			Name:   name,
			CPU:    cpuPct,
			Mem:    memPct,
			Status: status,
		})
	}
	sort.Slice(procs, func(i, j int) bool {
		return procs[i].CPU > procs[j].CPU
	})
	if len(procs) > limit {
		procs = procs[:limit]
	}
	return procs
}
