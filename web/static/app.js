'use strict';

// ── State ──────────────────────────────────────────────────────────────────
const state = {
  system: null,
  app: null,
  network: null,
  tunnel: null,
  services: null,
  security: null,
  urls: [],
  alerts: { active: [], recent: [] },
  thresholds: {},
  sseConnected: false,
  lastUpdate: null,
  cpuHistory: [],
  ramHistory: [],
  netSentHistory: [],
  netRecvHistory: [],
  dashboardURLIDs: new Set(),
};

const MAX_HISTORY = 60; // data points for sparklines
let sparklineSeq = 0;

// ── DOM helpers ────────────────────────────────────────────────────────────
function el(id) { return document.getElementById(id); }
function setText(id, v) { const e = el(id); if (e) e.textContent = v ?? '—'; }
function show(id) { const e = el(id); if (e) e.classList.remove('hidden'); }
function hide(id) { const e = el(id); if (e) e.classList.add('hidden'); }

// ── Formatting ─────────────────────────────────────────────────────────────
function fmtBytes(b) {
  if (b == null) return '—';
  if (b >= 1e9) return (b / 1e9).toFixed(1) + ' GB';
  if (b >= 1e6) return (b / 1e6).toFixed(1) + ' MB';
  if (b >= 1e3) return (b / 1e3).toFixed(1) + ' KB';
  return b + ' B';
}

function fmtUptime(seconds) {
  if (!seconds) return '—';
  const d = Math.floor(seconds / 86400);
  const h = Math.floor((seconds % 86400) / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  if (d > 0) return `${d}d ${h}h ${m}m`;
  if (h > 0) return `${h}h ${m}m`;
  return `${m}m`;
}

function fmtDate(iso) {
  if (!iso) return '—';
  return new Date(iso).toLocaleString();
}

function fmtPct(v) {
  if (v == null) return '—';
  return v.toFixed(1) + '%';
}

function fmtLatencyTV(ms) {
  if (ms == null) return '—ms';
  if (ms < 1000) return `${Math.round(ms)}ms`;
  const sec = ms / 1000;
  return sec >= 10 ? `${sec.toFixed(0)}s` : `${sec.toFixed(1)}s`;
}

function fmtStatusCodeTV(code) {
  if (code == null) return '—';
  return String(code);
}

function fmtUptimeTV(pct) {
  if (pct == null) return '—%';
  const digits = pct >= 99 ? 2 : 1;
  return `${pct.toFixed(digits)}%`;
}

function fmtClock(date) {
  if (!date) return '—';
  return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });
}

function alertID(a) {
  return a?.id ?? a?.ID;
}

function alertMessage(a) {
  return a?.message ?? a?.Message ?? 'Alert threshold breached';
}

function alertFiredAt(a) {
  return a?.fired_at ?? a?.FiredAt;
}

function urlCheckID(u) {
  return u?.id ?? u?.ID;
}

function urlCheckURL(u) {
  return u?.url ?? u?.URL ?? '';
}

function urlCheckLabel(u) {
  return u?.label ?? u?.Label ?? '';
}

function urlCheckEnabled(u) {
  return u?.enabled ?? u?.Enabled ?? true;
}

function urlCheckInterval(u) {
  return u?.interval_seconds ?? u?.IntervalSeconds ?? '—';
}

function urlCheckLastResult(u) {
  return u?.last_result ?? u?.LastResult ?? null;
}

function urlCheckUptime(u) {
  return u?.uptime_pct_24h ?? u?.UptimePct;
}

function urlResultUp(r) {
  return r?.up ?? r?.Up;
}

function urlResultLatency(r) {
  return r?.latency_ms ?? r?.LatencyMS;
}

function urlResultStatusCode(r) {
  return r?.status_code ?? r?.StatusCode;
}

function urlResultCheckedAt(r) {
  return r?.checked_at ?? r?.CheckedAt;
}

function urlResultError(r) {
  return r?.error ?? r?.Error;
}

function loadDisplayPrefs() {
  try {
    const ids = JSON.parse(localStorage.getItem('dashboardURLIDs') || '[]');
    state.dashboardURLIDs = new Set(ids.map(Number).filter(Number.isFinite));
  } catch {
    state.dashboardURLIDs = new Set();
  }

}

function saveDashboardURLPrefs() {
  localStorage.setItem('dashboardURLIDs', JSON.stringify([...state.dashboardURLIDs]));
}

function isURLOnDashboard(id) {
  return state.dashboardURLIDs.has(Number(id));
}

function setDashboardURL(id, pinned) {
  const numericID = Number(id);
  if (!Number.isFinite(numericID)) return;
  if (pinned) {
    state.dashboardURLIDs.add(numericID);
  } else {
    state.dashboardURLIDs.delete(numericID);
  }
  saveDashboardURLPrefs();
  renderURLs();
  renderDashboardURLs();
}

// ── Gauge color ────────────────────────────────────────────────────────────
function gaugeColor(pct) {
  if (pct >= 90) return 'bg-red-500';
  if (pct >= 70) return 'bg-yellow-500';
  return 'bg-green-500';
}

