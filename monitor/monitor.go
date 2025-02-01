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

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
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
