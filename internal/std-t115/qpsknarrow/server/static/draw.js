
export function cssVar(name, fallback = '#000') {
  const v = getComputedStyle(document.documentElement).getPropertyValue(name);
  return (v || '').trim() || fallback;
}

export const COL = {
  bg: () => cssVar('--canvas-bg', '#fff'),
  grid: () => cssVar('--grid', '#e3e7ee'),
  ink: () => cssVar('--ink', '#10161c'),
  ink3: () => cssVar('--ink-3', '#6a7683'),
  line: () => cssVar('--line', '#ccd2dd'),
  accent: () => cssVar('--accent', '#1d4e89'),
  data: () => cssVar('--data', '#1f4fa8'),
  ok: () => cssVar('--ok', '#196b45'),
  warn: () => cssVar('--warn', '#8a5a05'),
  crit: () => cssVar('--crit', '#a62b1e'),
};

export function fitCanvas(canvas, cssHeight) {
  const dpr = window.devicePixelRatio || 1;
  const w = Math.max(1, canvas.clientWidth || 300);
  if (cssHeight) canvas.style.height = `${cssHeight}px`;
  const h = Math.max(1, cssHeight || canvas.clientHeight || 60);
  const pw = Math.round(w * dpr);
  const ph = Math.round(h * dpr);
  if (canvas.width !== pw || canvas.height !== ph) {
    canvas.width = pw;
    canvas.height = ph;
  }
  const ctx = canvas.getContext('2d');
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  return { ctx, w, h };
}

export function clear(ctx, w, h, color) {
  ctx.fillStyle = color || COL.bg();
  ctx.fillRect(0, 0, w, h);
}

export function fade(ctx, w, h, alpha = 0.14, color) {
  ctx.save();
  ctx.globalAlpha = alpha;
  ctx.fillStyle = color || COL.bg();
  ctx.fillRect(0, 0, w, h);
  ctx.restore();
}

export function gridLines(ctx, w, h, { rows = 3, cols = 0 } = {}) {
  ctx.save();
  ctx.strokeStyle = COL.grid();
  ctx.lineWidth = 1;
  ctx.beginPath();
  for (let i = 0; i <= rows; i++) {
    const y = Math.round((i / rows) * (h - 1)) + 0.5;
    ctx.moveTo(0, y); ctx.lineTo(w, y);
  }
  for (let i = 1; i < cols; i++) {
    const x = Math.round((i / cols) * w) + 0.5;
    ctx.moveTo(x, 0); ctx.lineTo(x, h);
  }
  ctx.stroke();
  ctx.restore();
}

export function polyline(ctx, w, h, pts, { color, width = 1.4, fill = false } = {}) {
  if (!pts || pts.length === 0) return;
  ctx.save();
  ctx.strokeStyle = color || COL.accent();
  ctx.lineWidth = width;
  ctx.lineJoin = 'round';
  ctx.beginPath();
  pts.forEach((p, i) => {
    const x = p.x * w;
    const y = h - p.y * h;
    if (i === 0) ctx.moveTo(x, y); else ctx.lineTo(x, y);
  });
  ctx.stroke();
  if (fill) {
    ctx.lineTo(pts[pts.length - 1].x * w, h);
    ctx.lineTo(pts[0].x * w, h);
    ctx.closePath();
    ctx.globalAlpha = 0.14;
    ctx.fillStyle = color || COL.accent();
    ctx.fill();
  }
  ctx.restore();
}

export function thresholdLine(ctx, w, h, y01, color) {
  ctx.save();
  ctx.strokeStyle = color || COL.line();
  ctx.setLineDash([3, 3]);
  ctx.lineWidth = 1;
  const y = Math.round(h - y01 * h) + 0.5;
  ctx.beginPath(); ctx.moveTo(0, y); ctx.lineTo(w, y); ctx.stroke();
  ctx.restore();
}

export function marker(ctx, w, h, x01, color) {
  ctx.save();
  ctx.strokeStyle = color || COL.crit();
  ctx.lineWidth = 1;
  const x = Math.round(x01 * w) + 0.5;
  ctx.beginPath(); ctx.moveTo(x, 0); ctx.lineTo(x, h); ctx.stroke();
  ctx.restore();
}

export function label(ctx, text, x, y, { color, size = 10, align = 'left' } = {}) {
  ctx.save();
  ctx.fillStyle = color || COL.ink3();
  ctx.font = `${size}px ui-monospace, Menlo, Consolas, monospace`;
  ctx.textAlign = align;
  ctx.fillText(text, x, y);
  ctx.restore();
}

export function intensityColor(v) {
  const t = Math.max(0, Math.min(1, v));
  return `rgba(29, 78, 137, ${(0.10 + 0.85 * t).toFixed(3)})`;
}

export function norm(v, lo, hi) {
  if (hi === lo) return 0;
  return Math.max(0, Math.min(1, (v - lo) / (hi - lo)));
}

export function sparkline(canvas, values, { lo, hi, color } = {}) {
  const { ctx, w, h } = fitCanvas(canvas, 15);
  clear(ctx, w, h);
  if (!values || values.length < 2) return;
  let mn = lo, mx = hi;
  if (mn === undefined || mx === undefined) {
    mn = Math.min(...values); mx = Math.max(...values);
    if (mx - mn < 1e-9) { mx = mn + 1; }
  }
  const pts = values.map((v, i) => ({
    x: i / (values.length - 1),
    y: norm(v, mn, mx),
  }));
  polyline(ctx, w, h, pts, { color: color || COL.accent(), width: 1, fill: true });
}

export function bands(ctx, w, h, segs) {
  ctx.save();
  for (const s of segs) {
    ctx.globalAlpha = s.alpha === undefined ? 1 : s.alpha;
    ctx.fillStyle = s.color;
    const x0 = s.a * w;
    const x1 = Math.max(x0 + 1, s.b * w);
    ctx.fillRect(x0, 0, x1 - x0, h);
  }
  ctx.restore();
}
