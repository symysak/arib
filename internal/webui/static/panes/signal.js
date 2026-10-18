import { TOPIC } from '../store.js';
import { fit, fade, clear, COLOR } from '../draw.js';
import { CONST_TICK_MS, CONST_CATCHUP, CONST_BACKLOG_MAX } from '../ui.js';

export function init(store) {
  const body = document.getElementById('signal-body');
  body.innerHTML = `<canvas id="const-cv" style="width:100%;height:210px;display:block"></canvas>
    <div class="row tiny muted"><span id="const-info">-</span></div>`;
  const cv = document.getElementById('const-cv');
  const info = document.getElementById('const-info');

  function axes(g, w, h) {
    const cx = w / 2, cy = h / 2, r = Math.min(w, h) / 2 - 6;
    g.save();
    g.strokeStyle = COLOR.grid();
    g.lineWidth = 1;
    g.beginPath(); g.moveTo(cx - r, cy); g.lineTo(cx + r, cy);
    g.moveTo(cx, cy - r); g.lineTo(cx, cy + r); g.stroke();
    g.beginPath(); g.arc(cx, cy, r, 0, Math.PI * 2); g.stroke();
    g.strokeStyle = COLOR.line();
    g.setLineDash([2, 3]);
    for (const a of [Math.PI / 4, (3 * Math.PI) / 4, -Math.PI / 4, (-3 * Math.PI) / 4]) {
      g.beginPath(); g.moveTo(cx, cy); g.lineTo(cx + r * Math.cos(a), cy - r * Math.sin(a)); g.stroke();
    }
    g.setLineDash([]);
    g.restore();
    return { cx, cy, r };
  }

  const queue = [];
  let lastT = -1;
  let inited = false;

  function intake() {
    const list = store.state.constellations;
    const fresh = [];
    for (let i = list.length - 1; i >= 0 && list[i].t_sec > lastT; i--) fresh.push(list[i]);
    for (let i = fresh.length - 1; i >= 0; i--) queue.push(fresh[i]);
    if (fresh.length) lastT = fresh[0].t_sec;
    if (queue.length > CONST_BACKLOG_MAX) queue.splice(0, queue.length - CONST_CATCHUP);
  }

  function tick() {
    intake();
    const { g, w, h } = fit(cv);
    if (!inited) { clear(g, w, h); inited = true; }
    const backlog = queue.length;
    const take = backlog > CONST_CATCHUP ? 2 : 1;
    fade(g, w, h, 0.16);
    const { cx, cy, r } = axes(g, w, h);
    let drawn = null;
    const sel = store.selectedStation();
    const synced = !!(sel && sel.synced);
    for (let n = 0; n < take && queue.length; n++) {
      const c = queue.shift();
      drawn = c;
      g.fillStyle = synced ? COLOR.data() : COLOR.ink3();
      const p = c.points;
      for (let i = 0; i + 1 < p.length; i += 2) {
        const x = cx + p[i] * r * 0.62;
        const y = cy - p[i + 1] * r * 0.62;
        g.fillRect(x - 1.1, y - 1.1, 2.2, 2.2);
      }
    }
    if (drawn) {
      info.textContent = `${sel ? sel.mhz.toFixed(4) + ' MHz' : ''}　${synced ? '同期中' : '未同期（フレームに乗っていない点）'}`
        + `　差動シンボル ${drawn.points.length / 2} 点/40ms　待ち ${backlog} 枚`;
    }
  }

  store.on(TOPIC.selection, () => { queue.length = 0; lastT = -1; inited = false; });
  setInterval(tick, CONST_TICK_MS);
}
