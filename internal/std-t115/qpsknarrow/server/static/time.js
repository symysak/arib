let t0 = Date.now();
let estimated = false;

export function setOrigin(ms, isEstimated) {
  if (typeof ms === 'number' && ms > 0) t0 = ms;
  estimated = !!isEstimated;
}

export function isEstimated() { return estimated; }

export function toDate(tsec) { return new Date(t0 + tsec * 1000); }

const p2 = (n) => String(n).padStart(2, '0');

export function hms(tsec) {
  const d = toDate(tsec);
  return `${p2(d.getHours())}:${p2(d.getMinutes())}:${p2(d.getSeconds())}`;
}

export function hmsMilli(tsec) {
  const d = toDate(tsec);
  return `${hms(tsec)}.${String(d.getMilliseconds()).padStart(3, '0')}`;
}

export function ymd(tsec) {
  const d = toDate(tsec);
  return `${d.getFullYear()}-${p2(d.getMonth() + 1)}-${p2(d.getDate())}`;
}

export function full(tsec) {
  const d = toDate(tsec);
  return `${d.getFullYear()}-${p2(d.getMonth() + 1)}-${p2(d.getDate())} ${hms(tsec)}`;
}

export function dur(sec) {
  if (!isFinite(sec) || sec < 0) return '-';
  const s = Math.round(sec);
  if (s < 60) return `${s}秒`;
  return `${Math.floor(s / 60)}分${p2(s % 60)}秒`;
}
