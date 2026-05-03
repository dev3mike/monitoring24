## Project Overview

monitoring24 is a lightweight real-time server monitoring dashboard written in Go. It builds into a single self-contained binary with embedded static web assets and stores state in SQLite under the configured data directory. The app is intended for Linux/Ubuntu servers, often behind Cloudflare Tunnel, but most collectors degrade gracefully on macOS, Windows, or non-systemd systems.

Core capabilities:

- System metrics: CPU, per-core CPU, load average, memory, swap, disk usage, disk I/O, network I/O, uptime.
- App/process metrics for the monitoring24 process.
- Live browser updates through Server-Sent Events every collector tick.
- Cloudflare Tunnel process detection and transition events.
- systemd service status monitoring.
- SSH auth-log parsing and pending apt update counts.
- User-managed URL health checks with result history and 24h uptime.
- Threshold alerts persisted in SQLite and auto-resolved when conditions recover.

## Repository Layout

- `cmd/server/main.go`: application entrypoint, dependency wiring, background loops, HTTP server, graceful shutdown.
- `internal/config`: CLI flag parsing, basic-auth hashing, data-directory creation.
- `internal/http`: API route setup, middleware, handlers, and in-memory latest-snapshot cache (Go package `httpserver`).
- `internal/metrics`: system, app, and network collectors using `gopsutil`.
- `internal/storage`: SQLite schema, migration, and data access methods.
- `internal/alerts`: threshold evaluation and active alert lifecycle.
- `internal/urlcheck`: per-URL monitor goroutines, HTTP checks, result cache, SQLite persistence.
- `internal/sse`: typed SSE broker with client registration, broadcast queues, and heartbeat.
- `internal/tunnel`: `cloudflared` process/version detection and tunnel event persistence.
- `internal/security`: auth-log parser and apt pending-update checks.
- `internal/services`: systemd service status collection via `systemctl show`.
- `web`: embedded UI assets. `web/assets.go` exposes `embed.FS`; `index.html`, `static/app.js`, and `static/style.css` implement the dashboard.
- `tests`: black-box tests against public packages for storage, alerts, URL checks, HTTP routes, SSE, auth-line parsing, and tunnel argv parsing (`package tests`).
- `.github/workflows/release.yml`: cross-platform GitHub release workflow.
- `.air.toml`: live-reload config for `make dev`.
- `monitoring24.service`: production systemd unit.

Ignored/generated paths include `data/`, `tmp/`, built binaries such as `monitoring24`, and release binaries matching `monitoring24-*`.

## Runtime Architecture

Startup flow in `cmd/server/main.go`:

1. Parse CLI flags into `config.Config`.
2. Open `DATA_DIR/monitor.db` and run SQLite schema creation.
3. Construct collectors and subsystems: metrics, alerts, URL checker, tunnel detector, security monitor, service monitor, SSE broker, HTTP handler.
4. Load alert thresholds and restore active alerts from SQLite.
5. Start URL check goroutines for enabled persisted checks.
6. Start background goroutines:
   - SSE broker loop.
   - collector loop.
   - periodic purge loop.
   - URL-result-to-SSE relay.
7. Serve embedded web assets and JSON/SSE routes.
8. On SIGINT/SIGTERM, close the broker, cancel contexts, and gracefully shut down HTTP.

Collector cadence:

- Fast tick: every 2 seconds.
  - Collects system metrics, app metrics, network connections, and tunnel status.
  - Evaluates CPU/RAM/swap/disk/URL-down alerts.
  - Broadcasts `metrics`, `tunnel`, and newly fired `alert` SSE events.
  - Updates the handler's latest in-memory snapshot.
- Slow tick: every 30 seconds.
  - Parses auth logs, builds security snapshot, checks systemd services.
  - Broadcasts `services` over SSE (there is no `security` SSE event type).
  - Updates latest security/service snapshots in the handler cache (`GET /api/security` reflects this; the current UI loads that endpoint once at startup, not on SSE).
- Purge tick: every 6 hours.
  - Deletes old URL results, SSH events, tunnel events, and resolved alerts.

## API And Event Contract

