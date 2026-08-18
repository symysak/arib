import { fitCanvas, clear, gridLines, polyline, thresholdLine, norm, COL } from '../draw.js';
import { hms } from '../time.js';
import { TOPIC } from '../store.js';

const LANES = [
  { key: 'cch', name: 'CCH CRC16', lo: 0, hi: 1, color: () => COL.ok(), thr: 0.99, pct: true },
  { key: 'tch', name: 'TCH CRC16', lo: 0, hi: 1, color: () => COL.data(), thr: 0.99, pct: true },
  { key: 'corr', name: '同期相関', lo: 0, hi: 8, color: () => COL.accent(), thr: 0.75 * Math.sqrt(48) / 8 },
  { key: 'evm', name: 'EVM', lo: 0, hi: 0.6, color: () => COL.warn(), thr: 0.25 / 0.6, invert: true },
];

export function init(store) {
  const body = document.getElementById('quality-body');
  const sub = document.getElementById('quality-sub');
  body.innerHTML = `<div class="trend">${
    LANES.map((l) => `<div class="lane"><span class="name">${l.name}</span>
      <canvas data-lane="${l.key}"></canvas></div>`).join('')
  }<div class="axis"><span id="q-t0">-</span><span id="q-t1">-</span></div></div>`;

  const series = (key) => {
    const s = store.state;
    if (key === 'corr') return s.corr.map((p) => ({ t: p.t, v: p.v }));
    if (key === 'evm') return s.evm.map((p) => ({ t: p.t, v: p.v }));
    return s.trend.filter((p) => p[key] !== null).map((p) => ({ t: p.t, v: p[key] }));
  };

  const render = () => {
    const s = store.state;
    let t0 = Infinity, t1 = -Infinity;
    for (const l of LANES) {
      for (const p of series(l.key)) { if (p.t < t0) t0 = p.t; if (p.t > t1) t1 = p.t; }
    }
    if (!isFinite(t0)) { t0 = 0; t1 = 1; }
    if (t1 - t0 < 1) t1 = t0 + 1;
    document.getElementById('q-t0').textContent = hms(t0);
    document.getElementById('q-t1').textContent = hms(t1);
    const q = s.quality || {};
    sub.textContent = q.frames ? `${q.frames} 枚` : '';

    for (const l of LANES) {
      const cv = body.querySelector(`canvas[data-lane="${l.key}"]`);
      const { ctx, w, h } = fitCanvas(cv, 34);
      clear(ctx, w, h);
      gridLines(ctx, w, h, { rows: 2 });
      if (l.thr !== undefined) thresholdLine(ctx, w, h, l.thr);
      const pts = series(l.key).map((p) => ({
        x: (p.t - t0) / (t1 - t0),
        y: norm(p.v, l.lo, l.hi),
      }));
      polyline(ctx, w, h, pts, { color: l.color(), width: 1.3, fill: true });
      const lastV = series(l.key).slice(-1)[0];
      if (lastV) {
        const txt = l.pct ? `${(lastV.v * 100).toFixed(1)}%` : lastV.v.toFixed(2);
        ctx.fillStyle = COL.ink3();
        ctx.font = '10px ui-monospace, Menlo, monospace';
        ctx.textAlign = 'right';
        ctx.fillText(txt, w - 3, 10);
      }
    }
  };

  for (const t of [TOPIC.quality, TOPIC.frame, TOPIC.signal, TOPIC.meta, TOPIC.windows]) {
    store.on(t, () => render());
  }
  window.addEventListener('resize', render);
  render();
}
