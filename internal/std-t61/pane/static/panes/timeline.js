import { TOPIC } from '../store.js';
import { fit, clear, COLOR } from '../draw.js';
import { UI_TICK_MS } from '../ui.js';
import { hms } from '../time.js';
import { el } from '../dom.js';

const SPAN_SEC = 300;
const LANE_H = 16;
const LEFT = 62;

const BANDS = [
  { label: '音声', color: '#d05a45', match: (p) => p.synced && p.richOK && p.mode === '音声' },
  { label: 'FACCH', color: '#3f6fb5', match: (p) => p.synced && p.richOK && p.mode === 'FACCH' },
  { label: '非音声', color: '#8a5cc2', match: (p) => p.synced && p.richOK && (p.mode || '').startsWith('非音声') },
  { label: '空線', color: '#cfd6e0', match: (p) => p.synced && p.richOK && p.mode === '空線' },
  { label: 'アイドル・その他', color: '#9aa6b4', match: (p) => p.synced && p.richOK },
  { label: '同期・RICH 不一致', color: '#c9b07a', match: (p) => p.synced },
  { label: '未同期', color: '#e6e8ec', match: () => true },
];
const MARKS = [
  { label: '録音中', color: '#c98a2b', kind: 'stripe' },
  { label: '同期バースト', color: null, kind: 'tick' },
];

function bandColor(p) {
  if (p.ledger) return null;
  for (const b of BANDS) if (b.match(p)) return b.color;
  return BANDS[BANDS.length - 1].color;
}

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
  cv.id = 'tl-cv';
  cv.style.cssText = 'width:100%;display:block';
  const legend = el('div', { class: 'legend' });
  for (const b of BANDS) legend.append(el('span', { class: 'lg' }, [swatch(b.color), el('span', { text: b.label })]));
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
    const i = Math.floor((e.clientY - r.top) / LANE_H);
    const st = store.state.stations[i];
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
    const now = s.lastT || 0;
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

    s.stations.forEach((st, i) => {
      const y = i * LANE_H;
      const pts = s.trend.get(st.idx) || [];
      for (let k = 0; k < pts.length; k++) {
        const p = pts[k];
        if (p.t < t0) continue;
        const next = k + 1 < pts.length ? pts[k + 1].t : (st.watched ? now : p.t);
        const x1 = px(p.t);
        const x2 = px(next);
        const col = bandColor(p);
        if (!col) continue;
        g.fillStyle = col;
        g.fillRect(x1, y + 3, Math.max(1, x2 - x1 + 0.6), LANE_H - 6);
        if (p.recording) {
          g.fillStyle = MARKS[0].color;
          g.fillRect(x1, y + 3, Math.max(1, x2 - x1 + 0.6), 2);
        }
      }
      g.fillStyle = st.idx === s.selected ? COLOR.ink() : COLOR.ink3();
      g.font = `${st.idx === s.selected ? 'bold ' : ''}10px ui-monospace, monospace`;
      g.fillText(st.mhz.toFixed(4), 4, y + LANE_H - 5);
      if (st.idx === s.selected) {
        g.strokeStyle = accent;
        g.strokeRect(LEFT - 1.5, y + 1.5, w - LEFT - 2, LANE_H - 3);
      }
    });

    for (const c of s.calls) {
      if (c.kind !== 'sync_burst' || !c.id || c.t_sec < t0) continue;
      const i = s.stations.findIndex((x) => x.idx === c.idx);
      if (i < 0) continue;
      const x = px(c.t_sec);
      g.fillStyle = accent;
      g.fillRect(x - 0.5, i * LANE_H + 3, 1.5, LANE_H - 6);
    }
  }

  store.on(TOPIC.selection, draw);
  setInterval(draw, UI_TICK_MS);
}
