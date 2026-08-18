import { fitCanvas, clear, fade, intensityColor, COL, label } from '../draw.js';
import { TOPIC } from '../store.js';
import { CONST_TICK_MS, CONST_CATCHUP, CONST_BACKLOG_MAX } from '../ui.js';

export function init(store) {
  const body = document.getElementById('signal-body');
  body.innerHTML = `
    <canvas id="sig-const"></canvas>
    <dl class="kv tiny" id="sig-kv" style="margin-top:6px"></dl>`;
  const cv = document.getElementById('sig-const');
  const kv = document.getElementById('sig-kv');

  let drawnT = -1;
  let cur = { t_sec: 0, points: [] };

  function advance() {
    const hist = store.state.constellations;
    if (!hist.length) return false;
    let i = hist.findIndex((c) => c.t_sec > drawnT);
    if (i < 0) return false;
    const backlog = hist.length - i;
    if (backlog > CONST_BACKLOG_MAX) {
      i = hist.length - CONST_BACKLOG_MAX;
    } else if (backlog > CONST_CATCHUP) {
      i += 1;
    }
    if (i >= hist.length) i = hist.length - 1;
    cur = hist[i];
    drawnT = cur.t_sec;
    return true;
  }

  const draw = () => {
    const box = Math.max(120, Math.min(cv.clientWidth, 220));
    const { ctx, w, h } = fitCanvas(cv, box);
    fade(ctx, w, h, 0.22);
    const sig = store.state.signal || {};
    const pts = cur.points || [];
    const cx = w / 2, cy = h / 2, r = Math.min(w, h) * 0.34;

    ctx.strokeStyle = COL.grid();
    ctx.lineWidth = 1;
    ctx.beginPath();
    ctx.moveTo(cx, 4); ctx.lineTo(cx, h - 4);
    ctx.moveTo(4, cy); ctx.lineTo(w - 4, cy);
    ctx.stroke();
    ctx.fillStyle = COL.line();
    const ideal = [[1, 1, '00'], [1, -1, '01'], [-1, 1, '10'], [-1, -1, '11']];
    for (const [sx, sy, nm] of ideal) {
      ctx.beginPath(); ctx.arc(cx + sx * r, cy - sy * r, 2.5, 0, 7); ctx.fill();
      label(ctx, nm, cx + sx * r + (sx > 0 ? 6 : -16), cy - sy * r - 5);
    }
    for (let i = 0; i + 1 < pts.length; i += 2) {
      ctx.fillStyle = intensityColor(0.55);
      ctx.fillRect(cx + pts[i] * r - 0.9, cy - pts[i + 1] * r - 0.9, 1.8, 1.8);
    }
    if (pts.length === 0) label(ctx, '信号なし', 6, 12);

    kv.innerHTML = `
      <dt>EVM</dt><dd>${(sig.evm || 0).toFixed(4)}</dd>
      <dt>残留CFO</dt><dd>${(sig.cfo_hz || 0).toFixed(1)} Hz（卓越度 ${(sig.cfo_prom_db || 0).toFixed(0)} dB）</dd>
      <dt>タイミング</dt><dd>τ = ${(sig.timing_tau || 0).toFixed(3)}</dd>
      <dt>相関</dt><dd>${(sig.corr_peak || 0).toFixed(2)} / 6.93</dd>
      <dt>点数</dt><dd>${pts.length / 2}</dd>`;
  };

  setInterval(() => { if (advance()) draw(); }, CONST_TICK_MS);
  store.on(TOPIC.signal, () => draw());
  store.on(TOPIC.meta, () => draw());
  window.addEventListener('resize', draw);
  draw();
}