REST routes are defined in `internal/http/routes.go`:

- `GET /api/metrics`: latest system/app/network snapshot.
- `GET /api/alerts`: active and recent alerts.
- `POST /api/alerts/{id}/acknowledge`: marks an alert acknowledged.
- `GET /api/url-checks`: URL check summaries.
- `POST /api/url-checks`: create a URL check.
- `GET|PUT|DELETE /api/url-checks/{id}`: read, update, or delete one URL check.
- `GET /api/url-checks/{id}/history?limit=N`: recent URL check results.
- `GET|PUT /api/thresholds`: read or update alert thresholds.
- `GET /api/services`: latest service snapshot.
- `GET /api/security`: latest security snapshot.
- `GET /api/tunnel`: latest tunnel snapshot.
- `GET /events`: SSE stream.

SSE event types:

- `metrics`: `{ system, app, network }`
- `tunnel`: Cloudflare Tunnel status.
- `services`: systemd service snapshot.
- `alert`: a newly fired alert.
- `url_result`: latest URL check result.
- `heartbeat`: empty object every 30 seconds.

The frontend performs an initial REST load, then relies on SSE for live updates to metrics, tunnel, services, alerts, and URL check results. If SSE drops, `web/static/app.js` reconnects after 5 seconds.

## Storage Model

SQLite is opened with WAL mode, foreign keys enabled, a 5 second busy timeout, and `SetMaxOpenConns(1)`.

Tables:

- `alerts`: threshold breaches, resolved time, acknowledgement flag.
- `url_checks`: user-defined monitors with interval, timeout, enabled flag.
- `url_check_results`: per-check history with cascade delete.
- `ssh_events`: parsed SSH success/failure/invalid-user events.
- `tunnel_events`: cloudflared connected/disconnected events.
- `thresholds`: alert thresholds seeded on migration.

Default thresholds:

- `cpu_pct`: 90
- `ram_pct`: 85
- `disk_pct`: 90
- `swap_pct`: 80
- `url_latency_ms`: 5000

Note: `url_latency_ms` is exposed in settings and persisted, but the current alert evaluator only checks CPU, RAM, swap, disk usage, and URL down status. Slow URL latency is not currently evaluated as an alert condition.

## Important Conventions

- Keep the app as a single Go binary with embedded frontend assets.
- Prefer graceful degradation for host-specific integrations. Missing `systemctl`, auth logs, `apt-get`, network permissions, or `cloudflared` should not crash the app.
- Use `context.Context` for collectors and subprocesses.
- Keep long-running work in background goroutines controlled by cancellation or stop channels.
- Store durable operational state in SQLite; cache only latest snapshots and URL summaries in memory.
- Use `net/http` standard library routing with method/path patterns.
- If basic auth is enabled, all API routes and `/events` are protected. Static assets are currently served without the auth wrapper.
- JSON output is mixed:
  - Many snapshot structs have lower-case `json` tags.
  - Storage structs such as `storage.Alert` and `storage.URLCheck` do not have JSON tags, so Go emits exported field names like `ID`, `FiredAt`, and `CreatedAt`.
  - The frontend intentionally accepts both lower-case and exported Go field names for alerts and URL data.
- UI state such as theme and pinned dashboard URL IDs is stored in browser `localStorage`.

## Security And Ownership

