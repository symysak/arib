
"use strict";

const esc = s => String(s).replace(/[&<>"]/g,
  c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));

function renderHist(el, counts) {
  if (!el) return;
  const entries = Object.entries(counts || {})
    .filter(([, v]) => typeof v === "number")
    .sort((a, b) => b[1] - a[1]);
  const total = entries.reduce((s, [, v]) => s + v, 0) || 1;
  el.innerHTML = entries.map(([k, v]) =>
    `<div class="bar"><span class="lbl" title="${esc(k)}">${esc(k)}</span>`
    + `<span class="track"><span class="fill" style="width:${(v / total * 100).toFixed(0)}%">`
    + `</span></span><span class="n">${v}</span></div>`).join("")
    || `<div class="note">—</div>`;
}

export function init(store) {
  const typeEl = document.getElementById("hist-type");
  const tchEl = document.getElementById("hist-tch");

  const drawType = () => renderHist(typeEl, store.state.control?.type_counts);
  const drawTCH = () => renderHist(tchEl, store.state.tch);

  store.on("control", drawType);
  store.on("tch", drawTCH);
  store.on("snapshot", () => { drawType(); drawTCH(); });

  drawType();
  drawTCH();
}
