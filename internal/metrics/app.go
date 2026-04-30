package metrics

import (
	"context"
	"os"
	"runtime"
	"time"

	"github.com/shirou/gopsutil/v3/process"
)

// AppSnapshot holds self-metrics for the monitoring process.
type AppSnapshot struct {
	CollectedAt       time.Time `json:"collected_at"`
	GoRoutines        int       `json:"goroutines"`
	HeapAllocBytes    uint64    `json:"heap_alloc_bytes"`
	HeapSysBytes      uint64    `json:"heap_sys_bytes"`
	GCPauseTotalMs    float64   `json:"gc_pause_total_ms"`
	NumGC             uint32    `json:"num_gc"`
	ProcessCPUPercent float64   `json:"process_cpu_percent"`
	ProcessMemBytes   uint64    `json:"process_mem_bytes"`
	OpenFileCount     int32     `json:"open_file_count"`
	UptimeSeconds     int64     `json:"uptime_seconds"`
}

// CollectApp gathers runtime and process metrics for the current process.
func CollectApp(ctx context.Context, startTime time.Time) (*AppSnapshot, error) {
	snap := &AppSnapshot{CollectedAt: time.Now()}

	snap.GoRoutines = runtime.NumGoroutine()
	snap.UptimeSeconds = int64(time.Since(startTime).Seconds())

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	snap.HeapAllocBytes = ms.HeapAlloc
	snap.HeapSysBytes = ms.HeapSys
	snap.GCPauseTotalMs = float64(ms.PauseTotalNs) / 1e6
	snap.NumGC = ms.NumGC

	pid := int32(os.Getpid())
	if proc, err := process.NewProcess(pid); err == nil {
		if pct, err := proc.CPUPercentWithContext(ctx); err == nil {
			snap.ProcessCPUPercent = pct
		}
		if info, err := proc.MemoryInfoWithContext(ctx); err == nil {
			snap.ProcessMemBytes = info.RSS
		}
		if files, err := proc.OpenFilesWithContext(ctx); err == nil {
			snap.OpenFileCount = int32(len(files))
		}
	}

	return snap, nil
}
