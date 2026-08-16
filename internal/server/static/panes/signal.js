
"use strict";

import { fitCanvas, clear, fade, cssVar } from "../draw.js";

const R10 = Math.sqrt(10);
const LEVELS = [-3 / R10, -1 / R10, 1 / R10, 3 / R10];
const BOUNDS = [-2 / R10, 0, 2 / R10];
const SPAN = 1.6;

const PERSIST = [
  { key: "short", label: "残光 短", alpha: 0.55 },
  { key: "mid", label: "残光 中", alpha: 0.22 },
  { key: "long", label: "残光 長", alpha: 0.07 },
];

export function init(store) {
  const body = document.getElementById("signal-body");
  if (!body) return;

  body.innerHTML = `
    <canvas class="constel"></canvas>
    <div class="audiobar" data-s="chips"></div>
    <div class="note" data-s="evm"></div>`;

  const canvas = body.querySelector("canvas.constel");
  const chipBox = body.querySelector('[data-s="chips"]');
  const evmNote = body.querySelector('[data-s="evm"]');

  let persist = 1;
  let lastW = 0, lastH = 0;
  let lastPoints = [];

  chipBox.innerHTML = PERSIST.map((p, i) =>
    `<span class="chip${i === persist ? " on" : ""}" data-i="${i}">${p.label}</span>`).join("");
  chipBox.addEventListener("click", e => {
    const chip = e.target.closest(".chip");
    if (!chip) return;
    persist = Number(chip.dataset.i);
    for (const c of chipBox.querySelectorAll(".chip")) {
      c.classList.toggle("on", Number(c.dataset.i) === persist);
    }
  });

  function drawGrid(ctx, w, h) {
    const cx = w / 2, cy = h / 2, sc = Math.min(w, h) / (2 * SPAN);
    ctx.save();
    ctx.strokeStyle = cssVar("--grid", "#e2e8e8");
    ctx.lineWidth = 1;
    ctx.beginPath();
    for (const b of BOUNDS) {
      const x = Math.round(cx + b * sc) + 0.5;
      const y = Math.round(cy - b * sc) + 0.5;
      ctx.moveTo(x, 0); ctx.lineTo(x, h);
      ctx.moveTo(0, y); ctx.lineTo(w, y);
    }
    ctx.stroke();
    ctx.strokeStyle = cssVar("--accent", "#0a6b70");
    ctx.globalAlpha = 0.45;
    for (const re of LEVELS) {
      for (const im of LEVELS) {
        ctx.beginPath();
        ctx.arc(cx + re * sc, cy - im * sc, 3.5, 0, Math.PI * 2);
        ctx.stroke();
      }
    }
    ctx.restore();
  }

  function draw(points) {
    const { ctx, w, h } = fitCanvas(canvas);
    if (w !== lastW || h !== lastH) {
      lastW = w; lastH = h;
      clear(ctx, w, h);
    } else {
      fade(ctx, w, h, PERSIST[persist].alpha);
    }
    drawGrid(ctx, w, h);

    const cx = w / 2, cy = h / 2, sc = Math.min(w, h) / (2 * SPAN);
    ctx.save();
    ctx.fillStyle = cssVar("--data", "#1f4fa8");
    for (const p of points) {
      const x = cx + p[0] * sc, y = cy - p[1] * sc;
      if (x < -2 || y < -2 || x > w + 2 || y > h + 2) continue;
      ctx.fillRect(x - 1, y - 1, 2, 2);
    }
    ctx.restore();
  }

  function renderEVM() {
    const q = store.state.quality || {};
    const evm = q.evm_median;
    const txt = (evm === null || evm === undefined || !isFinite(evm))
      ? "—" : `${Number(evm).toFixed(1)} %`;
    evmNote.textContent = `EVM 中央 ${txt}｜EVM 最良スロットの補正済みシンボル（200ms 更新）`;
  }

  store.on("constellation", () => {
    lastPoints = store.state.constellation || [];
    draw(lastPoints);
  });
  store.on("quality", renderEVM);

  if (window.ResizeObserver) {
    new ResizeObserver(() => draw(lastPoints)).observe(canvas);
  }

  draw([]);
  renderEVM();
}