- Treat monitoring24 as a host-observability tool with access to sensitive operational data. Avoid logging secrets, basic-auth credentials, URLs with embedded credentials, auth-log contents beyond parsed event fields, or raw command output unless explicitly needed.
- Preserve the basic-auth behavior: plaintext passwords are accepted only through `--basic-auth user:password`, immediately bcrypt-hashed in memory, and never persisted.
- When public exposure is involved, prefer `--host 127.0.0.1` behind Cloudflare Tunnel or a trusted reverse proxy, plus `--basic-auth`.
- Keep `/events` protected whenever auth is enabled. If changing route wrapping, verify API and SSE auth behavior together.
- Static assets are currently served outside the auth wrapper. Do not assume this protects the UI shell; protection is on data/API/SSE routes.
- Do not expand filesystem privileges casually. Runtime writes should stay under the configured `--data-dir`, and production writes must be compatible with `monitoring24.service` `ReadWritePaths=/var/lib/monitoring24`.
- Do not require root. The production unit uses a dedicated `monitoring24` user and `SupplementaryGroups=adm` only to read Debian/Ubuntu auth logs.
- Be careful with ownership in deployment instructions: `/usr/local/bin/monitoring24` should be executable by the service user, and `/var/lib/monitoring24` should be owned by `monitoring24:monitoring24`.
- Do not commit or edit live runtime data in `data/`, SQLite WAL/SHM files, generated binaries, or `tmp/` artifacts.
- Treat host command integrations as untrusted boundaries. Use fixed command names/arguments where possible, context timeouts/cancellation, and avoid shell interpolation for user-controlled values.
- URL checks issue outbound GET requests for user-provided URLs. Consider SSRF risk before adding features that expose response bodies, internal metadata, custom headers, proxy support, or privileged network access.
- Auth-log parsing can expose IP addresses and usernames. Keep UI/API responses limited to the fields already modeled unless there is a clear product need.
- When adding new stored data, consider retention and purge behavior. Extend `DB.Purge` for high-volume or sensitive records.

## Development Workflow

Requirements:

- Go 1.25 or newer, matching `go.mod`.
- Linux gives the fullest runtime behavior. macOS/Windows are acceptable for build/UI work, with expected collector gaps.

Common commands:

```bash
go test ./...
make test
go vet ./...
go build -o monitoring24 ./cmd/server
make build
make run
make run-auth
make dev
make cross-linux
make cross-linux-arm64
make cross-windows
make cross-all
make clean
```

`make dev` uses Air and `.air.toml`. If Air is not installed, the Makefile installs `github.com/air-verse/air@latest`. It rebuilds to `tmp/monitoring24-dev` on Go, HTML, CSS, or JS changes and excludes `tmp`, `data`, `.git`, and `vendor`.

Default local run:

```bash
./monitoring24
```

Then open `http://localhost:47291`.

Useful flags:

```bash
./monitoring24 --host 127.0.0.1 --port 47291
./monitoring24 --basic-auth admin:changeme
./monitoring24 --data-dir /var/lib/monitoring24
./monitoring24 --services nginx,postgresql,redis,myapp
```

## Deployment Notes

The provided systemd unit runs as user/group `monitoring24`, binds to `127.0.0.1:47291`, stores data in `/var/lib/monitoring24`, and adds supplementary group `adm` so Debian/Ubuntu auth logs are readable.

Typical production posture:

- Run behind Cloudflare Tunnel or another reverse proxy.
- Bind the app to `127.0.0.1`.
- Enable `--basic-auth` if exposed beyond a private boundary.
- Disable reverse-proxy buffering for `/events`; the app sets `X-Accel-Buffering: no` for SSE.

The GitHub Actions workflow builds Linux amd64/arm64, Windows amd64, and macOS amd64/arm64 artifacts. Pushes to `main` update the moving prerelease `main-build`; tags matching `v*` create stable releases, and `workflow_dispatch` can publish stable or prerelease builds with an optional version. `make cross-all` only cross-compiles Linux amd64/arm64 and Windows amd64—macOS release binaries come from CI (or ad hoc `GOOS=darwin` builds), not from that Makefile aggregate target.

## Dependencies And Integrations

Direct Go dependencies:

- `github.com/shirou/gopsutil/v3`: system, process, network, disk, host, load metrics.
- `modernc.org/sqlite`: pure-Go SQLite driver, enabling CGO-free builds.
- `golang.org/x/crypto/bcrypt`: basic-auth password hashing.

External host commands/integrations:

- `systemctl show`: service monitoring.
- `cloudflared --version`: one-time version detection when cloudflared is running.
- Process scanning via `gopsutil/process`: cloudflared process detection.
- `/var/log/auth.log` or `/var/log/secure`: SSH event parsing.
- `apt-get --simulate -qq upgrade`: pending update counts on apt-based systems.
- Browser CDN requests from `web/index.html`: Tailwind CDN and Google Fonts. The Go binary embeds the local HTML/CSS/JS, but the UI still references those external frontend resources at runtime.

