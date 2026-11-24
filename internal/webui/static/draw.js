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
  let open = false;
  for (const p of pts) {
    if (p.t < tMin) continue;
    if (p.v === null || p.v === undefined || !Number.isFinite(p.v)) { open = false; continue; }
    const px = x + ((p.t - tMin) / tSpan) * w;
    const py = y + h - ((Math.max(vMin, Math.min(vMax, p.v)) - vMin) / vSpan) * h;
    if (!open || p.brk) g.moveTo(px, py); else g.lineTo(px, py);
    open = true;
  }
  g.stroke();
  g.restore();
}

const STARVE_SEC = 0.6;

function inDt(a, b) {
  return (a.inT != null && b.inT != null) ? b.inT - a.inT : b.t - a.t;
}

export function framesShort(a, b, rate) {
  if (!rate || a.frames == null || b.frames == null || b.frames < a.frames) return false;
  const dt = inDt(a, b);
  return dt > 0 && b.frames - a.frames < 0.5 * rate * dt;
}

function mergeSpans(spans) {
  spans.sort((a, b) => a.t0 - b.t0);
  const out = [];
  for (const s of spans) {
    const last = out[out.length - 1];
    if (last && s.t0 <= last.t1) last.t1 = Math.max(last.t1, s.t1);
    else out.push({ t0: s.t0, t1: s.t1 });
  }
  return out;
}

export function lostSpans(pts, tMax, rate = 0) {
  const out = [];
  let start = null;
  for (const p of pts) {
    if (start === null) {
      if (!p.synced) start = p.t;
    } else if (p.synced) {
      out.push({ t0: start, t1: p.t });
      start = null;
    }
  }
  if (start !== null) out.push({ t0: start, t1: tMax });
  if (rate) {
    let run = null;
    for (let j = 1; j < pts.length; j++) {
      const a = pts[j - 1], b = pts[j];
      if (framesShort(a, b, rate)) {
        if (!run) run = { t0: a.t, t1: b.t, dt: 0 };
        run.t1 = b.t;
        run.dt += inDt(a, b);
        continue;
      }
      if (run && run.dt > STARVE_SEC) out.push({ t0: run.t0, t1: run.t1 });
      run = null;
    }
    if (run && run.dt > STARVE_SEC) out.push({ t0: run.t0, t1: tMax });
  }
  return mergeSpans(out);
}

export function overlapsSpans(spans, t0, t1) {
  return spans.some((s) => Math.max(s.t0, t0) < Math.min(s.t1, t1));
}

export function cutSpans(vals, spans) {
  if (!spans.length) return vals;
  return vals.map((p, j) => (j > 0 && overlapsSpans(spans, vals[j - 1].t, p.t) ? { ...p, brk: true } : p));
}

export function shadeT(g, box, spans, tMin, tMax, color, alpha = 0.14) {
  const { x, y, w, h } = box;
  const tSpan = (tMax - tMin) || 1;
  g.save();
  g.globalAlpha = alpha;
  g.fillStyle = color;
  for (const s of spans) {
    const a = Math.max(s.t0, tMin), b = Math.min(s.t1, tMax);
    if (b <= a) continue;
    const x0 = x + ((a - tMin) / tSpan) * w;
    const x1 = x + ((b - tMin) / tSpan) * w;
    g.fillRect(x0, y, Math.max(1, x1 - x0), h);
  }
  g.restore();
}
