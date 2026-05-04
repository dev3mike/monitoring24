package tests

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/masoudx/monitoring24/internal/storage"
)

func TestStorage_OpenMigratesThresholds(t *testing.T) {
	// given
	db := openTestDB(t)

	// when
	th, err := db.GetThresholds()

	// then
	if err != nil {
		t.Fatalf("GetThresholds: %v", err)
	}
	if th["cpu_pct"] != 90 {
		t.Fatalf("expected default cpu_pct 90, got %v", th["cpu_pct"])
	}
	if th["url_latency_ms"] != 5000 {
		t.Fatalf("expected url_latency_ms seed, got %v", th["url_latency_ms"])
	}
}

func TestStorage_URLCheckCRUDAndCascade(t *testing.T) {
	// given
	db := openTestDB(t)
	now := time.Now()
	id, err := db.InsertURLCheck(storage.URLCheck{
		URL:             "https://example.com/a",
		Label:           "a",
		IntervalSeconds: 60,
		TimeoutSeconds:  10,
		Enabled:         true,
		CreatedAt:       now,
	})
	if err != nil {
		t.Fatal(err)
	}

	// when
	_ = db.InsertURLResult(storage.URLResult{
		CheckID:   id,
		CheckedAt: now,
		Up:        true,
	})
	u, err := db.GetURLCheck(id)

	// then
	if err != nil {
		t.Fatal(err)
	}
	if u.URL != "https://example.com/a" || !u.Enabled {
		t.Fatalf("unexpected row: %+v", u)
	}

	// when
	if err := db.DeleteURLCheck(id); err != nil {
		t.Fatal(err)
	}
	hist, err := db.URLResultHistory(id, 10)

	// then
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 0 {
		t.Fatalf("expected cascade delete of results, got %d rows", len(hist))
	}
}

func TestStorage_InsertURLCheck_DuplicateURL(t *testing.T) {
	// given
	db := openTestDB(t)
	now := time.Now()
	ch := storage.URLCheck{
		URL:             "https://dup.example/",
		Label:           "x",
		IntervalSeconds: 60,
		TimeoutSeconds:  10,
		Enabled:         true,
		CreatedAt:       now,
	}
	_, err := db.InsertURLCheck(ch)
	if err != nil {
		t.Fatal(err)
	}

	// when
	_, err = db.InsertURLCheck(ch)

	// then
	if err == nil {
		t.Fatal("expected unique constraint error on duplicate url")
	}
}

func TestStorage_URLUptime(t *testing.T) {
	// given
	db := openTestDB(t)
	now := time.Now()
	id, _ := db.InsertURLCheck(storage.URLCheck{
		URL:             "https://u.example/",
		Label:           "",
		IntervalSeconds: 60,
		TimeoutSeconds:  10,
		Enabled:         true,
		CreatedAt:       now,
	})
	since := now.Add(-time.Hour)
	_ = db.InsertURLResult(storage.URLResult{CheckID: id, CheckedAt: now, Up: true})
	_ = db.InsertURLResult(storage.URLResult{CheckID: id, CheckedAt: now.Add(time.Minute), Up: false})

	// when
	pct, err := db.URLUptime(id, since)

	// then
	if err != nil {
		t.Fatal(err)
	}
	if pct < 49 || pct > 51 {
		t.Fatalf("expected ~50%% uptime, got %v", pct)
	}
}

func TestStorage_PurgeOldURLResults(t *testing.T) {
	// given
	db := openTestDB(t)
	now := time.Now()
	id, _ := db.InsertURLCheck(storage.URLCheck{
		URL:             "https://old.example/",
		Label:           "",
		IntervalSeconds: 60,
		TimeoutSeconds:  10,
		Enabled:         true,
		CreatedAt:       now,
	})
	old := now.Add(-10 * 24 * time.Hour)
	_ = db.InsertURLResult(storage.URLResult{CheckID: id, CheckedAt: old, Up: true})
	_ = db.InsertURLResult(storage.URLResult{CheckID: id, CheckedAt: now, Up: true})

	// when
	if err := db.Purge(context.Background()); err != nil {
		t.Fatal(err)
	}
	hist, _ := db.URLResultHistory(id, 20)

	// then
	if len(hist) != 1 {
		t.Fatalf("expected 1 row after purge, got %d", len(hist))
	}
	if !hist[0].Up {
		t.Fatal("expected the recent row to remain")
	}
}

func TestStorage_AlertLifecycle(t *testing.T) {
	// given
	db := openTestDB(t)
	fired := time.Now()
	id, err := db.InsertAlert(storage.Alert{
		Kind:      "cpu_pct",
		Message:   "high",
		Value:     99,
		Threshold: 90,
		FiredAt:   fired,
	})
	if err != nil {
		t.Fatal(err)
	}

	// when
	active, err := db.ActiveAlerts()

	// then
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].ID != id {
		t.Fatalf("active alerts: %+v", active)
	}

	// when
	if err := db.ResolveAlert(id, time.Now()); err != nil {
		t.Fatal(err)
	}
	active2, _ := db.ActiveAlerts()

	// then
	if len(active2) != 0 {
		t.Fatalf("expected no active after resolve, got %+v", active2)
	}

	// when
	if err := db.AcknowledgeAlert(id); err != nil {
		t.Fatal(err)
	}
	recent, _ := db.RecentAlerts(5)

	// then
	found := false
	for _, a := range recent {
		if a.ID == id && a.Acknowledged {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected acknowledged recent alert")
	}
}

