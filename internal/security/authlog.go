package security

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"time"

	"github.com/masoudx/monitoring24/internal/storage"
)

var (
	reFailed = regexp.MustCompile(
		`(\w{3}\s+\d+\s+\d{2}:\d{2}:\d{2}).*Failed password for (?:invalid user )?(\S+) from (\d+\.\d+\.\d+\.\d+) port (\d+)`)
	reAccepted = regexp.MustCompile(
		`(\w{3}\s+\d+\s+\d{2}:\d{2}:\d{2}).*Accepted (?:password|publickey) for (\S+) from (\d+\.\d+\.\d+\.\d+) port (\d+)`)
	reInvalid = regexp.MustCompile(
		`(\w{3}\s+\d+\s+\d{2}:\d{2}:\d{2}).*Invalid user (\S+) from (\d+\.\d+\.\d+\.\d+) port (\d+)`)
)

// authLogPaths lists candidates in priority order.
var authLogPaths = []string{
	"/var/log/auth.log",
	"/var/log/secure",
}

// Parser incrementally parses an auth log file for SSH events.
type Parser struct {
	db         *storage.DB
	logPath    string
	lastOffset int64
	warned     bool
}

func newParser(db *storage.DB) *Parser {
	p := &Parser{db: db}
	for _, path := range authLogPaths {
		if _, err := os.Stat(path); err == nil {
			p.logPath = path
			break
		}
	}
	return p
}

// Parse reads new lines from the auth log since the last call.
func (p *Parser) parse(ctx context.Context) error {
	if p.logPath == "" {
		if !p.warned {
			log.Println("[security] auth log not found — SSH monitoring disabled")
			p.warned = true
		}
		return nil
	}

	f, err := os.Open(p.logPath)
	if err != nil {
		return fmt.Errorf("open auth log: %w", err)
	}
	defer f.Close()

	// Handle log rotation
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if p.lastOffset > fi.Size() {
		p.lastOffset = 0
	}

	if _, err := f.Seek(p.lastOffset, io.SeekStart); err != nil {
		return err
	}

	year := time.Now().Year()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if event, ok := ParseAuthLogLine(line, year); ok {
			_ = p.db.InsertSSHEvent(event)
		}
	}

	pos, _ := f.Seek(0, io.SeekCurrent)
	p.lastOffset = pos
	return scanner.Err()
}

// ParseAuthLogLine parses one auth.log-style line into an SSH event when it matches
// a known SSH pattern (failed login, accepted, or invalid user).
func ParseAuthLogLine(line string, year int) (storage.SSHEvent, bool) {
	if m := reFailed.FindStringSubmatch(line); m != nil {
		ts := parseTimestamp(m[1], year)
		user := m[2]
		port := m[4]
		return storage.SSHEvent{
			OccurredAt: ts,
			EventType:  "failed",
			Username:   &user,
			SourceIP:   m[3],
			Port:       &port,
		}, true
	}
	if m := reAccepted.FindStringSubmatch(line); m != nil {
		ts := parseTimestamp(m[1], year)
		user := m[2]
		port := m[4]
		return storage.SSHEvent{
			OccurredAt: ts,
			EventType:  "success",
			Username:   &user,
			SourceIP:   m[3],
			Port:       &port,
		}, true
	}
	if m := reInvalid.FindStringSubmatch(line); m != nil {
		ts := parseTimestamp(m[1], year)
		user := m[2]
		port := m[4]
		return storage.SSHEvent{
			OccurredAt: ts,
			EventType:  "invalid_user",
			Username:   &user,
			SourceIP:   m[3],
			Port:       &port,
		}, true
	}
	return storage.SSHEvent{}, false
}

func parseTimestamp(s string, year int) time.Time {
	t, err := time.Parse("Jan  2 15:04:05", s)
	if err != nil {
		t, err = time.Parse("Jan _2 15:04:05", s)
	}
	if err != nil {
		return time.Now()
	}
	return time.Date(year, t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.Local)
}

// CheckPendingUpdates runs apt-get --simulate and counts pending packages.
// Returns -1,-1 if apt is unavailable.
func checkPendingUpdates(ctx context.Context) (total int, security int) {
	// Try apt-get
	out, err := runCmd(ctx, "apt-get", "--simulate", "-qq", "upgrade")
	if err != nil {
		return -1, -1
	}
	scanner := out
	for scanner.Scan() {
		line := scanner.Text()
		total++
		if containsCI(line, "security") {
			security++
		}
	}
	return total, security
}

func containsCI(s, sub string) bool {
	return len(s) >= len(sub) && findCI(s, sub)
}

func findCI(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		match := true
		for j := 0; j < len(sub); j++ {
			a, b := s[i+j], sub[j]
			if a >= 'A' && a <= 'Z' {
				a += 32
			}
			if a != b {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
