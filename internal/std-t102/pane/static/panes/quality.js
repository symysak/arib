import { TOPIC } from '../store.js';
import { fit, clear, lineT, grid, label, COLOR } from '../draw.js';
import { UI_TICK_MS } from '../ui.js';
import { hms } from '../time.js';

const SPAN_SEC = 300;

const LANES = [
  { key: 'richPct', name: 'RICH が規定どおり [%]', min: 0, max: 100, color: () => COLOR.ok() },
  { key: 'crcPct', name: '機能チャネルの CRC 一致 [%]', min: 0, max: 100, color: () => COLOR.ink() },
  { key: 'level', name: '信号レベル [dB]', min: -90, max: 0, color: () => COLOR.accent() },
  { key: 'dev', name: '±1 の偏位 [Hz]（規格 315）', min: 0, max: 600, color: () => COLOR.data() },
  { key: 'dc', name: '直流（搬送波のずれ）[Hz]', min: -2000, max: 2000, color: () => COLOR.crit() },
];

function rates(pts) {
  const out = [];
  for (let i = 1; i < pts.length; i++) {
    const a = pts[i - 1], b = pts[i];
    const df = b.frames - a.frames;
    if (df <= 0) continue;
    const dc = b.crcAll - a.crcAll;
    out.push({ t: b.t, richPct: (100 * (b.richOK - a.richOK)) / df, crcPct: dc > 0 ? (100 * (b.crcOK - a.crcOK)) / dc : null,
      level: b.level, dev: b.dev, dc: b.dc });
  }
  return out;
}

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
    const rs = rates(pts);
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
      const vals = rs.filter((p) => p[ln.key] !== null && p[ln.key] !== undefined).map((p) => ({ t: p.t, v: p[ln.key] }));
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
