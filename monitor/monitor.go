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
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
	psnet "github.com/shirou/gopsutil/v3/net"
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
