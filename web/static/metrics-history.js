'use strict';

const KIND_LABEL = { cpu: 'CPU', ram: 'RAM', disk: 'Disk' };
const KIND_COLOR = { cpu: '#6366f1', ram: '#10b981', disk: '#f59e0b' };

const PRESET_SEC = {
  '24h': 24 * 3600,
  '48h': 48 * 3600,
  '7d': 7 * 24 * 3600,
  '30d': 30 * 24 * 3600,
};

function el(id) {
  return document.getElementById(id);
}

function nowUnix() {
  return Math.floor(Date.now() / 1000);
}

function toLocalDTValue(d) {
  const pad = (n) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

function parseLocalDTValue(s) {
  const d = new Date(s);
  return Number.isNaN(d.getTime()) ? null : Math.floor(d.getTime() / 1000);
}

function fmtPct(v) {
  if (v == null || Number.isNaN(v)) return '—';
  return v.toFixed(2) + '%';
}

/** Matches dashboard-style byte formatting (GB / MB). */
function fmtBytesShort(b) {
  if (b == null || !Number.isFinite(Number(b))) return '—';
  const x = Number(b);
  if (x >= 1e9) return (x / 1e9).toFixed(1) + ' GB';
  if (x >= 1e6) return (x / 1e6).toFixed(1) + ' MB';
  if (x >= 1e3) return (x / 1e3).toFixed(1) + ' KB';
  return String(Math.round(x)) + ' B';
}

function fmtUsedTotalPair(p) {
  if (p.used != null && p.total != null) {
    return `${fmtBytesShort(p.used)} / ${fmtBytesShort(p.total)}`;
  }
  return '—';
}

function fmtTime(ts) {
  return new Date(ts * 1000).toLocaleString();
}

function readStateFromURL() {
  const u = new URL(window.location.href);
  const kind = u.searchParams.get('kind') || 'cpu';
  const from = u.searchParams.get('from');
  const to = u.searchParams.get('to');
  const step = u.searchParams.get('step');
  return { kind, from, to, step };
}

function writeURL(kind, from, to, step) {
  const u = new URL(window.location.href);
  u.searchParams.set('kind', kind);
  u.searchParams.set('from', String(from));
  u.searchParams.set('to', String(to));
  u.searchParams.set('step', String(step));
  history.replaceState(null, '', u.pathname + u.search);
}

function detectPreset(from, to) {
  const span = to - from;
  const now = nowUnix();
  if (Math.abs(to - now) > 120) return 'custom';
  for (const [k, sec] of Object.entries(PRESET_SEC)) {
    if (Math.abs(span - sec) <= 120) return k;
  }
  return 'custom';
}

function applyPreset(preset, toUnix) {
  const sec = PRESET_SEC[preset];
  if (!sec) return null;
  return { from: toUnix - sec, to: toUnix };
}

function renderLineChart(points, color) {
  const w = 800;
  const h = 220;
  const pad = { l: 48, r: 12, t: 16, b: 32 };
  const iw = w - pad.l - pad.r;
  const ih = h - pad.t - pad.b;
  if (!points.length) {
    return '<p class="text-slate-500 text-center py-12">No data in this range.</p>';
  }
  const ts = points.map((p) => p.t);
  const vs = points.map((p) => p.v);
  const t0 = ts[0];
  const t1 = ts[ts.length - 1];
  const dt = Math.max(1, t1 - t0);
  let minV = Math.min(0, ...vs);
  let maxV = Math.max(100, ...vs);
  if (maxV - minV < 1e-6) maxV = minV + 1;
  const scaleX = (t) => pad.l + ((t - t0) / dt) * iw;
  const scaleY = (v) => pad.t + ih - ((v - minV) / (maxV - minV)) * ih;

  const linePath = `M ${points.map((p) => `${scaleX(p.t).toFixed(1)},${scaleY(p.v).toFixed(1)}`).join(' L ')}`;
  const xFirst = scaleX(ts[0]).toFixed(1);
  const xLast = scaleX(ts[ts.length - 1]).toFixed(1);
  const yBase = (pad.t + ih).toFixed(1);
  const areaD = `M ${xFirst} ${yBase} L ${points.map((p) => `${scaleX(p.t).toFixed(1)},${scaleY(p.v).toFixed(1)}`).join(' L ')} L ${xLast} ${yBase} Z`;

  const gid = 'hist-grad-' + Math.random().toString(36).slice(2, 9);
  return `<svg class="w-full h-auto max-h-[280px]" viewBox="0 0 ${w} ${h}" preserveAspectRatio="xMidYMid meet" role="img" aria-label="Metric chart">
    <defs>
      <linearGradient id="${gid}" x1="0" y1="0" x2="0" y2="1">
        <stop offset="0%" stop-color="${color}" stop-opacity="0.35"/>
        <stop offset="100%" stop-color="${color}" stop-opacity="0.02"/>
      </linearGradient>
    </defs>
    <path d="${areaD}" fill="url(#${gid})" stroke="none"/>
    <path d="${linePath}" fill="none" stroke="${color}" stroke-width="2" stroke-linejoin="round" stroke-linecap="round"/>
    <text x="${pad.l}" y="${h - 6}" class="fill-slate-500" style="font-size:11px">${fmtTime(t0)}</text>
    <text x="${w - pad.r}" y="${h - 6}" text-anchor="end" class="fill-slate-500" style="font-size:11px">${fmtTime(t1)}</text>
    <text x="4" y="${pad.t + 10}" class="fill-slate-500" style="font-size:11px">${maxV.toFixed(0)}%</text>
    <text x="4" y="${pad.t + ih}" class="fill-slate-500" style="font-size:11px">${minV.toFixed(0)}%</text>
  </svg>`;
}

async function apiFetch(path) {
  const resp = await fetch(path, {
    credentials: 'same-origin',
    headers: { Accept: 'application/json' },
  });
  if (!resp.ok) {
    const errTxt = await resp.text();
    let msg = `${resp.status} ${resp.statusText}`;
    try {
      const j = JSON.parse(errTxt);
      if (j.error) msg = j.error;
    } catch (_) {}
    throw new Error(msg);
  }
  return resp.json();
}

const state = {
  kind: 'cpu',
  from: 0,
  to: 0,
  step: 300,
};

async function load() {
  const status = el('chart-status');
  const host = el('chart-host');
  const tbody = el('points-body');
  status.textContent = 'Loading…';
  status.classList.remove('hidden');
  host.classList.add('hidden');
  tbody.innerHTML = '';

  const q = `/api/metrics/history?kind=${encodeURIComponent(state.kind)}&from=${state.from}&to=${state.to}&step=${state.step}`;
  try {
    const data = await apiFetch(q);
    const pts = data.points || [];
    status.classList.add('hidden');
    host.classList.remove('hidden');
    const col = KIND_COLOR[state.kind] || '#6366f1';
    host.innerHTML = renderLineChart(pts, col);

    const showCap = state.kind === 'ram' || state.kind === 'disk';
    const capHead = el('points-cap-head');
    if (capHead) capHead.classList.toggle('hidden', !showCap);

    const rev = [...pts].reverse();
    tbody.innerHTML = rev
      .map((p) => {
        const capCell = showCap
          ? `<td class="px-3 py-2 text-right text-slate-300">${fmtUsedTotalPair(p)}</td>`
          : '';
        return `<tr class="border-b border-slate-700/50 hover:bg-slate-800/40"><td class="px-3 py-2 text-slate-300">${fmtTime(p.t)}</td><td class="px-3 py-2 text-right text-slate-100">${fmtPct(p.v)}</td>${capCell}</tr>`;
      })
      .join('');
    if (!rev.length) {
      const colspan = showCap ? 3 : 2;
      tbody.innerHTML = `<tr><td colspan="${colspan}" class="px-3 py-4 text-slate-500 text-center">No rows</td></tr>`;
    }
  } catch (e) {
    status.textContent = String(e.message || e);
    status.classList.remove('hidden');
    host.classList.add('hidden');
  }
}

function syncTitle() {
  const t = el('history-title');
  const label = KIND_LABEL[state.kind] || state.kind;
  t.textContent = `${label} history`;
  document.title = `${label} history — monitoring24`;
}

function syncControlsFromState() {
  el('resolution').value = String(state.step);
  const preset = detectPreset(state.from, state.to);
  el('preset-range').value = preset;
  const btn = el('btn-custom-range');
  if (preset === 'custom') {
    btn.classList.remove('hidden');
  } else {
    btn.classList.add('hidden');
  }
}

function openCustomDialog() {
  const dlg = el('custom-range-dialog');
  const fromD = new Date(state.from * 1000);
  const toD = new Date(state.to * 1000);
  el('custom-from').value = toLocalDTValue(fromD);
  el('custom-to').value = toLocalDTValue(toD);
  dlg.showModal();
}

function init() {
  const url = readStateFromURL();
  state.kind = url.kind in KIND_LABEL ? url.kind : 'cpu';
  const now = nowUnix();
  if (url.from && url.to && url.step) {
    state.from = parseInt(url.from, 10);
    state.to = parseInt(url.to, 10);
    state.step = parseInt(url.step, 10);
  } else {
    state.to = now;
    state.from = now - PRESET_SEC['24h'];
    state.step = 300;
    writeURL(state.kind, state.from, state.to, state.step);
  }

  syncTitle();
  syncControlsFromState();
  load();

  el('preset-range').addEventListener('change', () => {
    const preset = el('preset-range').value;
    if (preset === 'custom') {
      openCustomDialog();
      return;
    }
    const nowU = nowUnix();
    const b = applyPreset(preset, nowU);
    if (!b) return;
    state.from = b.from;
    state.to = b.to;
    writeURL(state.kind, state.from, state.to, state.step);
    syncControlsFromState();
    load();
  });

  el('resolution').addEventListener('change', () => {
    state.step = parseInt(el('resolution').value, 10);
    writeURL(state.kind, state.from, state.to, state.step);
    load();
  });

  el('btn-custom-range').addEventListener('click', () => openCustomDialog());

  el('custom-range-form').addEventListener('submit', (ev) => {
    ev.preventDefault();
    const fromU = parseLocalDTValue(el('custom-from').value);
    const toU = parseLocalDTValue(el('custom-to').value);
    if (fromU == null || toU == null || fromU > toU) {
      return;
    }
    state.from = fromU;
    state.to = toU;
    el('preset-range').value = 'custom';
    el('btn-custom-range').classList.remove('hidden');
    el('custom-range-dialog').close();
    writeURL(state.kind, state.from, state.to, state.step);
    load();
  });

  el('custom-range-cancel').addEventListener('click', () => {
    el('custom-range-dialog').close();
    syncControlsFromState();
  });
}

init();