function gaugeCls(pct) {
  return `h-2 rounded-full transition-all duration-500 ${gaugeColor(pct)} ${pct >= 90 ? 'bar-danger' : ''}`;
}

function textColor(pct) {
  if (pct >= 90) return 'text-red-500';
  if (pct >= 70) return 'text-yellow-500';
  return 'text-green-500';
}

// ── Sparkline SVG ──────────────────────────────────────────────────────────
function sparkline(data, color = '#6366f1', height = 32) {
  if (!data || data.length < 2) return '';
  const w = 200, h = height;
  const gradId = `sg-${color.replace('#','')}-${sparklineSeq++}`;
  const max = Math.max(...data, 1);
  const pts = data.map((v, i) => {
    const x = (i / (data.length - 1)) * w;
    const y = h - (v / max) * h;
    return `${x},${y}`;
  });
  const area = `M ${pts[0]} ` + pts.slice(1).map(p => `L ${p}`).join(' ') +
    ` L ${w},${h} L 0,${h} Z`;
  const line = `M ${pts[0]} ` + pts.slice(1).map(p => `L ${p}`).join(' ');
  return `<svg class="sparkline" viewBox="0 0 ${w} ${h}" preserveAspectRatio="none" style="overflow:hidden">
    <defs><linearGradient id="${gradId}" x1="0" y1="0" x2="0" y2="1">
      <stop offset="0%" stop-color="${color}" stop-opacity="0.3"/>
      <stop offset="100%" stop-color="${color}" stop-opacity="0.02"/>
    </linearGradient></defs>
    <path d="${area}" fill="url(#${gradId})" />
    <path d="${line}" fill="none" stroke="${color}" stroke-width="2" vector-effect="non-scaling-stroke"/>
  </svg>`;
}

// ── Render: System ─────────────────────────────────────────────────────────
function renderSystem() {
  const s = state.system;
  if (!s) return;

  // CPU
  const cpu = s.cpu_percent || 0;
  setText('cpu-pct', fmtPct(cpu));
  const cpuBar = el('cpu-bar');
  if (cpuBar) { cpuBar.style.width = cpu + '%'; cpuBar.className = gaugeCls(cpu); }
  const cpuText = el('cpu-text');
  if (cpuText) cpuText.className = `text-3xl font-bold tabular-nums ${textColor(cpu)}`;
  setText('load-avg', `${s.load_avg_1?.toFixed(2)} / ${s.load_avg_5?.toFixed(2)} / ${s.load_avg_15?.toFixed(2)}`);

  // RAM
  const ram = s.mem_percent || 0;
  setText('ram-pct', fmtPct(ram));
  setText('ram-used', `${fmtBytes(s.mem_used)} / ${fmtBytes(s.mem_total)}`);
  const ramBar = el('ram-bar');
  if (ramBar) { ramBar.style.width = ram + '%'; ramBar.className = gaugeCls(ram); }

  // Swap
  const swap = s.swap_percent || 0;
  setText('swap-pct', fmtPct(swap));
  setText('swap-used', `${fmtBytes(s.swap_used)} / ${fmtBytes(s.swap_total)}`);
  const swapBar = el('swap-bar');
  if (swapBar) { swapBar.style.width = swap + '%'; swapBar.className = gaugeCls(swap); }

  // Uptime
  setText('uptime', fmtUptime(s.uptime_seconds));
  setText('status-uptime', fmtUptime(s.uptime_seconds));

  // Summary cards
  setText('summary-cpu', fmtPct(cpu));
  setText('summary-ram', fmtPct(ram));

  // Disks
  renderDisks(s.disks || []);

  // Network IO
  renderNetworkIO(s.network_io || []);

  // Disk IO
  renderDiskIO(s.disk_io || []);

  // Per-core
  renderCoreGrid(s.cpu_per_core || []);

  // History
  pushHistory(state.cpuHistory, cpu);
  pushHistory(state.ramHistory, ram);
  renderSparkline('cpu-sparkline', state.cpuHistory, '#6366f1');
  renderSparkline('ram-sparkline', state.ramHistory, '#10b981');
}

function pushHistory(arr, val) {
  arr.push(val || 0);
  if (arr.length > MAX_HISTORY) arr.shift();
}

function renderSparkline(id, data, color) {
  const e = el(id);
  if (!e) return;
  const h = Math.max(16, e.clientHeight || 32);
  e.innerHTML = sparkline(data, color, h);
}

function renderDisks(disks) {
  const container = el('disk-list');
  if (!container) return;
  container.innerHTML = disks.map(d => `
    <div class="mb-3">
      <div class="flex justify-between text-sm mb-1">
        <span class="font-mono dark:text-slate-300 text-slate-700">${d.mountpoint}</span>
        <span class="${textColor(d.percent)} font-semibold">${fmtPct(d.percent)}</span>
      </div>
      <div class="w-full bg-slate-200 dark:bg-slate-700 rounded-full h-2">
        <div class="${gaugeCls(d.percent)}" style="width:${d.percent}%"></div>
      </div>
      <div class="flex justify-between text-xs mt-1 dark:text-slate-400 text-slate-500">
        <span>${fmtBytes(d.used)} used</span>
        <span>${fmtBytes(d.total)} total · ${d.fs_type}</span>
      </div>
    </div>`).join('') || '<p class="text-sm dark:text-slate-400 text-slate-500">No disk data</p>';
  setText('summary-disk', fmtPct(Math.max(0, ...(disks.map(d => d.percent)))));
}

