package alerts

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/masoudx/monitoring24/internal/storage"
)

// AllMetrics bundles all snapshot types for alert evaluation.
type AllMetrics struct {
	CPUPercent  float64
	RAMPercent  float64
	SwapPercent float64
	Disks       []DiskAlert
	URLStats    []URLAlert
}

type DiskAlert struct {
	Mountpoint string
	Percent    float64
}

type URLAlert struct {
	ID    int64
	URL   string
	Label string
	Up    bool
}

// Engine evaluates threshold conditions and manages alert lifecycle.
type Engine struct {
	db           *storage.DB
	mu           sync.Mutex
	thresholds   map[string]float64
	activeAlerts map[string]int64 // kind key → alert id
}

func NewEngine(db *storage.DB) *Engine {
	return &Engine{
		db:           db,
		thresholds:   make(map[string]float64),
		activeAlerts: make(map[string]int64),
	}
}

// LoadThresholds reads thresholds from the DB and re-populates activeAlerts
// from any still-active alerts (survives restarts without duplicate rows).
func (e *Engine) LoadThresholds(ctx context.Context) error {
	t, err := e.db.GetThresholds()
	if err != nil {
		return err
	}
	e.mu.Lock()
	e.thresholds = t
	e.mu.Unlock()

	active, err := e.db.ActiveAlerts()
	if err != nil {
		return err
	}
	e.mu.Lock()
	for _, a := range active {
		e.activeAlerts[a.Kind] = a.ID
	}
	e.mu.Unlock()
	return nil
}

// Threshold returns the configured threshold for key, or the provided default.
func (e *Engine) Threshold(key string, def float64) float64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	if v, ok := e.thresholds[key]; ok {
		return v
	}
	return def
}

// UpdateThreshold updates a single threshold value in memory and DB.
func (e *Engine) UpdateThreshold(ctx context.Context, key string, value float64) error {
	if err := e.db.SetThreshold(key, value); err != nil {
		return err
	}
	e.mu.Lock()
	e.thresholds[key] = value
	e.mu.Unlock()
	return nil
}

// GetThresholds returns a copy of the current threshold map.
func (e *Engine) GetThresholds() map[string]float64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make(map[string]float64, len(e.thresholds))
	for k, v := range e.thresholds {
		out[k] = v
	}
	return out
}

// Evaluate checks all metrics against thresholds and fires/resolves alerts.
// Returns newly fired alerts (for SSE broadcast).
func (e *Engine) Evaluate(ctx context.Context, m AllMetrics) ([]storage.Alert, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	type check struct {
		kind      string
		value     float64
		threshold float64
		msg       string
	}

	checks := []check{
		{
			kind:      "cpu_pct",
			value:     m.CPUPercent,
			threshold: e.thresholds["cpu_pct"],
			msg:       fmt.Sprintf("CPU usage at %.1f%% (threshold %.0f%%)", m.CPUPercent, e.thresholds["cpu_pct"]),
		},
		{
			kind:      "ram_pct",
			value:     m.RAMPercent,
			threshold: e.thresholds["ram_pct"],
			msg:       fmt.Sprintf("RAM usage at %.1f%% (threshold %.0f%%)", m.RAMPercent, e.thresholds["ram_pct"]),
		},
		{
			kind:      "swap_pct",
			value:     m.SwapPercent,
			threshold: e.thresholds["swap_pct"],
			msg:       fmt.Sprintf("Swap usage at %.1f%% (threshold %.0f%%)", m.SwapPercent, e.thresholds["swap_pct"]),
		},
	}

	diskThreshold := e.thresholds["disk_pct"]
	for _, d := range m.Disks {
		kind := "disk_pct:" + d.Mountpoint
		checks = append(checks, check{
			kind:      kind,
			value:     d.Percent,
			threshold: diskThreshold,
			msg:       fmt.Sprintf("Disk %s at %.1f%% (threshold %.0f%%)", d.Mountpoint, d.Percent, diskThreshold),
		})
	}

	for _, u := range m.URLStats {
		kind := fmt.Sprintf("url_down:%d", u.ID)
		label := u.Label
		if label == "" {
			label = u.URL
		}
		val := 0.0
		if !u.Up {
			val = 1.0
		}
		checks = append(checks, check{
			kind:      kind,
			value:     val,
			threshold: 1.0,
			msg:       fmt.Sprintf("URL %q is DOWN", label),
		})
	}

	var fired []storage.Alert
	for _, c := range checks {
		if c.threshold <= 0 {
			continue
		}
		if c.value >= c.threshold {
			if _, alreadyActive := e.activeAlerts[c.kind]; !alreadyActive {
				a := storage.Alert{
					Kind:      c.kind,
					Message:   c.msg,
					Value:     c.value,
					Threshold: c.threshold,
					FiredAt:   time.Now(),
				}
				id, err := e.db.InsertAlert(a)
				if err != nil {
					return fired, err
				}
				e.activeAlerts[c.kind] = id
				a.ID = id
				fired = append(fired, a)
			}
		} else {
			if id, active := e.activeAlerts[c.kind]; active {
				_ = e.db.ResolveAlert(id, time.Now())
				delete(e.activeAlerts, c.kind)
			}
		}
	}

	return fired, nil
}
