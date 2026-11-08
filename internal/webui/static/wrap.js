
export function tokyoToWGS84(lat, lon) {
  return [lat - 0.00010695 * lat + 0.000017464 * lon + 0.0046017,
    lon - 0.000046038 * lat - 0.000083043 * lon + 0.010040];
}

export function wgs84ToTokyo(lat, lon) {
  let t = [lat, lon];
  for (let i = 0; i < 3; i++) {
    const w = tokyoToWGS84(t[0], t[1]);
    t = [t[0] - (w[0] - lat), t[1] - (w[1] - lon)];
  }
  return t;
}

export function fold(x, span) {
  return ((x + span / 2) % span + span) % span - span / 2;
}

export function circularMean(vs, span) {
  let s = 0, c = 0;
  for (const v of vs) {
    const a = 2 * Math.PI * v / span;
    s += Math.sin(a);
    c += Math.cos(a);
  }
  const m = Math.atan2(s, c) / (2 * Math.PI) * span;
  return (m % span + span) % span;
}

function nearest(cv, origin, unit, span, ref) {
  const k = Math.round(((ref - origin) / unit - cv) / span);
  return origin + (k * span + cv) * unit;
}

const groupKey = (w) => [w.group || '', w.span, w.unit, w.origin_lat, w.origin_lon, w.datum].join('|');

export function resolveAll(wraps, ref) {
  const out = new Array(wraps.length).fill(null);
  if (!ref) return out;
  const groups = new Map();
  wraps.forEach((w, i) => {
    if (!w || !(w.span > 0) || !(w.unit > 0)) return;
    const k = w.group ? groupKey(w) : `#${i}`;
    if (!groups.has(k)) groups.set(k, []);
    groups.get(k).push(i);
  });
  for (const idx of groups.values()) {
    const w0 = wraps[idx[0]], span = w0.span, unit = w0.unit;
    const tokyo = w0.datum === 'tokyo';
    const [rla, rlo] = tokyo ? wgs84ToTokyo(ref[0], ref[1]) : ref;
    const cla = circularMean(idx.map((i) => wraps[i].lat), span);
    const clo = circularMean(idx.map((i) => wraps[i].lon), span);
    const gla = nearest(cla, w0.origin_lat, unit, span, rla);
    const glo = nearest(clo, w0.origin_lon, unit, span, rlo);
    for (const i of idx) {
      const w = wraps[i];
      const la = gla + fold(w.lat - cla, span) * unit;
      const lo = glo + fold(w.lon - clo, span) * unit;
      out[i] = tokyo ? tokyoToWGS84(la, lo) : [la, lo];
    }
  }
  return out;
}

export function halfWindow(w) {
  return w.span * w.unit / 2;
}
