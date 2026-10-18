
"use strict";

export function cssVar(name, fallback = "#000") {
  const v = getComputedStyle(document.documentElement).getPropertyValue(name);
  return v ? v.trim() : fallback;
}

export function fitCanvas(canvas) {
  const dpr = window.devicePixelRatio || 1;
  const rect = canvas.getBoundingClientRect();
  const w = Math.max(1, Math.round(rect.width));
  const h = Math.max(1, Math.round(rect.height || canvas.clientHeight || 1));
  if (canvas.width !== w * dpr || canvas.height !== h * dpr) {
    canvas.width = w * dpr;
    canvas.height = h * dpr;
  }
  const ctx = canvas.getContext("2d");
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  return { ctx, w, h };
}

export function clear(ctx, w, h, color) {
  ctx.fillStyle = color || cssVar("--canvas-bg", "#ffffff");
  ctx.fillRect(0, 0, w, h);
}

export function fade(ctx, w, h, alpha, color) {
  ctx.save();
  ctx.globalAlpha = alpha;
  ctx.fillStyle = color || cssVar("--canvas-bg", "#ffffff");
  ctx.fillRect(0, 0, w, h);
  ctx.restore();
}

export function gridLines(ctx, w, h, { rows = 4, cols = 0 } = {}) {
  ctx.save();
  ctx.strokeStyle = cssVar("--grid", "#e2e8e8");
  ctx.lineWidth = 1;
  ctx.beginPath();
  for (let i = 1; i < rows; i++) {
    const y = Math.round(h * i / rows) + 0.5;
    ctx.moveTo(0, y); ctx.lineTo(w, y);
  }
  for (let i = 1; i < cols; i++) {
    const x = Math.round(w * i / cols) + 0.5;
    ctx.moveTo(x, 0); ctx.lineTo(x, h);
  }
  ctx.stroke();
  ctx.restore();
}

export function polyline(ctx, w, h, pts, { color, width = 1.5, fill = false } = {}) {
  if (!pts.length) return;
  const stroke = color || cssVar("--accent", "#0a6b70");
  ctx.save();
  ctx.lineWidth = width;
  ctx.strokeStyle = stroke;
  ctx.lineJoin = "round";
  ctx.beginPath();
  let started = false;
  for (const p of pts) {
    if (p.y === null || p.y === undefined || !isFinite(p.y)) { started = false; continue; }
    const x = p.x * w, y = (1 - p.y) * h;
    if (!started) { ctx.moveTo(x, y); started = true; } else { ctx.lineTo(x, y); }
  }
  ctx.stroke();
  if (fill) {
    ctx.globalAlpha = 0.12;
    ctx.lineTo(w, h); ctx.lineTo(0, h); ctx.closePath();
    ctx.fillStyle = stroke;
    ctx.fill();
  }
  ctx.restore();
}

export function thresholdLine(ctx, w, h, y01, color) {
  ctx.save();
  ctx.strokeStyle = color || cssVar("--warn", "#8a5a05");
  ctx.setLineDash([3, 3]);
  ctx.lineWidth = 1;
  const y = Math.round((1 - y01) * h) + 0.5;
  ctx.beginPath(); ctx.moveTo(0, y); ctx.lineTo(w, y); ctx.stroke();
  ctx.restore();
}

export function marker(ctx, w, h, x01, color) {
  ctx.save();
  ctx.strokeStyle = color || cssVar("--crit", "#a62b1e");
  ctx.globalAlpha = 0.65;
  ctx.lineWidth = 1;
  const x = Math.round(x01 * w) + 0.5;
  ctx.beginPath(); ctx.moveTo(x, 0); ctx.lineTo(x, h); ctx.stroke();
  ctx.restore();
}

export function label(ctx, text, x, y, { color, size = 10, align = "left" } = {}) {
  ctx.save();
  ctx.fillStyle = color || cssVar("--ink-3", "#6b7a7a");
  ctx.font = `${size}px ui-monospace, SFMono-Regular, Menlo, monospace`;
  ctx.textAlign = align;
  ctx.textBaseline = "top";
  ctx.fillText(text, x, y);
  ctx.restore();
}

export function intensityColor(v, hue = "teal") {
  const x = Math.max(0, Math.min(1, v));
  const ramps = {
    teal: [[255, 255, 255], [214, 234, 234], [90, 160, 162], [10, 75, 79]],
    warm: [[255, 255, 255], [246, 230, 200], [200, 140, 40], [110, 60, 5]],
    crit: [[255, 255, 255], [246, 220, 216], [200, 90, 75], [110, 25, 18]],
  };
  const r = ramps[hue] || ramps.teal;
  const seg = (r.length - 1) * x;
  const i = Math.min(r.length - 2, Math.floor(seg));
  const f = seg - i;
  const c = k => Math.round(r[i][k] + (r[i + 1][k] - r[i][k]) * f);
  return `rgb(${c(0)},${c(1)},${c(2)})`;
}

export function norm(v, lo, hi) {
  if (v === null || v === undefined || !isFinite(v)) return null;
  if (hi === lo) return 0.5;
  return Math.max(0, Math.min(1, (v - lo) / (hi - lo)));
}