## Common Tasks

Add or change an API endpoint:

1. Add handler logic in `internal/http/handlers.go`.
2. Register the route in `internal/http/routes.go`.
3. Update `web/static/app.js` if the UI consumes it.
4. Keep responses JSON and use `writeJSON`/`writeError`.

Add a new collected metric:

1. Add fields with JSON tags to the relevant snapshot struct in `internal/metrics`.
2. Populate fields in the collector.
3. Include it in `LatestData` or existing broadcast payloads if needed.
4. Render it in `web/static/app.js` and markup/CSS as needed.

Add a new persistent setting or entity:

1. Extend `schemaSQL` in `internal/storage/schema.go` using `CREATE TABLE IF NOT EXISTS`, `CREATE INDEX IF NOT EXISTS`, or compatible `INSERT OR IGNORE` patterns.
2. Add typed methods in `internal/storage/sqlite.go`.
3. Wire through handlers and frontend state.

Change alert behavior:

1. Update threshold seeding in `internal/storage/schema.go` if needed.
2. Load/update thresholds through `alerts.Engine`.
3. Modify `Evaluate` in `internal/alerts/alerts.go`.
4. Ensure active alert keys are stable; keys are used to deduplicate and resolve alerts.

Change URL check behavior:

1. Work in `internal/urlcheck/checker.go`.
2. Preserve per-check goroutine lifecycle: `Add` starts enabled checks, `Update` stops then restarts if enabled, `Remove` stops and deletes.
3. Keep `ResultCh` non-blocking so slow SSE consumers do not stall checks.

Change the dashboard:

1. Edit `web/index.html`, `web/static/app.js`, and/or `web/static/style.css`.
2. No bundler is used. Static assets are embedded by `go:embed`.
3. Remember `index.html` loads Tailwind from CDN at runtime.

## Gotchas

- Automated tests live under `tests/` (`package tests`): SQLite storage, alert engine, URL checker against `httptest` servers, HTTP+SSE wiring, `security.ParseAuthLogLine`, and `tunnel.TunnelNameFromArgs`. `make test` runs `go test ./... -race -count=1`. Host-dependent code (`config.ParseFlags` with global `flag` state, gopsutil collectors, `tunnel.Detector.Collect`, `services.Monitor.Collect`, incremental auth-log file parsing, and `main`) is not covered end-to-end; exercise those manually on a target OS.
- The fast collector calls `cpu.PercentWithContext(ctx, time.Second, false)` inside a 2-second tick, so each fast loop includes a 1-second CPU sampling delay.
- First disk/network I/O samples may be empty because rates require a previous counter snapshot.
- `storage.URLUptime` returns `0` when no rows exist because it ignores `sql.NullFloat64.Valid`; this means new checks may display 0% until results arrive.
- URL checks treat HTTP status codes below 400 as up and follow at most 5 redirects.
- URL monitor URLs are unique in SQLite.
- Acknowledging an alert only sets `acknowledged=1`; the UI also removes it locally. Active unacknowledged/resolved state is still governed by `resolved_at` and alert evaluation.
- `alerts.Engine.Evaluate` holds its mutex while writing to SQLite. Keep new alert checks lightweight.
- The security parser tracks byte offset only in memory. On restart it reparses the current auth log from the beginning, which can create duplicate SSH event rows.
- Auth-log timestamp parsing uses the current year because syslog lines do not include a year.
- `checkPendingUpdates` counts every output line from `apt-get --simulate -qq upgrade`; this is a rough package/update signal, not a full package manager model.
- `tunnel.TunnelNameFromArgs` (used when cloudflared is running) only recognizes `--name NAME` or `run NAME` in argv.
- The Makefile injects `-X main.version=$(VERSION)`, but the current code does not expose or use a `version` variable.
- `--log-level` is parsed but not currently used to filter logs.
- `monitoring24.service` uses `ProtectSystem=strict` and `ReadWritePaths=/var/lib/monitoring24`; any new production write path must be reflected there.
