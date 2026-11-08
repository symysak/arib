import { TOPIC } from '../store.js';
import { fit, clear, COLOR } from '../draw.js';
import { UI_TICK_MS } from '../ui.js';
import { hms } from '../time.js';
import { el } from '../dom.js';

const SPAN_SEC = 300;
const LANE_H = 16;
const LEFT = 62;

const TX_BANDS = [
  { label: '音声', color: '#d05a45', match: (k) => k === '音声' },
  { label: 'データ', color: '#2f9e6e', match: (k) => (k || '').startsWith('データ') },
  { label: '送信（種類不明）', color: '#8a5cc2', match: () => true },
];
const BASE = [
  { label: '同期だけ（RICH 読めず・外れる前）', color: '#c9b07a', match: (p) => p.synced },
  { label: '未同期', color: '#e6e8ec', match: () => true },
];
const MARKS = [
  { label: '録音中', color: '#c98a2b', kind: 'stripe' },
  { label: '送信の頭', color: null, kind: 'tick' },
];

const pick = (list, x) => list.find((b) => b.match(x)).color;

function swatch(color, kind) {
  const s = el('i', { class: 'sw' });
  s.style.background = color;
  if (kind === 'stripe') { s.style.height = '3px'; s.style.marginBottom = '4px'; }
  if (kind === 'tick') { s.style.width = '3px'; }
  return s;
}

export function init(store) {
  const body = document.getElementById('timeline-body');
  const sub = document.getElementById('timeline-sub');
  const cv = document.createElement('canvas');
  cv.style.cssText = 'width:100%;display:block';
  const legend = el('div', { class: 'legend' });
  for (const b of [...TX_BANDS, ...BASE]) legend.append(el('span', { class: 'lg' }, [swatch(b.color), el('span', { text: b.label })]));
  const accentSwatches = [];
  for (const m of MARKS) {
    const sw = swatch(m.color || COLOR.accent(), m.kind);
    if (!m.color) accentSwatches.push(sw);
    legend.append(el('span', { class: 'lg' }, [sw, el('span', { text: m.label })]));
  }
  legend.append(el('span', { class: 'lg muted tiny', text: '行をクリックするとその局を選びます' }));
  body.append(cv, legend);

  cv.addEventListener('click', (e) => {
    const r = cv.getBoundingClientRect();
    const st = store.state.stations[Math.floor((e.clientY - r.top) / LANE_H)];
    if (st) store.select(st.idx);
  });

  function draw() {
    const s = store.state;
    const n = s.stations.length || 1;
    const hCss = n * LANE_H + 16;
    if (cv.style.height !== `${hCss}px`) cv.style.height = `${hCss}px`;
    const { g, w, h } = fit(cv);
    clear(g, w, h);
    const accent = COLOR.accent();
    for (const sw of accentSwatches) if (sw.style.background !== accent) sw.style.background = accent;
    let now = s.lastT || 0;
    for (const st of s.stations) now = Math.max(now, st.last_t_sec || 0);
    for (const t of s.t102txs || []) now = Math.max(now, t.end_sec || t.t_sec || 0);
    const t0 = now - SPAN_SEC;
    const px = (t) => LEFT + ((t - t0) / SPAN_SEC) * (w - LEFT - 4);
    sub.textContent = `直近 ${SPAN_SEC / 60} 分`;

    g.strokeStyle = COLOR.grid();
    g.lineWidth = 1;
    g.font = '10px system-ui, sans-serif';
    for (let t = Math.ceil(t0 / 60) * 60; t <= now; t += 60) {
      const x = Math.round(px(t)) + 0.5;
      g.beginPath(); g.moveTo(x, 0); g.lineTo(x, n * LANE_H); g.stroke();
      g.fillStyle = COLOR.ink3();
      g.fillText(hms(t), x + 2, n * LANE_H + 11);
    }

    const txs = new Map();
    for (const t of s.t102txs || []) {
      if (!txs.has(t.idx)) txs.set(t.idx, []);
      txs.get(t.idx).push(t);
    }
    s.stations.forEach((st, i) => {
      const y = i * LANE_H;
      const pts = s.trend.get(st.idx) || [];
      const bar = (ta, tb, col, top = 3, hh = LANE_H - 6) => {
        if (tb < t0) return;
        const x1 = px(Math.max(ta, t0));
        g.fillStyle = col;
        g.fillRect(x1, y + top, Math.max(1, px(tb) - x1 + 0.6), hh);
      };
      for (let k = 0; k < pts.length; k++) {
        const p = pts[k];
        if (p.ledger) continue;
        const next = k + 1 < pts.length ? pts[k + 1].t : (st.watched ? now : p.t);
        bar(p.t, next, pick(BASE, p));
        if (p.recording) bar(p.t, next, MARKS[0].color, 3, 2);
      }
      for (const t of txs.get(st.idx) || []) {
        bar(t.t_sec, t.open ? now : t.end_sec, pick(TX_BANDS, t.kind));
      }
      g.fillStyle = st.idx === s.selected ? COLOR.ink() : COLOR.ink3();
      g.font = `${st.idx === s.selected ? 'bold ' : ''}10px ui-monospace, monospace`;
      g.fillText(st.mhz.toFixed(4), 4, y + LANE_H - 5);
      if (st.idx === s.selected) {
        g.strokeStyle = accent;
        g.strokeRect(LEFT - 1.5, y + 1.5, w - LEFT - 2, LANE_H - 3);
      }
    });

    for (const t of s.t102txs || []) {
      if (t.t_sec < t0) continue;
      const i = s.stations.findIndex((x) => x.idx === t.idx);
      if (i < 0) continue;
      g.fillStyle = accent;
      g.fillRect(px(t.t_sec) - 0.5, i * LANE_H + 3, 1.5, LANE_H - 6);
    }
  }

  store.on(TOPIC.selection, draw);
  setInterval(draw, UI_TICK_MS);
}
