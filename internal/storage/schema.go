package storage

import (
	"database/sql"
	"time"
)

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

CREATE TABLE IF NOT EXISTS metric_snapshots (
    ts               INTEGER PRIMARY KEY,
    cpu_pct          REAL    NOT NULL,
    ram_pct          REAL    NOT NULL,
    swap_pct         REAL    NOT NULL,
    ram_used         INTEGER NOT NULL,
    ram_total        INTEGER NOT NULL,
    disk_json        TEXT    NOT NULL,
    net_json         TEXT    NOT NULL,
    disk_agg_pct     REAL    NOT NULL DEFAULT 0,
    disk_agg_used    INTEGER NOT NULL DEFAULT 0,
    disk_agg_total   INTEGER NOT NULL DEFAULT 0
);
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

// MetricSnapshot is one minute of aggregated system metrics stored in SQLite.
type MetricSnapshot struct {
	TS           int64
	CPUPct       float64
	RAMPct       float64
	SwapPct      float64
	RAMUsed      uint64
	RAMTotal     uint64
	Disks        []DiskSample
	NetIfaces    []NetSample
	DiskAggPct   float64 // persisted combined used/total % (see CombinedDiskUsagePct)
	DiskAggUsed  uint64  // bytes; sum(used) across mounts at snapshot time
	DiskAggTotal uint64  // bytes; sum(total) across mounts at snapshot time
}

// DiskSample is a per-mountpoint aggregated disk metric within a MetricSnapshot.
type DiskSample struct {
	Mountpoint string  `json:"mountpoint"`
	Total      uint64  `json:"total"`
	Used       uint64  `json:"used"`
	Percent    float64 `json:"percent"`
	FsType     string  `json:"fs_type"`
}

// NetSample is a per-interface aggregated network metric within a MetricSnapshot.
type NetSample struct {
	Interface    string `json:"interface"`
	BytesSentSec uint64 `json:"bytes_sent_sec"`
	BytesRecvSec uint64 `json:"bytes_recv_sec"`
}

// MetricHistoryBucket is one SQL-aggregated time bucket (e.g. AVG(cpu_pct) per step).
// AuxUsedAvg/AuxTotalAvg are averaged byte counters within the bucket when applicable (RAM/disk history).
type MetricHistoryBucket struct {
	BucketTS    int64
	Value       float64
	AuxUsedAvg  sql.NullFloat64
	AuxTotalAvg sql.NullFloat64
}
