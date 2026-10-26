import { TOPIC } from '../store.js';
import { fit, clear, COLOR } from '../draw.js';
import { UI_TICK_MS } from '../ui.js';
import { hms } from '../time.js';
import { el } from '../dom.js';

const SPAN_SEC = 300;
const SUB_H = 5;
const LANE_H = 4 * SUB_H + 6;
const LEFT = 62;
const GAP_SEC = 5;

const BANDS = [
  { label: '音声', color: '#d05a45', kind: '音声' },
  { label: 'FACCH', color: '#3f6fb5', kind: 'FACCH' },
  { label: '同期バースト', color: '#8a5cc2', kind: '同期バースト' },
  { label: '制御（CAC）', color: '#2f9e8f', kind: '制御' },
  { label: '制御（上りの CAC）', color: '#5fc0b2', kind: '制御（上り）' },
  { label: '送信なし（上り）', color: '#f4f5f7', kind: '送信なし' },
  { label: '空線', color: '#cfd6e0', kind: '空線' },
  { label: '慣性（同期ワード無し）', color: '#9aa6b4', kind: '慣性' },
  { label: '未同期', color: '#eef0f3', kind: '未同期' },
];
const CALL_MARK = '#c98a2b';

function color(kind) {
  const k = (kind || '未同期').replace(/^通話:/, '');
  const b = BANDS.find((x) => x.kind === k);
  return b ? b.color : BANDS[BANDS.length - 1].color;
}

function swatch(c, stripe) {
  const s = el('i', { class: 'sw' });
  s.style.background = c;
  if (stripe) { s.style.height = '3px'; s.style.marginBottom = '4px'; }
  return s;
}

export function init(store) {
  const body = document.getElementById('timeline-body');
  const sub = document.getElementById('timeline-sub');
  const cv = document.createElement('canvas');
  cv.style.cssText = 'width:100%;display:block';
  const legend = el('div', { class: 'legend' });
  for (const b of BANDS) legend.append(el('span', { class: 'lg' }, [swatch(b.color), el('span', { text: b.label })]));
  legend.append(el('span', { class: 'lg' }, [swatch(CALL_MARK, true), el('span', { text: '通話中（そのスロットを録音）' })]));
  legend.append(el('span', { class: 'lg muted tiny', text: '1 局 = 上から ST#0〜#3 の 4 本。行をクリックするとその局を選びます。監視を終えた局は周波数に「終」を付け、その後は描かない' }));
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
      const y = i * LANE_H + 3;
      const pts = s.trend.get(st.idx) || [];
      for (let k = 0; k < pts.length; k++) {
        const p = pts[k];
        if (p.t < t0 || !p.slots) continue;
        const next = k + 1 < pts.length ? pts[k + 1].t : (st.watched ? now : p.t);
        const x1 = px(p.t);
        const x2 = px(Math.min(next, p.t + GAP_SEC));
        const wd = Math.max(1, x2 - x1 + 0.6);
        for (let sl = 0; sl < 4; sl++) {
          const kind = p.slots[sl] || '未同期';
          g.fillStyle = color(kind);
          g.fillRect(x1, y + sl * SUB_H, wd, SUB_H - 1);
          if (kind.startsWith('通話:')) {
            g.fillStyle = CALL_MARK;
            g.fillRect(x1, y + sl * SUB_H, wd, 1.5);
          }
        }
      }
      g.fillStyle = st.idx === s.selected ? COLOR.ink() : COLOR.ink3();
      g.font = `${st.idx === s.selected ? 'bold ' : ''}10px ui-monospace, monospace`;
      g.fillText(st.mhz.toFixed(4) + (st.watched ? '' : ' 終'), 4, y + LANE_H / 2 + 1);
      if (st.idx === s.selected) {
        g.strokeStyle = COLOR.accent();
        g.strokeRect(LEFT - 1.5, y - 1.5, w - LEFT - 2, LANE_H - 3);
      }
    });
  }

  store.on(TOPIC.selection, draw);
  setInterval(draw, UI_TICK_MS);
}