function renderNetworkIO(ifaces) {
  const container = el('network-io-list');
  const detailContainer = el('network-io-list-2');
  if ((!container && !detailContainer) || !ifaces.length) {
    setText('summary-network', '—');
    setText('network-panel-rate', '—');
    if (container) container.innerHTML = '<p class="text-sm dark:text-slate-400 text-slate-500">No network data</p>';
    if (detailContainer) detailContainer.innerHTML = '<p class="text-sm dark:text-slate-400 text-slate-500">No network data</p>';
    return;
  }

  let totalSent = 0, totalRecv = 0;
  ifaces.forEach(i => { totalSent += i.bytes_sent_sec || 0; totalRecv += i.bytes_recv_sec || 0; });
  setText('summary-network', `${fmtBytes(totalRecv)}/s`);
  setText('network-panel-rate', `${fmtBytes(totalRecv)}/s`);
  pushHistory(state.netSentHistory, totalSent);
  pushHistory(state.netRecvHistory, totalRecv);
  renderSparkline('net-sparkline', state.netRecvHistory, '#f59e0b');

  const html = ifaces.map(i => `
    <div class="flex items-center justify-between py-1.5 border-b dark:border-slate-700 border-slate-200 last:border-0">
      <span class="font-mono text-sm dark:text-slate-300 text-slate-700">${i.interface}</span>
      <div class="text-xs text-right">
        <span class="text-green-500">↑ ${fmtBytes(i.bytes_sent_sec)}/s</span>
        <span class="mx-1 dark:text-slate-500 text-slate-400">|</span>
        <span class="text-blue-500">↓ ${fmtBytes(i.bytes_recv_sec)}/s</span>
      </div>
    </div>`).join('');
  if (container) container.innerHTML = html;
  if (detailContainer) detailContainer.innerHTML = html;
}

function renderDiskIO(ios) {
  const container = el('disk-io-list');
  if (!container) return;
  if (!ios || !ios.length) {
    container.innerHTML = '<p class="text-xs dark:text-slate-400 text-slate-500">No I/O data yet (appears after 2nd sample)</p>';
    return;
  }
  container.innerHTML = ios.map(d => `
    <div class="flex items-center justify-between py-1.5 border-b dark:border-slate-700 border-slate-200 last:border-0">
      <span class="font-mono text-sm dark:text-slate-300 text-slate-700">${d.device}</span>
      <div class="text-xs text-right">
        <span class="text-orange-500">R: ${fmtBytes(d.read_bytes_sec)}/s</span>
        <span class="mx-1 dark:text-slate-500 text-slate-400">|</span>
        <span class="text-purple-500">W: ${fmtBytes(d.write_bytes_sec)}/s</span>
      </div>
    </div>`).join('');
}

function renderCoreGrid(cores) {
  const container = el('cpu-cores');
  if (!container || !cores.length) return;
  container.innerHTML = cores.map((pct, i) => `
    <div class="text-center">
      <div class="text-xs dark:text-slate-400 text-slate-500 mb-1">C${i}</div>
      <div class="text-sm font-semibold ${textColor(pct)}">${pct.toFixed(0)}%</div>
      <div class="w-full bg-slate-200 dark:bg-slate-700 rounded-full h-1 mt-1">
        <div class="${gaugeCls(pct)}" style="width:${pct}%"></div>
      </div>
    </div>`).join('');
}

// ── Render: App Metrics ────────────────────────────────────────────────────
function renderApp() {
  const a = state.app;
  if (!a) return;
  setText('app-goroutines', a.goroutines);
  setText('app-heap', fmtBytes(a.heap_alloc_bytes));
  setText('app-uptime', fmtUptime(a.uptime_seconds));
  setText('app-cpu', fmtPct(a.process_cpu_percent));
  setText('app-mem', fmtBytes(a.process_mem_bytes));
  setText('app-files', a.open_file_count);
  setText('app-gc', a.num_gc);
}

