import { TOPIC } from '../store.js';
import { fit, clear, lineT, grid, label, COLOR } from '../draw.js';
import { UI_TICK_MS } from '../ui.js';
import { hms } from '../time.js';

const SPAN_SEC = 300;

const LANES = [
  { key: 'swPct', name: '同期ワード率 [%]', min: 0, max: 100, color: () => COLOR.ok() },
  { key: 'phase', name: '差動位相誤差 [°]', min: 0, max: 45, color: () => COLOR.crit() },
  { key: 'level', name: '信号レベル [dB]', min: -70, max: 0, color: () => COLOR.accent() },
  { key: 'cfo', name: '搬送波ずれ [Hz]', min: -6000, max: 6000, color: () => COLOR.data() },
  { key: 'cacPct', name: 'CAC の CRC 一致率 [%]（制御キャリア）', min: 0, max: 100, color: () => COLOR.ink() },
];

export function init(store) {
  const body = document.getElementById('quality-body');
  const sub = document.getElementById('quality-sub');
  const cv = document.createElement('canvas');
  cv.style.cssText = 'width:100%;height:270px;display:block';
  body.appendChild(cv);

  function draw() {
    const { g, w, h } = fit(cv);
    clear(g, w, h);
    const st = store.selectedStation();
    const pts = st ? (store.state.trend.get(st.idx) || []) : [];
    const now = store.state.lastT || 0;
    const t0 = now - SPAN_SEC;
    sub.textContent = st ? `${st.mhz.toFixed(4)} MHz · 直近 ${SPAN_SEC / 60} 分` : '-';
    const laneH = (h - 12) / LANES.length;
    LANES.forEach((ln, i) => {
      const box = { x: 56, y: i * laneH + 12, w: w - 66, h: laneH - 22 };
      grid(g, box, 2);
      label(g, ln.name, 4, box.y + 8);
      label(g, String(ln.max), 4, box.y + 18, COLOR.grid());
      label(g, String(ln.min), 4, box.y + box.h, COLOR.grid());
      const vals = pts.map((p) => ({ t: p.t, v: p[ln.key] ?? 0 }));
      lineT(g, box, vals, t0, now, ln.min, ln.max, ln.color());
      const last = vals[vals.length - 1];
      if (last) label(g, last.v.toFixed(1), box.x + box.w - 36, box.y + 8, COLOR.ink());
    });
    g.font = '10px system-ui, sans-serif';
    for (let t = Math.ceil(t0 / 60) * 60; t <= now; t += 60) {
      const x = 56 + ((t - t0) / SPAN_SEC) * (w - 66);
      g.fillStyle = COLOR.ink3();
      g.fillText(hms(t), x - 16, h - 1);
    }
  }

  store.on(TOPIC.selection, draw);
  setInterval(draw, UI_TICK_MS);
}
