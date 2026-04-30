package services

import (
	"bufio"
	"bytes"
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ServiceStatus holds the status of a single systemd service.
type ServiceStatus struct {
	Name         string     `json:"name"`
	Active       string     `json:"active"`
	SubState     string     `json:"sub_state"`
	LoadState    string     `json:"load_state"`
	RestartCount int        `json:"restart_count"`
	Since        *time.Time `json:"since,omitempty"`
}

// Snapshot holds all service statuses at a point in time.
type Snapshot struct {
	CollectedAt       time.Time       `json:"collected_at"`
	Services          []ServiceStatus `json:"services"`
	SystemdAvailable  bool            `json:"systemd_available"`
}

// DefaultServices is the default set of services to monitor.
var DefaultServices = []string{
	"nginx", "apache2", "postgresql", "mysql", "redis",
	"docker", "ssh", "sshd", "cloudflared", "fail2ban",
}

// Monitor checks systemd service states.
type Monitor struct {
	services []string
}

func NewMonitor(services []string) *Monitor {
	if len(services) == 0 {
		services = DefaultServices
	}
	return &Monitor{services: services}
}

// Collect queries all configured services via systemctl.
func (m *Monitor) Collect(ctx context.Context) (*Snapshot, error) {
	snap := &Snapshot{CollectedAt: time.Now()}

	if _, err := exec.LookPath("systemctl"); err != nil {
		return snap, nil // systemd not available (macOS, non-systemd Linux)
	}
	snap.SystemdAvailable = true

	for _, name := range m.services {
		svc, err := queryService(ctx, name)
		if err != nil {
			continue
		}
		snap.Services = append(snap.Services, svc)
	}

	return snap, nil
}

func queryService(ctx context.Context, name string) (ServiceStatus, error) {
	svc := ServiceStatus{Name: name}

	cmd := exec.CommandContext(ctx, "systemctl", "show", name,
		"--no-pager",
		"--property=ActiveState,SubState,LoadState,NRestarts,ActiveEnterTimestamp",
	)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	if err := cmd.Run(); err != nil {
		svc.Active = "unknown"
		return svc, nil
	}

	scanner := bufio.NewScanner(&buf)
	for scanner.Scan() {
		line := scanner.Text()
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch k {
		case "ActiveState":
			svc.Active = v
		case "SubState":
			svc.SubState = v
		case "LoadState":
			svc.LoadState = v
		case "NRestarts":
			n, _ := strconv.Atoi(v)
			svc.RestartCount = n
		case "ActiveEnterTimestamp":
			if v != "" && v != "n/a" {
				// format: "Wed 2024-01-10 08:00:00 UTC"
				t, err := time.Parse("Mon 2006-01-02 15:04:05 MST", v)
				if err == nil {
					svc.Since = &t
				}
			}
		}
	}

	return svc, scanner.Err()
}