// ── Render: Tunnel ─────────────────────────────────────────────────────────
function renderTunnel() {
  const t = state.tunnel;
  const card = el('dashboard-tunnel-card');
  const badge = el('tunnel-badge');
  const summaryBadge = el('summary-tunnel');
  const hasTunnel = !!(t && (t.running || t.tunnel_name || t.pid || t.version));

  if (card) card.classList.toggle('hidden', !hasTunnel);

  if (!t) {
    if (badge) badge.textContent = 'Unknown';
    if (summaryBadge) summaryBadge.textContent = 'Unknown';
    return;
  }

  const statusText = t.running ? 'Connected' : 'Disconnected';
  const badgeCls = t.running
    ? 'big-status text-green-500'
    : 'big-status text-red-500';
  const summaryCls = t.running
    ? 'px-2 py-0.5 rounded-full text-xs font-semibold bg-green-100 text-green-800 dark:bg-green-900/30 dark:text-green-400'
    : 'px-2 py-0.5 rounded-full text-xs font-semibold bg-red-100 text-red-800 dark:bg-red-900/30 dark:text-red-400';

  if (badge) { badge.textContent = statusText; badge.className = badgeCls; }
  if (summaryBadge) { summaryBadge.textContent = statusText; summaryBadge.className = `tile-status ${summaryCls}`; }

  setText('tunnel-uptime', fmtUptime(t.uptime_seconds));
  setText('tunnel-name', t.tunnel_name || '—');
  setText('tunnel-version', t.version || '—');
  setText('tunnel-pid', t.pid || '—');

  const eventsEl = el('tunnel-events');
  if (eventsEl && t.recent_events) {
    eventsEl.innerHTML = t.recent_events.map(e => `
      <div class="flex items-center gap-2 py-1.5 border-b dark:border-slate-700 border-slate-200 last:border-0 text-sm">
        <span class="w-2 h-2 rounded-full flex-shrink-0 ${e.event_type === 'connected' ? 'bg-green-500' : 'bg-red-500'}"></span>
        <span class="dark:text-slate-300 text-slate-700 capitalize">${e.event_type}</span>
        <span class="ml-auto dark:text-slate-400 text-slate-500 text-xs">${fmtDate(e.occurred_at)}</span>
      </div>`).join('') || '<p class="text-sm dark:text-slate-400 text-slate-500">No events</p>';
  }
}

// ── Render: Services ───────────────────────────────────────────────────────
function renderServices() {
  const s = state.services;
  const container = el('services-list');
  if (!container) return;

  if (!s) {
    container.innerHTML = '<tr><td colspan="4" class="text-center py-4 dark:text-slate-400 text-slate-500">Loading...</td></tr>';
    return;
  }
  if (!s.systemd_available) {
    container.innerHTML = '<tr><td colspan="4" class="text-center py-4 dark:text-slate-400 text-slate-500">systemd not available on this platform</td></tr>';
    return;
  }

  container.innerHTML = (s.services || []).map(svc => {
    const isActive = svc.active === 'active' && svc.sub_state === 'running';
    const isFailed = svc.active === 'failed';
    const statusCls = isActive
      ? 'badge-up px-2 py-0.5 rounded text-xs font-semibold'
      : isFailed
        ? 'badge-down px-2 py-0.5 rounded text-xs font-semibold'
        : 'px-2 py-0.5 rounded text-xs font-semibold bg-slate-100 text-slate-600 dark:bg-slate-700 dark:text-slate-300';
    const dot = isActive ? 'bg-green-500' : isFailed ? 'bg-red-500' : 'bg-slate-400';
    return `<tr class="border-b dark:border-slate-700 border-slate-200 hover:bg-slate-50 dark:hover:bg-slate-800/50">
      <td class="py-3 px-4 font-mono text-sm dark:text-slate-200">${svc.name}</td>
      <td class="py-3 px-4">
        <div class="flex items-center gap-2">
          <span class="w-2 h-2 rounded-full ${dot} flex-shrink-0"></span>
          <span class="${statusCls}">${svc.sub_state || svc.active}</span>
        </div>
      </td>
      <td class="py-3 px-4 text-sm dark:text-slate-400 text-slate-500">${svc.restart_count ?? 0}x</td>
      <td class="py-3 px-4 text-xs dark:text-slate-400 text-slate-500">${svc.since ? fmtDate(svc.since) : '—'}</td>
    </tr>`;
  }).join('') || '<tr><td colspan="4" class="text-center py-4 dark:text-slate-400 text-slate-500">No services configured</td></tr>';
}

// ── Render: Security ───────────────────────────────────────────────────────
function renderSecurity() {
  const s = state.security;
  const container = el('ssh-failed-list');
  const ipList = el('top-failed-ips');

  if (!s) return;

  setText('pending-updates', s.pending_updates < 0 ? 'N/A' : s.pending_updates);
  setText('pending-security-updates', s.pending_security_updates < 0 ? 'N/A' : s.pending_security_updates);
  setText('ssh-log-status', s.log_available ? 'Available' : 'Not found');

  if (container) {
    const events = (s.recent_failed_logins || []).slice(0, 20);
    container.innerHTML = events.map(e => `
      <tr class="border-b dark:border-slate-700 border-slate-200 hover:bg-slate-50 dark:hover:bg-slate-800/50">
        <td class="py-2 px-4 text-xs">${fmtDate(e.occurred_at)}</td>
        <td class="py-2 px-4 font-mono text-xs dark:text-red-400 text-red-600">${e.source_ip}</td>
        <td class="py-2 px-4 text-xs dark:text-slate-300">${e.username || '—'}</td>
        <td class="py-2 px-4 text-xs dark:text-slate-400 text-slate-500">${e.event_type}</td>
      </tr>`).join('') || '<tr><td colspan="4" class="text-center py-4 dark:text-slate-400 text-slate-500">No failed logins recorded</td></tr>';
  }

  if (ipList) {
    ipList.innerHTML = (s.top_failed_ips || []).map(ip => `
      <div class="flex items-center justify-between py-2 border-b dark:border-slate-700 border-slate-200 last:border-0">
        <span class="font-mono text-sm dark:text-red-400 text-red-600">${ip.ip}</span>
        <span class="text-sm font-semibold dark:text-slate-300 text-slate-700">${ip.count} attempts</span>
      </div>`).join('') || '<p class="text-sm dark:text-slate-400 text-slate-500">No suspicious activity</p>';
  }
}