func TestStorage_QueryMetricHistoryAggregated(t *testing.T) {
	// given
	ctx := context.Background()
	db := openTestDB(t)
	base := time.Date(2025, 1, 2, 12, 0, 0, 0, time.UTC).Unix()
	for i := int64(0); i < 5; i++ {
		ts := base + i*60
		err := db.InsertMetricSnapshot(ctx, storage.MetricSnapshot{
			TS:        ts,
			CPUPct:    float64(10 * (i + 1)),
			RAMPct:    50,
			SwapPct:   0,
			RAMUsed:   1,
			RAMTotal:  2,
			Disks:     []storage.DiskSample{{Mountpoint: "/", Used: uint64(i+1) * 10, Total: 100}},
			NetIfaces: nil,
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	// when
	buckets, err := db.QueryMetricHistoryAggregated(ctx, "cpu", base, base+4*60, 60)

	// then
	if err != nil {
		t.Fatal(err)
	}
	if len(buckets) != 5 {
		t.Fatalf("want 5 buckets at 60s step, got %d", len(buckets))
	}

	// when
	buckets, err = db.QueryMetricHistoryAggregated(ctx, "cpu", base, base+4*60, 300)

	// then
	if err != nil {
		t.Fatal(err)
	}
	if len(buckets) != 1 {
		t.Fatalf("want 1 bucket at 300s step, got %d %+v", len(buckets), buckets)
	}
	wantAvg := (10.0 + 20 + 30 + 40 + 50) / 5
	if buckets[0].Value < wantAvg-0.01 || buckets[0].Value > wantAvg+0.01 {
		t.Fatalf("avg cpu: got %v want %v", buckets[0].Value, wantAvg)
	}

	// when
	diskBuckets, err := db.QueryMetricHistoryAggregated(ctx, "disk", base, base+4*60, 300)

	// then
	if err != nil {
		t.Fatal(err)
	}
	if len(diskBuckets) != 1 {
		t.Fatalf("disk buckets: %d", len(diskBuckets))
	}
	if diskBuckets[0].Value < 29.9 || diskBuckets[0].Value > 30.1 {
		t.Fatalf("disk avg: got %v", diskBuckets[0].Value)
	}
	if !diskBuckets[0].AuxUsedAvg.Valid || !diskBuckets[0].AuxTotalAvg.Valid {
		t.Fatal("expected disk avg used/total bytes")
	}
	if math.Abs(diskBuckets[0].AuxUsedAvg.Float64-30) > 0.02 || math.Abs(diskBuckets[0].AuxTotalAvg.Float64-100) > 0.02 {
		t.Fatalf("disk aux bytes: got %v / %v", diskBuckets[0].AuxUsedAvg.Float64, diskBuckets[0].AuxTotalAvg.Float64)
	}

	// when
	ramBuckets, err := db.QueryMetricHistoryAggregated(ctx, "ram", base, base+4*60, 300)

	// then
	if err != nil {
		t.Fatal(err)
	}
	if len(ramBuckets) != 1 {
		t.Fatalf("ram buckets: %d", len(ramBuckets))
	}
	if !ramBuckets[0].AuxUsedAvg.Valid || !ramBuckets[0].AuxTotalAvg.Valid {
		t.Fatal("expected ram avg used/total bytes")
	}
	if math.Abs(ramBuckets[0].AuxUsedAvg.Float64-1) > 0.02 || math.Abs(ramBuckets[0].AuxTotalAvg.Float64-2) > 0.02 {
		t.Fatalf("ram aux bytes: got %v / %v", ramBuckets[0].AuxUsedAvg.Float64, ramBuckets[0].AuxTotalAvg.Float64)
	}

	// when
	cpuBuckets, err := db.QueryMetricHistoryAggregated(ctx, "cpu", base, base+60, 60)

	// then
	if err != nil {
		t.Fatal(err)
	}
	if len(cpuBuckets) < 1 {
		t.Fatal("expected cpu buckets")
	}
	if cpuBuckets[0].AuxUsedAvg.Valid || cpuBuckets[0].AuxTotalAvg.Valid {
		t.Fatal("cpu history should not populate aux averages")
	}

	// when
	_, err = db.QueryMetricHistoryAggregated(ctx, "invalid", base, base+60, 60)

	// then
	if err == nil {
		t.Fatal("expected error for invalid kind")
	}
}

func TestStorage_FailedSSHByIP(t *testing.T) {
	// given
	db := openTestDB(t)
	ip := "192.168.1.10"
	u := "alice"
	p := "22"
	now := time.Now()
	_ = db.InsertSSHEvent(storage.SSHEvent{OccurredAt: now, EventType: "failed", Username: &u, SourceIP: ip, Port: &p})
	_ = db.InsertSSHEvent(storage.SSHEvent{OccurredAt: now, EventType: "failed", Username: &u, SourceIP: ip, Port: &p})

	// when
	counts, err := db.FailedSSHByIP(now.Add(-time.Hour))

	// then
	if err != nil {
		t.Fatal(err)
	}
	if len(counts) != 1 || counts[0].IP != ip || counts[0].Count != 2 {
		t.Fatalf("unexpected counts: %+v", counts)
	}
}
