package metrics

import (
	"context"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
	psnet "github.com/shirou/gopsutil/v3/net"
)

// SystemSnapshot holds a point-in-time view of system-level metrics.
type SystemSnapshot struct {
	CollectedAt   time.Time   `json:"collected_at"`
	CPUPercent    float64     `json:"cpu_percent"`
	CPUPerCore    []float64   `json:"cpu_per_core"`
	LoadAvg1      float64     `json:"load_avg_1"`
	LoadAvg5      float64     `json:"load_avg_5"`
	LoadAvg15     float64     `json:"load_avg_15"`
	MemTotal      uint64      `json:"mem_total"`
	MemUsed       uint64      `json:"mem_used"`
	MemPercent    float64     `json:"mem_percent"`
	SwapTotal     uint64      `json:"swap_total"`
	SwapUsed      uint64      `json:"swap_used"`
	SwapPercent   float64     `json:"swap_percent"`
	Disks         []DiskStat  `json:"disks"`
	DiskIO        []DiskIOStat `json:"disk_io"`
	NetworkIO     []NetIOStat `json:"network_io"`
	UptimeSeconds uint64      `json:"uptime_seconds"`
}

type DiskStat struct {
	Mountpoint string  `json:"mountpoint"`
	Total      uint64  `json:"total"`
	Used       uint64  `json:"used"`
	Percent    float64 `json:"percent"`
	FsType     string  `json:"fs_type"`
}

type DiskIOStat struct {
	Device        string `json:"device"`
	ReadBytesSec  uint64 `json:"read_bytes_sec"`
	WriteBytesSec uint64 `json:"write_bytes_sec"`
}

type NetIOStat struct {
	Interface   string `json:"interface"`
	BytesSentSec uint64 `json:"bytes_sent_sec"`
	BytesRecvSec uint64 `json:"bytes_recv_sec"`
}

// pseudoFS lists filesystem types to skip in disk listing.
var pseudoFS = map[string]bool{
	"tmpfs": true, "devtmpfs": true, "squashfs": true, "overlay": true,
	"proc": true, "sysfs": true, "devpts": true, "cgroup": true,
	"cgroup2": true, "mqueue": true, "hugetlbfs": true, "debugfs": true,
	"autofs": true, "securityfs": true, "fusectl": true,
}

// Collector tracks previous I/O counters for delta calculation.
type Collector struct {
	prevDiskIO map[string]disk.IOCountersStat
	prevNetIO  map[string]psnet.IOCountersStat
	prevTime   time.Time
}

func NewCollector() *Collector {
	return &Collector{}
}

func (c *Collector) Collect(ctx context.Context) (*SystemSnapshot, error) {
	snap := &SystemSnapshot{CollectedAt: time.Now()}
	now := snap.CollectedAt

	// CPU — averaged across all cores (1-second sample)
	cpuPct, err := cpu.PercentWithContext(ctx, time.Second, false)
	if err == nil && len(cpuPct) > 0 {
		snap.CPUPercent = cpuPct[0]
	}

	// Per-core (no extra sleep — share the sample window)
	perCore, err := cpu.PercentWithContext(ctx, 0, true)
	if err == nil {
		snap.CPUPerCore = perCore
	}

	// Load average
	if avg, err := load.AvgWithContext(ctx); err == nil {
		snap.LoadAvg1 = avg.Load1
		snap.LoadAvg5 = avg.Load5
		snap.LoadAvg15 = avg.Load15
	}

	// Memory
	if vm, err := mem.VirtualMemoryWithContext(ctx); err == nil {
		snap.MemTotal = vm.Total
		snap.MemUsed = vm.Used
		snap.MemPercent = vm.UsedPercent
	}

	// Swap
	if sw, err := mem.SwapMemoryWithContext(ctx); err == nil {
		snap.SwapTotal = sw.Total
		snap.SwapUsed = sw.Used
		snap.SwapPercent = sw.UsedPercent
	}

	// Disk usage per partition
	if parts, err := disk.PartitionsWithContext(ctx, false); err == nil {
		for _, p := range parts {
			if pseudoFS[p.Fstype] {
				continue
			}
			if strings.HasPrefix(p.Mountpoint, "/sys") || strings.HasPrefix(p.Mountpoint, "/proc") {
				continue
			}
			if u, err := disk.UsageWithContext(ctx, p.Mountpoint); err == nil {
				snap.Disks = append(snap.Disks, DiskStat{
					Mountpoint: p.Mountpoint,
					Total:      u.Total,
					Used:       u.Used,
					Percent:    u.UsedPercent,
					FsType:     p.Fstype,
				})
			}
		}
	}

	// Disk I/O — deltas from previous snapshot
	if ioCounters, err := disk.IOCountersWithContext(ctx); err == nil {
		elapsed := now.Sub(c.prevTime).Seconds()
		if elapsed <= 0 {
			elapsed = 1
		}
		for dev, cur := range ioCounters {
			if prev, ok := c.prevDiskIO[dev]; ok && !c.prevTime.IsZero() {
				rSec := uint64(float64(cur.ReadBytes-prev.ReadBytes) / elapsed)
				wSec := uint64(float64(cur.WriteBytes-prev.WriteBytes) / elapsed)
				snap.DiskIO = append(snap.DiskIO, DiskIOStat{
					Device:        dev,
					ReadBytesSec:  rSec,
					WriteBytesSec: wSec,
				})
			}
		}
		c.prevDiskIO = ioCounters
	}

	// Network I/O — deltas from previous snapshot
	if netCounters, err := psnet.IOCountersWithContext(ctx, true); err == nil {
		elapsed := now.Sub(c.prevTime).Seconds()
		if elapsed <= 0 {
			elapsed = 1
		}
		netMap := make(map[string]psnet.IOCountersStat)
		for _, iface := range netCounters {
			netMap[iface.Name] = iface
			if prev, ok := c.prevNetIO[iface.Name]; ok && !c.prevTime.IsZero() {
				sSec := uint64(float64(iface.BytesSent-prev.BytesSent) / elapsed)
				rSec := uint64(float64(iface.BytesRecv-prev.BytesRecv) / elapsed)
				if iface.Name == "lo" {
					continue
				}
				snap.NetworkIO = append(snap.NetworkIO, NetIOStat{
					Interface:    iface.Name,
					BytesSentSec: sSec,
					BytesRecvSec: rSec,
				})
			}
		}
		c.prevNetIO = netMap
	}

	// Uptime
	if up, err := host.UptimeWithContext(ctx); err == nil {
		snap.UptimeSeconds = up
	}

	c.prevTime = now
	return snap, nil
}