// ── Render: Alerts ─────────────────────────────────────────────────────────
function renderAlerts() {
  const container = el('alerts-list');
  if (!container) return;

  const active = state.alerts.active || [];
  // Update alert badge count
  const badge = el('alert-badge');
  if (badge) {
    badge.textContent = active.length;
    badge.classList.toggle('hidden', active.length === 0);
  }

  const ackAllBtn = el('ack-all-alerts');
  if (ackAllBtn) {
    ackAllBtn.classList.toggle('hidden', active.length === 0);
    ackAllBtn.textContent = active.length > 0 ? `Acknowledge all (${active.length})` : 'Acknowledge all';
  }

  // Update summary card
  const summaryEl = el('summary-alerts');
  if (summaryEl) {
    summaryEl.textContent = active.length > 0 ? `${active.length} active` : 'All clear';
    summaryEl.className = active.length > 0
      ? 'tile-status text-red-500'
      : 'tile-status text-green-500';
  }

  setText('top-alert-count', active.length);

  if (!active.length) {
    container.innerHTML = `
      <div class="flex flex-col items-center justify-center py-12 text-center">
        <div class="text-4xl mb-2">✅</div>
        <p class="dark:text-slate-400 text-slate-500">All systems operating normally</p>
      </div>`;
    return;
  }

  container.innerHTML = active.map(a => `
    <div class="flex items-start gap-3 p-4 rounded-lg border border-red-200 dark:border-red-900/40 bg-red-50 dark:bg-red-900/10 mb-3">
      <span class="text-red-500 text-lg flex-shrink-0">⚠</span>
      <div class="flex-1 min-w-0">
        <p class="font-semibold text-red-800 dark:text-red-300 text-sm">${alertMessage(a)}</p>
        <p class="text-xs dark:text-slate-400 text-slate-500 mt-0.5">Fired: ${fmtDate(alertFiredAt(a))}</p>
      </div>
      <button onclick="acknowledgeAlert(${alertID(a)})"
        class="text-xs px-2 py-1 rounded bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-300 hover:bg-red-200 dark:hover:bg-red-900/50 flex-shrink-0">
        Acknowledge
      </button>
    </div>`).join('');
}

// ── Render: URL Checks ─────────────────────────────────────────────────────
function renderURLs() {
  const container = el('url-list');
  if (!container) {
    renderDashboardURLs();
    return;
  }

  if (!state.urls.length) {
    container.innerHTML = `
      <div class="flex flex-col items-center justify-center py-12 text-center">
        <div class="text-4xl mb-2">🔗</div>
        <p class="dark:text-slate-400 text-slate-500">No URL monitors yet.</p>
        <p class="text-sm dark:text-slate-500 text-slate-400 mt-1">Add a URL below to start monitoring.</p>
      </div>`;
    return;
  }

  container.innerHTML = state.urls.map(u => {
    const id = urlCheckID(u);
    const url = urlCheckURL(u);
    const label = urlCheckLabel(u);
    const enabled = urlCheckEnabled(u);
    const pinned = isURLOnDashboard(id);
    const lr = urlCheckLastResult(u);
    const up = lr ? urlResultUp(lr) : null;
    const statusHtml = up == null
      ? '<span class="px-2 py-0.5 rounded text-xs bg-slate-100 text-slate-600 dark:bg-slate-700 dark:text-slate-300">Pending</span>'
      : up
        ? '<span class="badge-up px-2 py-0.5 rounded text-xs font-semibold">UP</span>'
        : '<span class="badge-down px-2 py-0.5 rounded text-xs font-semibold">DOWN</span>';

    const latencyMs = urlResultLatency(lr);
    const statusCode = urlResultStatusCode(lr);
    const error = urlResultError(lr);
    const uptimePct = urlCheckUptime(u);
    const latency = latencyMs != null ? `${latencyMs}ms` : '—';
    const code = statusCode != null ? statusCode : '—';
    const checked = lr ? fmtDate(urlResultCheckedAt(lr)) : 'Pending';
    const uptime = uptimePct != null ? uptimePct.toFixed(1) + '%' : '—';

    return `
    <div class="p-4 rounded-lg border dark:border-slate-700 border-slate-200 dark:bg-slate-800/50 mb-3 hover:shadow-sm transition-shadow">
      <div class="flex items-start justify-between gap-2">
        <div class="min-w-0 flex-1">
          <div class="flex items-center gap-2 mb-1">
            ${statusHtml}
            <span class="font-semibold dark:text-slate-100 text-slate-900 truncate">${label || url}</span>
          </div>
          <a href="${url}" target="_blank" rel="noopener"
            class="text-xs font-mono text-indigo-500 hover:underline truncate block">${url}</a>
        </div>
        <div class="flex gap-2 flex-shrink-0">
          <button onclick="setDashboardURL(${id}, ${!pinned})"
            class="text-xs px-2 py-1 rounded ${pinned ? 'bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-300' : 'bg-slate-100 dark:bg-slate-700 text-slate-600 dark:text-slate-300'} hover:opacity-80">
            ${pinned ? 'On Home' : 'Add Home'}
          </button>
          <button onclick="toggleURLCheck(${id}, ${!enabled})"
            class="text-xs px-2 py-1 rounded ${enabled ? 'bg-slate-100 dark:bg-slate-700 text-slate-600 dark:text-slate-300' : 'bg-indigo-100 dark:bg-indigo-900/30 text-indigo-700 dark:text-indigo-300'} hover:opacity-80">
            ${enabled ? 'Pause' : 'Resume'}
          </button>
          <button onclick="deleteURLCheck(${id})"
            class="text-xs px-2 py-1 rounded bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-300 hover:opacity-80">
            Delete
          </button>
        </div>
      </div>
      <div class="grid grid-cols-4 gap-2 mt-3 text-xs dark:text-slate-400 text-slate-500">
        <div><span class="block font-medium dark:text-slate-300 text-slate-600">Latency</span>${latency}</div>
        <div><span class="block font-medium dark:text-slate-300 text-slate-600">Status</span>${code}</div>
        <div><span class="block font-medium dark:text-slate-300 text-slate-600">24h Uptime</span>${uptime}</div>
        <div><span class="block font-medium dark:text-slate-300 text-slate-600">Interval</span>${urlCheckInterval(u)}s</div>
      </div>
      <div class="mt-2 text-xs dark:text-slate-500 text-slate-400">Last checked: ${checked}</div>
      ${lr && !up && error ? `<div class="mt-2 p-2 rounded bg-red-50 dark:bg-red-900/20 text-xs text-red-600 dark:text-red-400 font-mono">${error}</div>` : ''}
    </div>`;
  }).join('');
  renderDashboardURLs();
}

