package tests

import (
	"context"
	"testing"
	"time"

	"github.com/masoudx/monitoring24/internal/security"
	"github.com/masoudx/monitoring24/internal/storage"
)

func TestSecurity_ParseAuthLogLine_Failed(t *testing.T) {
	// given
	line := `Mar 15 10:11:22 host sshd[123]: Failed password for root from 203.0.113.5 port 22 ssh2`
	year := 2026

	// when
	ev, ok := security.ParseAuthLogLine(line, year)

	// then
	if !ok {
		t.Fatal("expected match")
	}
	if ev.EventType != "failed" || ev.SourceIP != "203.0.113.5" {
		t.Fatalf("unexpected event: %+v", ev)
	}
	if ev.Username == nil || *ev.Username != "root" {
		t.Fatalf("username: %+v", ev.Username)
	}
}

func TestSecurity_ParseAuthLogLine_InvalidUser(t *testing.T) {
	// given
	line := `Apr  1 08:00:01 host sshd[1]: Invalid user scanner from 198.51.100.2 port 5555`
	year := 2026

	// when
	ev, ok := security.ParseAuthLogLine(line, year)

	// then
	if !ok || ev.EventType != "invalid_user" {
		t.Fatalf("got ok=%v ev=%+v", ok, ev)
	}
	if ev.Username == nil || *ev.Username != "scanner" {
		t.Fatal("username")
	}
}

func TestSecurity_ParseAuthLogLine_Accepted(t *testing.T) {
	// given
	line := `May  3 12:00:00 host sshd[9]: Accepted publickey for deploy from 10.0.0.1 port 22 ssh2`
	year := 2026

	// when
	ev, ok := security.ParseAuthLogLine(line, year)

	// then
	if !ok || ev.EventType != "success" {
		t.Fatalf("got %+v", ev)
	}
}

func TestSecurity_ParseAuthLogLine_NoMatch(t *testing.T) {
	// given
	line := `May  3 12:00:00 host crontab[1]: (root) CMD (something)`

	// when
	_, ok := security.ParseAuthLogLine(line, 2026)

	// then
	if ok {
		t.Fatal("expected no match")
	}
}

func TestSecurity_ParseAuthLogLine_TimestampUsesYear(t *testing.T) {
	// given
	line := `Jan  2 15:04:05 host sshd[1]: Failed password for u from 1.2.3.4 port 99 ssh2`

	// when
	ev, ok := security.ParseAuthLogLine(line, 2026)

	// then
	if !ok {
		t.Fatal("expected match")
	}
	if ev.OccurredAt.Year() != 2026 || ev.OccurredAt.Month() != time.January || ev.OccurredAt.Day() != 2 {
		t.Fatalf("unexpected time: %v", ev.OccurredAt)
	}
}

func TestSecurity_Snapshot_ClassifiesEvents(t *testing.T) {
	// given
	db := openTestDB(t)
	u := "x"
	p := "22"
	now := time.Now()
	_ = db.InsertSSHEvent(storage.SSHEvent{OccurredAt: now, EventType: "failed", Username: &u, SourceIP: "10.0.0.1", Port: &p})
	_ = db.InsertSSHEvent(storage.SSHEvent{OccurredAt: now, EventType: "success", Username: &u, SourceIP: "10.0.0.2", Port: &p})
	m := security.NewMonitor(db)

	// when
	snap, err := m.Snapshot(context.Background())

	// then
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.RecentFailed) != 1 || len(snap.RecentSuccess) != 1 {
		t.Fatalf("failed=%d success=%d", len(snap.RecentFailed), len(snap.RecentSuccess))
	}
}
