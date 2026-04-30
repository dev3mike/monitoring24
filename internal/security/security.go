package security

import (
	"bufio"
	"bytes"
	"context"
	"os/exec"
	"time"

	"github.com/masoudx/monitoring24/internal/storage"
)

// Snapshot is the current security posture.
type Snapshot struct {
	CollectedAt          time.Time        `json:"collected_at"`
	RecentFailed         []storage.SSHEvent `json:"recent_failed_logins"`
	RecentSuccess        []storage.SSHEvent `json:"recent_success_logins"`
	TopFailedIPs         []storage.IPCount  `json:"top_failed_ips"`
	PendingUpdates       int              `json:"pending_updates"`
	PendingSecUpdates    int              `json:"pending_security_updates"`
	LogAvailable         bool             `json:"log_available"`
}

// Monitor owns the auth log parser and exposes snapshots.
type Monitor struct {
	db     *storage.DB
	parser *Parser
}

func NewMonitor(db *storage.DB) *Monitor {
	return &Monitor{
		db:     db,
		parser: newParser(db),
	}
}

// Parse incrementally ingests new auth log lines.
func (m *Monitor) Parse(ctx context.Context) error {
	return m.parser.parse(ctx)
}

// Snapshot builds and returns the current security snapshot.
func (m *Monitor) Snapshot(ctx context.Context) (*Snapshot, error) {
	s := &Snapshot{
		CollectedAt:  time.Now(),
		LogAvailable: m.parser.logPath != "",
	}

	failed, err := m.db.RecentSSHEvents(50)
	if err != nil {
		return s, err
	}
	for _, e := range failed {
		switch e.EventType {
		case "failed", "invalid_user":
			s.RecentFailed = append(s.RecentFailed, e)
		case "success":
			s.RecentSuccess = append(s.RecentSuccess, e)
		}
	}

	ips, err := m.db.FailedSSHByIP(time.Now().Add(-24 * time.Hour))
	if err != nil {
		return s, err
	}
	s.TopFailedIPs = ips

	total, sec := checkPendingUpdates(ctx)
	s.PendingUpdates = total
	s.PendingSecUpdates = sec

	return s, nil
}

// runCmd executes a command and returns its combined output as a reader.
func runCmd(ctx context.Context, name string, args ...string) (*bufio.Scanner, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return bufio.NewScanner(&buf), nil
}