function renderDashboardURLs() {
  const panel = el('dashboard-url-panel');
  const container = el('dashboard-url-list');
  if (!panel || !container) return;

  const pinned = state.urls.filter(u => isURLOnDashboard(urlCheckID(u)));
  panel.classList.toggle('hidden', pinned.length === 0);

  if (!pinned.length) {
    container.innerHTML = '';
    return;
  }

  container.innerHTML = pinned.map(u => {
    const id = urlCheckID(u);
    const url = urlCheckURL(u);
    const label = urlCheckLabel(u);
    const lr = urlCheckLastResult(u);
    const up = lr ? urlResultUp(lr) : null;
    const latencyMs = urlResultLatency(lr);
    const statusCode = urlResultStatusCode(lr);
    const uptimePct = urlCheckUptime(u);
    const statusClass = up == null ? 'url-pending' : up ? 'url-up' : 'url-down';
    const statusText = up == null ? 'Pending' : up ? 'UP' : 'DOWN';
    const latency = fmtLatencyTV(latencyMs);
    const code = fmtStatusCodeTV(statusCode);
    const uptime = fmtUptimeTV(uptimePct);
    const title = label || url;

    return `
      <article class="metric-tile home-url-card ${statusClass}" title="${title}">
        <button type="button" class="home-url-remove" onclick="setDashboardURL(${id}, false)" aria-label="Remove ${title} from home">×</button>
        <div class="tile-label">${title}</div>
        <div class="tile-value">${statusText}</div>
        <div class="tile-caption">${latency} | ${code} | ${uptime}</div>
      </article>`;
  }).join('');
}

// ── Render: Connection status ──────────────────────────────────────────────
function renderConnectionStatus() {
  const dot = el('sse-dot');
  const text = el('sse-text');
  if (!dot || !text) return;
  if (state.sseConnected) {
    dot.className = 'status-dot bg-green-500 pulse-dot';
    text.textContent = 'Live';
    text.className = 'text-green-500';
  } else {
    dot.className = 'status-dot bg-red-500';
    text.textContent = 'Reconnecting...';
    text.className = 'text-red-500';
  }
}

// ── SSE ────────────────────────────────────────────────────────────────────
let esReconnectTimer = null;

function connectSSE() {
  if (esReconnectTimer) { clearTimeout(esReconnectTimer); esReconnectTimer = null; }

  const es = new EventSource('/events');

  es.addEventListener('metrics', e => {
    const data = JSON.parse(e.data);
    state.system = data.system;
    state.app = data.app;
    state.network = data.network;
    state.lastUpdate = new Date();
    setText('last-update', fmtClock(state.lastUpdate));
    renderSystem();
    renderApp();
  });

  es.addEventListener('tunnel', e => {
    state.tunnel = JSON.parse(e.data);
    renderTunnel();
  });

  es.addEventListener('services', e => {
    state.services = JSON.parse(e.data);
    renderServices();
  });

  es.addEventListener('alert', e => {
    const a = JSON.parse(e.data);
    // Avoid duplicates
    if (!state.alerts.active.find(x => alertID(x) === alertID(a))) {
      state.alerts.active.unshift(a);
    }
    renderAlerts();
    showToast(alertMessage(a), 'error');
  });

  es.addEventListener('url_result', e => {
  const r = JSON.parse(e.data);
    const checkID = r.check_id ?? r.CheckID;
    const u = state.urls.find(x => urlCheckID(x) === checkID);
    if (u) {
      u.last_result = r;
      u.LastResult = r;
      if (!urlResultUp(r)) showToast(`${urlCheckLabel(u) || urlCheckURL(u)} is DOWN`, 'warning');
    }
    renderURLs();
  });

  es.addEventListener('heartbeat', () => {
    state.sseConnected = true;
    renderConnectionStatus();
  });

  es.onopen = () => {
    state.sseConnected = true;
    renderConnectionStatus();
  };

  es.onerror = () => {
    state.sseConnected = false;
    renderConnectionStatus();
    es.close();
    esReconnectTimer = setTimeout(connectSSE, 5000);
  };
}

