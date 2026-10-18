const css = (n, fb) => {
  const v = getComputedStyle(document.documentElement).getPropertyValue(n).trim();
  return v || fb;
};

export const COLOR = {
  bg: () => css('--canvas-bg', '#fff'),
  grid: () => css('--grid', '#e3e7ee'),
  ink: () => css('--ink', '#10161c'),
  ink3: () => css('--ink-3', '#6a7683'),
  accent: () => css('--accent', '#8a4a12'),
  data: () => css('--data', '#1f4fa8'),
  ok: () => css('--ok', '#196b45'),
  crit: () => css('--crit', '#a62b1e'),
  line: () => css('--line', '#ccd2dd'),
};

export function fit(cv) {
  const r = window.devicePixelRatio || 1;
  const w = cv.clientWidth || 1;
  const h = cv.clientHeight || 1;
  if (cv.width !== Math.round(w * r) || cv.height !== Math.round(h * r)) {
    cv.width = Math.round(w * r);
    cv.height = Math.round(h * r);
  }
  const g = cv.getContext('2d');
  g.setTransform(r, 0, 0, r, 0, 0);
  return { g, w, h };
}

export function fade(g, w, h, alpha) {
  g.save();
  g.globalAlpha = alpha;
  g.fillStyle = COLOR.bg();
  g.fillRect(0, 0, w, h);
  g.restore();
}

export function clear(g, w, h) {
  g.fillStyle = COLOR.bg();
  g.fillRect(0, 0, w, h);
}

export function line(g, box, pts, min, max, color, width = 1.5) {
  if (pts.length < 2) return;
  const { x, y, w, h } = box;
  const span = (max - min) || 1;
  g.save();
  g.strokeStyle = color;
  g.lineWidth = width;
  g.beginPath();
  pts.forEach((p, i) => {
    const px = x + (i / (pts.length - 1)) * w;
    const py = y + h - ((p - min) / span) * h;
    if (i === 0) g.moveTo(px, py); else g.lineTo(px, py);
  });
  g.stroke();
  g.restore();
}

export function grid(g, box, rows = 4) {
  const { x, y, w, h } = box;
  g.save();
  g.strokeStyle = COLOR.grid();
  g.lineWidth = 1;
  for (let i = 0; i <= rows; i++) {
    const py = Math.round(y + (i / rows) * h) + 0.5;
    g.beginPath(); g.moveTo(x, py); g.lineTo(x + w, py); g.stroke();
  }
  g.restore();
}

export function label(g, text, x, y, color) {
  g.save();
  g.fillStyle = color || COLOR.ink3();
  g.font = '10px system-ui, sans-serif';
  g.fillText(text, x, y);
  g.restore();
}

export function lineT(g, box, pts, tMin, tMax, vMin, vMax, color, width = 1.5) {
  if (pts.length < 2) return;
  const { x, y, w, h } = box;
  const tSpan = (tMax - tMin) || 1;
  const vSpan = (vMax - vMin) || 1;
  g.save();
  g.strokeStyle = color;
  g.lineWidth = width;
  g.beginPath();
  let started = false;
  for (const p of pts) {
    if (p.t < tMin) continue;
    const px = x + ((p.t - tMin) / tSpan) * w;
    const py = y + h - ((Math.max(vMin, Math.min(vMax, p.v)) - vMin) / vSpan) * h;
    if (!started) { g.moveTo(px, py); started = true; } else g.lineTo(px, py);
  }
  g.stroke();
  g.restore();
}
