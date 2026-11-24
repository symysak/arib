import { fitCanvas, clear, gridLines, polyline, thresholdLine, norm, COL } from '../t115draw.js';
import { hms } from '../time.js';
import { TOPIC } from '../store.js';

const GAP_SEC = 0.5;

function lostSpans(frames, tEnd) {
  const out = [];
  for (let i = 1; i < frames.length; i++) {
    if (frames[i].t - frames[i - 1].t > GAP_SEC) out.push({ t0: frames[i - 1].t, t1: frames[i].t });
  }
  const last = frames[frames.length - 1];
  if (last && tEnd - last.t > GAP_SEC) out.push({ t0: last.t, t1: tEnd });
  return out;
}

function segments(pts, spans) {
  const out = [];
  let cur = [];
  for (const p of pts) {
    const prev = cur[cur.length - 1];
    if (prev && spans.some((s) => Math.max(s.t0, prev.t) < Math.min(s.t1, p.t))) {
      out.push(cur);
      cur = [];
    }
    cur.push(p);
  }
  if (cur.length) out.push(cur);
  return out;
}

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
    const s = store.t115();
    if (key === 'corr') return s.corr.map((p) => ({ t: p.t, v: p.v }));
    if (key === 'evm') return s.evm.map((p) => ({ t: p.t, v: p.v }));
    return s.trend.filter((p) => p[key] !== null).map((p) => ({ t: p.t, v: p[key] }));
  };

  const render = () => {
    const s = store.t115();
    let t0 = Infinity, t1 = -Infinity;
    for (const l of LANES) {
      for (const p of series(l.key)) { if (p.t < t0) t0 = p.t; if (p.t > t1) t1 = p.t; }
    }
    if (!isFinite(t0)) { t0 = 0; t1 = 1; }
    const tLast = t1;
    if (t1 - t0 < 1) t1 = t0 + 1;
    document.getElementById('q-t0').textContent = hms(t0);
    document.getElementById('q-t1').textContent = hms(t1);
    const q = s.quality || {};
    sub.textContent = (q.frames ? `${q.frames} 枚 · ` : '') + '赤い網 = 同期が切れていた間';
    const lost = lostSpans(s.corr, tLast);

    for (const l of LANES) {
      const cv = body.querySelector(`canvas[data-lane="${l.key}"]`);
      const { ctx, w, h } = fitCanvas(cv, 34);
      clear(ctx, w, h);
      gridLines(ctx, w, h, { rows: 2 });
      if (l.thr !== undefined) thresholdLine(ctx, w, h, l.thr);
      ctx.save();
      ctx.globalAlpha = 0.14;
      ctx.fillStyle = COL.crit();
      for (const sp of lost) {
        const a = Math.max(sp.t0, t0), b = Math.min(sp.t1, t1);
        if (b > a) ctx.fillRect(((a - t0) / (t1 - t0)) * w, 0, Math.max(1, ((b - a) / (t1 - t0)) * w), h);
      }
      ctx.restore();
      for (const seg of segments(series(l.key), lost)) {
        const pts = seg.map((p) => ({
          x: (p.t - t0) / (t1 - t0),
          y: norm(p.v, l.lo, l.hi),
        }));
        polyline(ctx, w, h, pts, { color: l.color(), width: 1.3, fill: true });
      }
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
