package tunnel

import (
	"bytes"
	"context"
	"log"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/process"
	"github.com/masoudx/monitoring24/internal/storage"
)

// Status represents the current state of the Cloudflare Tunnel.
type Status struct {
	CollectedAt   time.Time            `json:"collected_at"`
	Running       bool                 `json:"running"`
	PID           int32                `json:"pid,omitempty"`
	UptimeSeconds int64                `json:"uptime_seconds"`
	TunnelName    string               `json:"tunnel_name,omitempty"`
	Version       string               `json:"version,omitempty"`
	RecentEvents  []storage.TunnelEvent `json:"recent_events"`
}

var reVersion = regexp.MustCompile(`cloudflared version ([^\s]+)`)

// Detector tracks cloudflared process state and emits events on transitions.
type Detector struct {
	db              *storage.DB
	mu              sync.Mutex
	lastRunning     bool
	cachedVersion   string
	versionDetected bool
}

func NewDetector(db *storage.DB) *Detector {
	return &Detector{db: db}
}

// Collect detects the cloudflared process and returns current status.
func (d *Detector) Collect(ctx context.Context) (*Status, error) {
	s := &Status{CollectedAt: time.Now()}

	procs, err := process.ProcessesWithContext(ctx)
	if err != nil {
		procs = nil // graceful degradation
	}

	var found *process.Process
	for _, p := range procs {
		name, err := p.NameWithContext(ctx)
		if err != nil {
			continue
		}
		if strings.Contains(strings.ToLower(name), "cloudflared") {
			found = p
			break
		}
	}

	if found != nil {
		s.Running = true
		s.PID = found.Pid

		if ct, err := found.CreateTimeWithContext(ctx); err == nil {
			startTime := time.UnixMilli(ct)
			s.UptimeSeconds = int64(time.Since(startTime).Seconds())
		}

		if cmdline, err := found.CmdlineSliceWithContext(ctx); err == nil {
			s.TunnelName = TunnelNameFromArgs(cmdline)
		}

		s.Version = d.getVersion(ctx)
	}

	// State transition events
	d.mu.Lock()
	wasRunning := d.lastRunning
	d.lastRunning = s.Running
	d.mu.Unlock()

	now := time.Now()
	if wasRunning && !s.Running {
		_ = d.db.InsertTunnelEvent(storage.TunnelEvent{
			OccurredAt: now,
			EventType:  "disconnected",
		})
		log.Println("[tunnel] cloudflared process stopped")
	} else if !wasRunning && s.Running {
		_ = d.db.InsertTunnelEvent(storage.TunnelEvent{
			OccurredAt: now,
			EventType:  "connected",
		})
		log.Println("[tunnel] cloudflared process started")
	}

	events, _ := d.db.RecentTunnelEvents(10)
	s.RecentEvents = events

	return s, nil
}

// TunnelNameFromArgs extracts a tunnel name from a cloudflared argv slice
// (--name NAME or "run NAME").
func TunnelNameFromArgs(args []string) string {
	for i, arg := range args {
		if arg == "--name" && i+1 < len(args) {
			return args[i+1]
		}
		if arg == "run" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func (d *Detector) getVersion(ctx context.Context) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.versionDetected {
		return d.cachedVersion
	}
	d.versionDetected = true

	cmd := exec.CommandContext(ctx, "cloudflared", "--version")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	if err := cmd.Run(); err == nil {
		if m := reVersion.FindSubmatch(buf.Bytes()); m != nil {
			d.cachedVersion = string(m[1])
		}
	}
	return d.cachedVersion
}
