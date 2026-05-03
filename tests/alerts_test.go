package tests

import (
	"context"
	"testing"

	"github.com/masoudx/monitoring24/internal/alerts"
)

func TestAlerts_Engine_FireAndResolveCPU(t *testing.T) {
	// given
	db := openTestDB(t)
	eng := alerts.NewEngine(db)
	ctx := context.Background()
	if err := eng.LoadThresholds(ctx); err != nil {
		t.Fatal(err)
	}

	// when
	fired, err := eng.Evaluate(ctx, alerts.AllMetrics{
		CPUPercent:  95,
		RAMPercent:  10,
		SwapPercent: 0,
	})

	// then
	if err != nil {
		t.Fatal(err)
	}
	if len(fired) != 1 || fired[0].Kind != "cpu_pct" {
		t.Fatalf("expected one cpu_pct alert, got %+v", fired)
	}

	// when
	fired2, err := eng.Evaluate(ctx, alerts.AllMetrics{
		CPUPercent:  95,
		RAMPercent:  10,
		SwapPercent: 0,
	})

	// then
	if err != nil {
		t.Fatal(err)
	}
	if len(fired2) != 0 {
		t.Fatalf("expected no duplicate fire, got %+v", fired2)
	}

	// when
	_, err = eng.Evaluate(ctx, alerts.AllMetrics{
		CPUPercent:  10,
		RAMPercent:  10,
		SwapPercent: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	active, _ := db.ActiveAlerts()

	// then
	if len(active) != 0 {
		t.Fatalf("expected resolved cpu alert, active=%+v", active)
	}
}

func TestAlerts_Engine_DiskPerMount(t *testing.T) {
	// given
	db := openTestDB(t)
	eng := alerts.NewEngine(db)
	ctx := context.Background()
	if err := eng.LoadThresholds(ctx); err != nil {
		t.Fatal(err)
	}

	// when
	fired, err := eng.Evaluate(ctx, alerts.AllMetrics{
		CPUPercent:  1,
		RAMPercent:  1,
		SwapPercent: 0,
		Disks: []alerts.DiskAlert{
			{Mountpoint: "/data", Percent: 92},
		},
	})

	// then
	if err != nil {
		t.Fatal(err)
	}
	if len(fired) != 1 || fired[0].Kind != "disk_pct:/data" {
		t.Fatalf("unexpected fired: %+v", fired)
	}
}

func TestAlerts_Engine_URLDown(t *testing.T) {
	// given
	db := openTestDB(t)
	eng := alerts.NewEngine(db)
	ctx := context.Background()
	if err := eng.LoadThresholds(ctx); err != nil {
		t.Fatal(err)
	}

	// when
	fired, err := eng.Evaluate(ctx, alerts.AllMetrics{
		CPUPercent:  1,
		RAMPercent:  1,
		SwapPercent: 0,
		URLStats: []alerts.URLAlert{
			{ID: 42, URL: "https://x.example", Label: "svc", Up: false},
		},
	})

	// then
	if err != nil {
		t.Fatal(err)
	}
	if len(fired) != 1 || fired[0].Kind != "url_down:42" {
		t.Fatalf("unexpected: %+v", fired)
	}
}

func TestAlerts_Engine_LoadThresholdsRestoresActiveMap(t *testing.T) {
	// given
	db := openTestDB(t)
	eng := alerts.NewEngine(db)
	ctx := context.Background()
	_ = eng.LoadThresholds(ctx)
	_, _ = eng.Evaluate(ctx, alerts.AllMetrics{CPUPercent: 99, RAMPercent: 1, SwapPercent: 0})
	eng2 := alerts.NewEngine(db)

	// when
	if err := eng2.LoadThresholds(ctx); err != nil {
		t.Fatal(err)
	}
	fired, err := eng2.Evaluate(ctx, alerts.AllMetrics{CPUPercent: 99, RAMPercent: 1, SwapPercent: 0})

	// then
	if err != nil {
		t.Fatal(err)
	}
	if len(fired) != 0 {
		t.Fatalf("expected dedupe after restart, got %+v", fired)
	}
}

func TestAlerts_Engine_UpdateThreshold(t *testing.T) {
	// given
	db := openTestDB(t)
	eng := alerts.NewEngine(db)
	ctx := context.Background()
	if err := eng.LoadThresholds(ctx); err != nil {
		t.Fatal(err)
	}

	// when
	if err := eng.UpdateThreshold(ctx, "cpu_pct", 50); err != nil {
		t.Fatal(err)
	}

	// then
	if eng.Threshold("cpu_pct", 0) != 50 {
		t.Fatalf("expected updated threshold in memory")
	}
	reloaded := alerts.NewEngine(db)
	if err := reloaded.LoadThresholds(ctx); err != nil {
		t.Fatal(err)
	}
	if reloaded.Threshold("cpu_pct", 0) != 50 {
		t.Fatalf("expected persisted threshold")
	}
}

func TestAlerts_Engine_ZeroThresholdSkipsCheck(t *testing.T) {
	// given
	db := openTestDB(t)
	eng := alerts.NewEngine(db)
	ctx := context.Background()
	if err := eng.UpdateThreshold(ctx, "cpu_pct", 0); err != nil {
		t.Fatal(err)
	}
	if err := eng.UpdateThreshold(ctx, "ram_pct", 0); err != nil {
		t.Fatal(err)
	}
	if err := eng.UpdateThreshold(ctx, "swap_pct", 0); err != nil {
		t.Fatal(err)
	}
	if err := eng.UpdateThreshold(ctx, "disk_pct", 0); err != nil {
		t.Fatal(err)
	}

	// when
	fired, err := eng.Evaluate(ctx, alerts.AllMetrics{
		CPUPercent:  100,
		RAMPercent:  100,
		SwapPercent: 100,
		Disks:       []alerts.DiskAlert{{Mountpoint: "/", Percent: 100}},
	})

	// then
	if err != nil {
		t.Fatal(err)
	}
	if len(fired) != 0 {
		t.Fatalf("expected no alerts when thresholds are zero, got %+v", fired)
	}
}
