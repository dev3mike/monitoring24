package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// DB wraps a SQLite connection.
type DB struct {
	conn *sql.DB
}

// Open opens (or creates) the SQLite database at path and applies the schema.
func Open(path string) (*DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode=WAL&_pragma=foreign_keys=ON&_pragma=busy_timeout=5000", path)
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	conn.SetMaxOpenConns(1) // SQLite is single-writer; WAL handles reads
	db := &DB{conn: conn}
	if err := db.migrate(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}

func (db *DB) migrate() error {
	_, err := db.conn.Exec(schemaSQL)
	return err
}

func (db *DB) Close() error {
	return db.conn.Close()
}

// Purge deletes old records to keep the database lean.
func (db *DB) Purge(ctx context.Context) error {
	now := time.Now().Unix()
	stmts := []string{
		fmt.Sprintf("DELETE FROM url_check_results WHERE checked_at < %d", now-7*86400),
		fmt.Sprintf("DELETE FROM ssh_events WHERE occurred_at < %d", now-30*86400),
		fmt.Sprintf("DELETE FROM tunnel_events WHERE occurred_at < %d", now-30*86400),
		fmt.Sprintf("DELETE FROM alerts WHERE resolved_at IS NOT NULL AND resolved_at < %d", now-14*86400),
	}
	for _, s := range stmts {
		if _, err := db.conn.ExecContext(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

// ── Alerts ──────────────────────────────────────────────────────────────────

func (db *DB) InsertAlert(a Alert) (int64, error) {
	res, err := db.conn.Exec(
		`INSERT INTO alerts(kind,message,value,threshold,fired_at) VALUES(?,?,?,?,?)`,
		a.Kind, a.Message, a.Value, a.Threshold, a.FiredAt.Unix(),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (db *DB) ResolveAlert(id int64, t time.Time) error {
	_, err := db.conn.Exec(`UPDATE alerts SET resolved_at=? WHERE id=?`, t.Unix(), id)
	return err
}

func (db *DB) AcknowledgeAlert(id int64) error {
	_, err := db.conn.Exec(`UPDATE alerts SET acknowledged=1 WHERE id=?`, id)
	return err
}

func (db *DB) ActiveAlerts() ([]Alert, error) {
	rows, err := db.conn.Query(
		`SELECT id,kind,message,value,threshold,fired_at,acknowledged FROM alerts WHERE resolved_at IS NULL ORDER BY fired_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAlerts(rows)
}

func (db *DB) RecentAlerts(limit int) ([]Alert, error) {
	rows, err := db.conn.Query(
		`SELECT id,kind,message,value,threshold,fired_at,acknowledged FROM alerts ORDER BY fired_at DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAlerts(rows)
}

func scanAlerts(rows *sql.Rows) ([]Alert, error) {
	var out []Alert
	for rows.Next() {
		var a Alert
		var firedAt int64
		var ack int
		if err := rows.Scan(&a.ID, &a.Kind, &a.Message, &a.Value, &a.Threshold, &firedAt, &ack); err != nil {
			return nil, err
		}
		a.FiredAt = time.Unix(firedAt, 0)
		a.Acknowledged = ack == 1
		out = append(out, a)
	}
	return out, rows.Err()
}

// ── URL Checks ───────────────────────────────────────────────────────────────

func (db *DB) InsertURLCheck(u URLCheck) (int64, error) {
	enabled := 0
	if u.Enabled {
		enabled = 1
	}
	res, err := db.conn.Exec(
		`INSERT INTO url_checks(url,label,interval_seconds,timeout_seconds,enabled,created_at) VALUES(?,?,?,?,?,?)`,
		u.URL, u.Label, u.IntervalSeconds, u.TimeoutSeconds, enabled, u.CreatedAt.Unix(),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (db *DB) UpdateURLCheck(u URLCheck) error {
	enabled := 0
	if u.Enabled {
		enabled = 1
	}
	_, err := db.conn.Exec(
		`UPDATE url_checks SET url=?,label=?,interval_seconds=?,timeout_seconds=?,enabled=? WHERE id=?`,
		u.URL, u.Label, u.IntervalSeconds, u.TimeoutSeconds, enabled, u.ID,
	)
	return err
}

func (db *DB) DeleteURLCheck(id int64) error {
	_, err := db.conn.Exec(`DELETE FROM url_checks WHERE id=?`, id)
	return err
}

func (db *DB) ListURLChecks() ([]URLCheck, error) {
	rows, err := db.conn.Query(
		`SELECT id,url,label,interval_seconds,timeout_seconds,enabled,created_at FROM url_checks ORDER BY created_at ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []URLCheck
	for rows.Next() {
		var u URLCheck
		var enabled int
		var createdAt int64
		if err := rows.Scan(&u.ID, &u.URL, &u.Label, &u.IntervalSeconds, &u.TimeoutSeconds, &enabled, &createdAt); err != nil {
			return nil, err
		}
		u.Enabled = enabled == 1
		u.CreatedAt = time.Unix(createdAt, 0)
		out = append(out, u)
	}
	return out, rows.Err()
}

func (db *DB) GetURLCheck(id int64) (URLCheck, error) {
	var u URLCheck
	var enabled int
	var createdAt int64
	err := db.conn.QueryRow(
		`SELECT id,url,label,interval_seconds,timeout_seconds,enabled,created_at FROM url_checks WHERE id=?`, id,
	).Scan(&u.ID, &u.URL, &u.Label, &u.IntervalSeconds, &u.TimeoutSeconds, &enabled, &createdAt)
	if err != nil {
		return u, err
	}
	u.Enabled = enabled == 1
	u.CreatedAt = time.Unix(createdAt, 0)
	return u, nil
}

func (db *DB) InsertURLResult(r URLResult) error {
	up := 0
	if r.Up {
		up = 1
	}
	_, err := db.conn.Exec(
		`INSERT INTO url_check_results(check_id,checked_at,up,status_code,latency_ms,error) VALUES(?,?,?,?,?,?)`,
		r.CheckID, r.CheckedAt.Unix(), up, r.StatusCode, r.LatencyMS, r.Error,
	)
	return err
}

func (db *DB) URLResultHistory(checkID int64, limit int) ([]URLResult, error) {
	rows, err := db.conn.Query(
		`SELECT id,check_id,checked_at,up,status_code,latency_ms,error FROM url_check_results WHERE check_id=? ORDER BY checked_at DESC LIMIT ?`,
		checkID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []URLResult
	for rows.Next() {
		var r URLResult
		var checkedAt int64
		var up int
		if err := rows.Scan(&r.ID, &r.CheckID, &checkedAt, &up, &r.StatusCode, &r.LatencyMS, &r.Error); err != nil {
			return nil, err
		}
		r.CheckedAt = time.Unix(checkedAt, 0)
		r.Up = up == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

func (db *DB) URLUptime(checkID int64, since time.Time) (float64, error) {
	var pct sql.NullFloat64
	err := db.conn.QueryRow(
		`SELECT CAST(SUM(up) AS REAL) * 100.0 / COUNT(*) FROM url_check_results WHERE check_id=? AND checked_at >= ?`,
		checkID, since.Unix(),
	).Scan(&pct)
	if err != nil {
		return 0, err
	}
	return pct.Float64, nil
}

// ── SSH Events ────────────────────────────────────────────────────────────────

func (db *DB) InsertSSHEvent(e SSHEvent) error {
	_, err := db.conn.Exec(
		`INSERT INTO ssh_events(occurred_at,event_type,username,source_ip,port) VALUES(?,?,?,?,?)`,
		e.OccurredAt.Unix(), e.EventType, e.Username, e.SourceIP, e.Port,
	)
	return err
}

func (db *DB) RecentSSHEvents(limit int) ([]SSHEvent, error) {
	rows, err := db.conn.Query(
		`SELECT id,occurred_at,event_type,username,source_ip,port FROM ssh_events ORDER BY occurred_at DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SSHEvent
	for rows.Next() {
		var e SSHEvent
		var ts int64
		if err := rows.Scan(&e.ID, &ts, &e.EventType, &e.Username, &e.SourceIP, &e.Port); err != nil {
			return nil, err
		}
		e.OccurredAt = time.Unix(ts, 0)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (db *DB) FailedSSHByIP(since time.Time) ([]IPCount, error) {
	rows, err := db.conn.Query(
		`SELECT source_ip, COUNT(*) as cnt FROM ssh_events WHERE event_type='failed' AND occurred_at >= ? GROUP BY source_ip ORDER BY cnt DESC LIMIT 20`,
		since.Unix(),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IPCount
	for rows.Next() {
		var ic IPCount
		if err := rows.Scan(&ic.IP, &ic.Count); err != nil {
			return nil, err
		}
		out = append(out, ic)
	}
	return out, rows.Err()
}

// ── Tunnel Events ─────────────────────────────────────────────────────────────

func (db *DB) InsertTunnelEvent(e TunnelEvent) error {
	_, err := db.conn.Exec(
		`INSERT INTO tunnel_events(occurred_at,event_type,detail) VALUES(?,?,?)`,
		e.OccurredAt.Unix(), e.EventType, e.Detail,
	)
	return err
}

func (db *DB) RecentTunnelEvents(limit int) ([]TunnelEvent, error) {
	rows, err := db.conn.Query(
		`SELECT id,occurred_at,event_type,detail FROM tunnel_events ORDER BY occurred_at DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TunnelEvent
	for rows.Next() {
		var e TunnelEvent
		var ts int64
		if err := rows.Scan(&e.ID, &ts, &e.EventType, &e.Detail); err != nil {
			return nil, err
		}
		e.OccurredAt = time.Unix(ts, 0)
		out = append(out, e)
	}
	return out, rows.Err()
}

// ── Thresholds ────────────────────────────────────────────────────────────────

func (db *DB) GetThresholds() (map[string]float64, error) {
	rows, err := db.conn.Query(`SELECT key,value FROM thresholds`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]float64)
	for rows.Next() {
		var k string
		var v float64
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

func (db *DB) SetThreshold(key string, value float64) error {
	_, err := db.conn.Exec(`INSERT INTO thresholds(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}