// ── API helpers ────────────────────────────────────────────────────────────
async function apiFetch(url, opts = {}) {
  const resp = await fetch(url, {
    headers: { 'Content-Type': 'application/json', ...(opts.headers || {}) },
    ...opts,
  });
  if (!resp.ok) throw new Error(`${resp.status} ${resp.statusText}`);
  return resp.json();
}

// ── URL check CRUD ─────────────────────────────────────────────────────────
async function submitAddURL(e) {
  e.preventDefault();
  const form = e.target;
  const url   = form.elements.url.value.trim();
  const label = form.elements.label.value.trim();
  const interval = parseInt(form.elements.interval.value, 10);
  const timeout  = parseInt(form.elements.timeout.value, 10);

  if (!url) { showToast('URL is required', 'error'); return; }

  try {
    const check = await apiFetch('/api/url-checks', {
      method: 'POST',
      body: JSON.stringify({ url, label, interval_seconds: interval, timeout_seconds: timeout }),
    });
    state.urls.push(check);
    renderURLs();
    form.reset();
    closeAddURLDialog();
    showToast('URL monitor added', 'success');
  } catch (err) {
    showToast('Error: ' + err.message, 'error');
  }
}

function openAddURLDialog() {
  const dialog = el('add-url-dialog');
  if (!dialog) return;
  dialog.classList.remove('hidden');
  const urlInput = dialog.querySelector('input[name="url"]');
  if (urlInput) urlInput.focus();
}

function closeAddURLDialog() {
  const dialog = el('add-url-dialog');
  if (!dialog) return;
  dialog.classList.add('hidden');
}

async function deleteURLCheck(id) {
  if (!confirm('Delete this URL monitor?')) return;
  try {
    await apiFetch(`/api/url-checks/${id}`, { method: 'DELETE' });
    state.urls = state.urls.filter(u => urlCheckID(u) !== id);
    state.dashboardURLIDs.delete(Number(id));
    saveDashboardURLPrefs();
    renderURLs();
  } catch (err) {
    showToast('Error: ' + err.message, 'error');
  }
}

async function toggleURLCheck(id, enabled) {
  try {
    const updated = await apiFetch(`/api/url-checks/${id}`, {
      method: 'PUT',
      body: JSON.stringify({ enabled }),
    });
    const idx = state.urls.findIndex(u => urlCheckID(u) === id);
    if (idx >= 0) state.urls[idx] = { ...state.urls[idx], ...updated };
    renderURLs();
  } catch (err) {
    showToast('Error: ' + err.message, 'error');
  }
}

// ── Alert management ───────────────────────────────────────────────────────
async function acknowledgeAlert(id) {
  try {
    await apiFetch(`/api/alerts/${id}/acknowledge`, { method: 'POST' });
    const a = state.alerts.active.find(x => alertID(x) === id);
    if (a) { a.acknowledged = true; }
    state.alerts.active = state.alerts.active.filter(x => alertID(x) !== id);
    renderAlerts();
  } catch (err) {
    showToast('Error: ' + err.message, 'error');
  }
}

async function acknowledgeAllAlerts() {
  const ids = (state.alerts.active || []).map(alertID).filter(id => id != null);
  if (!ids.length) return;

  try {
    await Promise.all(ids.map(id => apiFetch(`/api/alerts/${id}/acknowledge`, { method: 'POST' })));
    state.alerts.active = state.alerts.active.filter(a => !ids.includes(alertID(a)));
    renderAlerts();
    showToast(`Acknowledged ${ids.length} alerts`, 'success');
  } catch (err) {
    showToast('Error: ' + err.message, 'error');
  }
}

// ── Threshold settings ─────────────────────────────────────────────────────
async function saveThresholds(e) {
  e.preventDefault();
  const form = e.target;
  const thresholds = {};
  for (const input of form.querySelectorAll('input[type=number]')) {
    thresholds[input.name] = parseFloat(input.value);
  }
  try {
    const updated = await apiFetch('/api/thresholds', {
      method: 'PUT',
      body: JSON.stringify(thresholds),
    });
    state.thresholds = updated;
    showToast('Thresholds saved', 'success');
  } catch (err) {
    showToast('Error: ' + err.message, 'error');
  }
}

function renderThresholds() {
  const t = state.thresholds;
  const fields = ['cpu_pct', 'ram_pct', 'disk_pct', 'swap_pct', 'url_latency_ms'];
  fields.forEach(k => {
    const inp = el('threshold-' + k.replaceAll('_', '-'));
    if (inp && t[k] != null) inp.value = t[k];
  });
}

