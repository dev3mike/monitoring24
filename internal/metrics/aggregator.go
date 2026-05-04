package metrics

import (
	"time"

	"github.com/masoudx/monitoring24/internal/storage"
)

// MinuteAggregator accumulates fast-tick SystemSnapshot samples and produces a
// time-averaged storage.MetricSnapshot on each minute boundary.
//
// Not safe for concurrent use — call Add and Flush from the same goroutine.
type MinuteAggregator struct {
	currentMinute int64 // minute-truncated Unix timestamp of the open bucket

	count        int
	sumCPU       float64
	sumRAM       float64
	sumSwap      float64
	sumRAMUsed   float64
	sumRAMTotal  float64

	diskAcc map[string]*diskAccum
	netAcc  map[string]*netAccum
}

type diskAccum struct {
	sumPct float64
	count  int
	total  uint64
	used   uint64
	fsType string
}

type netAccum struct {
	sumSent float64
	sumRecv float64
	count   int
}

// NewMinuteAggregator returns a ready-to-use MinuteAggregator.
func NewMinuteAggregator() *MinuteAggregator {
	return &MinuteAggregator{
		diskAcc: make(map[string]*diskAccum),
		netAcc:  make(map[string]*netAccum),
	}
}

// Add incorporates one SystemSnapshot into the current minute bucket.
// If the snapshot belongs to a later minute, it is silently skipped; the
// caller must call Flush first to commit the completed bucket, then call Add
// again with the same snapshot to open the new bucket.
func (a *MinuteAggregator) Add(snap SystemSnapshot) {
	bucket := snap.CollectedAt.Truncate(time.Minute).Unix()
	if a.currentMinute == 0 {
		a.currentMinute = bucket
	}
	if bucket != a.currentMinute {
		return
	}

	a.count++
	a.sumCPU += snap.CPUPercent
	a.sumRAM += snap.MemPercent
	a.sumSwap += snap.SwapPercent
	a.sumRAMUsed += float64(snap.MemUsed)
	a.sumRAMTotal += float64(snap.MemTotal)

	for _, d := range snap.Disks {
		acc, ok := a.diskAcc[d.Mountpoint]
		if !ok {
			acc = &diskAccum{}
			a.diskAcc[d.Mountpoint] = acc
		}
		acc.sumPct += d.Percent
		acc.count++
		acc.total = d.Total
		acc.used = d.Used
		acc.fsType = d.FsType
	}

	for _, n := range snap.NetworkIO {
		acc, ok := a.netAcc[n.Interface]
		if !ok {
			acc = &netAccum{}
			a.netAcc[n.Interface] = acc
		}
		acc.sumSent += float64(n.BytesSentSec)
		acc.sumRecv += float64(n.BytesRecvSec)
		acc.count++
	}
}

// Flush checks whether the current minute bucket has closed relative to now.
// If yes, it builds an averaged MetricSnapshot, resets internal state, and
// returns (snapshot, true). Returns (zero, false) when the bucket is still open
// or no samples have been collected yet.
//
// Typical call pattern in the collector loop (called before Add):
//
//	if snap, ok := agg.Flush(time.Now()); ok {
//	    db.InsertMetricSnapshot(ctx, snap)
//	}
//	agg.Add(sysnap)
func (a *MinuteAggregator) Flush(now time.Time) (storage.MetricSnapshot, bool) {
	currentBucket := now.Truncate(time.Minute).Unix()
	if a.count == 0 || currentBucket <= a.currentMinute {
		return storage.MetricSnapshot{}, false
	}

	n := float64(a.count)
	snap := storage.MetricSnapshot{
		TS:       a.currentMinute,
		CPUPct:   a.sumCPU / n,
		RAMPct:   a.sumRAM / n,
		SwapPct:  a.sumSwap / n,
		RAMUsed:  uint64(a.sumRAMUsed / n),
		RAMTotal: uint64(a.sumRAMTotal / n),
	}

	snap.Disks = make([]storage.DiskSample, 0, len(a.diskAcc))
	for mp, acc := range a.diskAcc {
		snap.Disks = append(snap.Disks, storage.DiskSample{
			Mountpoint: mp,
			Percent:    acc.sumPct / float64(acc.count),
			Total:      acc.total,
			Used:       acc.used,
			FsType:     acc.fsType,
		})
	}

	snap.NetIfaces = make([]storage.NetSample, 0, len(a.netAcc))
	for iface, acc := range a.netAcc {
		snap.NetIfaces = append(snap.NetIfaces, storage.NetSample{
			Interface:    iface,
			BytesSentSec: uint64(acc.sumSent / float64(acc.count)),
			BytesRecvSec: uint64(acc.sumRecv / float64(acc.count)),
		})
	}

	a.currentMinute = 0
	a.count = 0
	a.sumCPU, a.sumRAM, a.sumSwap = 0, 0, 0
	a.sumRAMUsed, a.sumRAMTotal = 0, 0
	a.diskAcc = make(map[string]*diskAccum)
	a.netAcc = make(map[string]*netAccum)

	return snap, true
}
