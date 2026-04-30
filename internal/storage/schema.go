package storage

import "time"

const schemaSQL = `
CREATE TABLE IF NOT EXISTS alerts (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    kind         TEXT    NOT NULL,
    message      TEXT    NOT NULL,
    value        REAL    NOT NULL,
    threshold    REAL    NOT NULL,
    fired_at     INTEGER NOT NULL,
    resolved_at  INTEGER,
    acknowledged INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS url_checks (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    url              TEXT    NOT NULL UNIQUE,
    label            TEXT    NOT NULL DEFAULT '',
    interval_seconds INTEGER NOT NULL DEFAULT 60,
    timeout_seconds  INTEGER NOT NULL DEFAULT 10,
    enabled          INTEGER NOT NULL DEFAULT 1,
    created_at       INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS url_check_results (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    check_id    INTEGER NOT NULL REFERENCES url_checks(id) ON DELETE CASCADE,
    checked_at  INTEGER NOT NULL,
    up          INTEGER NOT NULL,
    status_code INTEGER,
    latency_ms  INTEGER,
    error       TEXT
);
CREATE INDEX IF NOT EXISTS idx_url_results_check_time ON url_check_results(check_id, checked_at);

CREATE TABLE IF NOT EXISTS ssh_events (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    occurred_at INTEGER NOT NULL,
    event_type  TEXT    NOT NULL,
    username    TEXT,
    source_ip   TEXT    NOT NULL,
    port        TEXT
);
CREATE INDEX IF NOT EXISTS idx_ssh_events_occurred ON ssh_events(occurred_at);

CREATE TABLE IF NOT EXISTS tunnel_events (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    occurred_at INTEGER NOT NULL,
    event_type  TEXT    NOT NULL,
    detail      TEXT
);

CREATE TABLE IF NOT EXISTS thresholds (
    key   TEXT PRIMARY KEY,
    value REAL NOT NULL
);

INSERT OR IGNORE INTO thresholds(key, value) VALUES
    ('cpu_pct',        90.0),
    ('ram_pct',        85.0),
    ('disk_pct',       90.0),
    ('swap_pct',       80.0),
    ('url_latency_ms', 5000.0);
`

// Alert represents a fired threshold breach.
type Alert struct {
	ID           int64
	Kind         string
	Message      string
	Value        float64
	Threshold    float64
	FiredAt      time.Time
	ResolvedAt   *time.Time
	Acknowledged bool
}

// URLCheck is a user-defined URL health check target.
type URLCheck struct {
	ID              int64
	URL             string
	Label           string
	IntervalSeconds int
	TimeoutSeconds  int
	Enabled         bool
	CreatedAt       time.Time
}

// URLResult is a single check result for a URLCheck.
type URLResult struct {
	ID         int64
	CheckID    int64
	CheckedAt  time.Time
	Up         bool
	StatusCode *int
	LatencyMS  *int
	Error      *string
}

// SSHEvent is a parsed SSH login event from auth.log.
type SSHEvent struct {
	ID         int64
	OccurredAt time.Time
	EventType  string // "success", "failed", "invalid_user"
	Username   *string
	SourceIP   string
	Port       *string
}

// TunnelEvent records a cloudflared state change.
type TunnelEvent struct {
	ID         int64
	OccurredAt time.Time
	EventType  string // "connected", "reconnecting", "disconnected"
	Detail     *string
}

// IPCount is an IP address with an occurrence count.
type IPCount struct {
	IP    string
	Count int
}
