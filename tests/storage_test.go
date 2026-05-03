package tests

import (
	"context"
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
