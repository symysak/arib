import { TOPIC } from '../store.js';
export function init(store) {
  const draw = (elID, map) => {
    const el = document.getElementById(elID);
    const rows = [...map.entries()].sort((a, b) => b[1] - a[1]);
    if (!rows.length) { el.innerHTML = '<p class="muted tiny">-</p>'; return; }
    const max = rows[0][1];
    el.innerHTML = rows.map(([k, v]) => `
      <div class="h">
        <span class="lbl" title="${k}">${k}</span>
        <span class="n">${v}</span>
      </div>
      <div class="h"><div class="bar"><i style="width:${(100 * v / max).toFixed(1)}%"></i></div><span></span></div>
    `).join('');
  };
  const render = () => {
    draw('hist-type', store.state.histType);
    draw('hist-ch', store.state.histCh);
  };
  store.on(TOPIC.control, render);
  store.on(TOPIC.meta, render);
  render();
}