// ── Dark mode ──────────────────────────────────────────────────────────────
function initTheme() {
  const saved = localStorage.getItem('theme');
  const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
  const useDark = saved ? saved === 'dark' : prefersDark;
  document.documentElement.classList.toggle('dark', useDark);
}

// ── Navigation ─────────────────────────────────────────────────────────────
function initNav() {
  const links = document.querySelectorAll('.nav-link');
  const sectionLinks = document.querySelectorAll('[data-section]');
  const sections = document.querySelectorAll('.section');

  function activate(id) {
    links.forEach(l => l.classList.toggle('active', l.dataset.section === id));
    sections.forEach(s => s.classList.toggle('hidden', s.id !== 'section-' + id));
    history.replaceState(null, '', '#' + id);
  }

  sectionLinks.forEach(l => {
    l.addEventListener('click', e => {
      e.preventDefault();
      activate(l.dataset.section);
    });
  });

  window.addEventListener('hashchange', () => {
    activate(location.hash.replace('#', '') || 'dashboard');
  });

  const hash = location.hash.replace('#', '');
  activate(hash || 'dashboard');
}

// ── Toast ──────────────────────────────────────────────────────────────────
function showToast(msg, type = 'info') {
  const container = el('toast-container');
  if (!container) return;

  const colors = {
    success: 'bg-green-50 dark:bg-green-900/80 border border-green-200 dark:border-green-800 text-green-800 dark:text-green-200',
    error:   'bg-red-50 dark:bg-red-900/80 border border-red-200 dark:border-red-800 text-red-800 dark:text-red-200',
    warning: 'bg-yellow-50 dark:bg-yellow-900/80 border border-yellow-200 dark:border-yellow-800 text-yellow-800 dark:text-yellow-200',
    info:    'bg-blue-50 dark:bg-blue-900/80 border border-blue-200 dark:border-blue-800 text-blue-800 dark:text-blue-200',
  };
  const icons = { success: '✓', error: '✕', warning: '⚠', info: 'ℹ' };

  const toast = document.createElement('div');
  toast.className = `toast ${colors[type] || colors.info}`;
  toast.innerHTML = `<span class="flex-shrink-0 font-bold">${icons[type] || 'ℹ'}</span><span>${msg}</span>`;
  container.appendChild(toast);

  setTimeout(() => {
    toast.classList.add('toast-exit');
    setTimeout(() => toast.remove(), 300);
  }, 4000);
}

// ── Boot ───────────────────────────────────────────────────────────────────
document.addEventListener('DOMContentLoaded', async () => {
  initTheme();
  initNav();

  // Register form handlers
  const addForm = el('add-url-form');
  if (addForm) addForm.addEventListener('submit', submitAddURL);
  const openAddURLBtn = el('open-add-url-dialog');
  if (openAddURLBtn) openAddURLBtn.addEventListener('click', openAddURLDialog);
  const closeAddURLBtn = el('close-add-url-dialog');
  if (closeAddURLBtn) closeAddURLBtn.addEventListener('click', closeAddURLDialog);
  const addURLDialog = el('add-url-dialog');
  if (addURLDialog) {
    addURLDialog.addEventListener('click', e => {
      if (e.target === addURLDialog) closeAddURLDialog();
    });
  }
  document.addEventListener('keydown', e => {
    if (e.key === 'Escape') closeAddURLDialog();
  });

  const threshForm = el('threshold-form');
  if (threshForm) threshForm.addEventListener('submit', saveThresholds);

  const ackAllBtn = el('ack-all-alerts');
  if (ackAllBtn) ackAllBtn.addEventListener('click', acknowledgeAllAlerts);

  loadDisplayPrefs();

  // Initial data load via REST
  try {
    const [alertData, urlData, thresholdData, metricData] = await Promise.all([
      apiFetch('/api/alerts').catch(() => ({ active: [], recent: [] })),
      apiFetch('/api/url-checks').catch(() => []),
      apiFetch('/api/thresholds').catch(() => ({})),
      apiFetch('/api/metrics').catch(() => ({})),
    ]);

    state.alerts = alertData || { active: [], recent: [] };
    state.urls = urlData || [];
    state.thresholds = thresholdData || {};
    if (metricData) {
      state.system = metricData.system;
      state.app = metricData.app;
      state.network = metricData.network;
      state.lastUpdate = new Date();
      setText('last-update', fmtClock(state.lastUpdate));
    }

    // Load slower data in background
    Promise.all([
      apiFetch('/api/security').then(d => { state.security = d; renderSecurity(); }).catch(() => {}),
      apiFetch('/api/services').then(d => { state.services = d; renderServices(); }).catch(() => {}),
      apiFetch('/api/tunnel').then(d => { state.tunnel = d; renderTunnel(); }).catch(() => {}),
    ]);

    renderSystem();
    renderApp();
    renderAlerts();
    renderURLs();
    renderDashboardURLs();
    renderThresholds();
    renderTunnel();
    renderConnectionStatus();
  } catch (err) {
    console.error('Initial load failed:', err);
    showToast('Failed to load initial data', 'error');
  }

  connectSSE();
});
