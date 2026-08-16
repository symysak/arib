
"use strict";

const origin = { t0MS: null, estimated: false, source: "" };

export function setOrigin(t0WallMS, estimated, source) {
  origin.t0MS = typeof t0WallMS === "number" ? t0WallMS : null;
  origin.estimated = !!estimated;
  origin.source = source || "";
}

export function originInfo() {
  return { ...origin };
}

export function hasOrigin() {
  return origin.t0MS !== null;
}

export function wallMS(t) {
  if (origin.t0MS === null || typeof t !== "number" || !isFinite(t)) return null;
  return origin.t0MS + t * 1000;
}

export function wallDate(t) {
  const ms = wallMS(t);
  return ms === null ? null : new Date(ms);
}

const p2 = n => String(n).padStart(2, "0");
const p3 = n => String(n).padStart(3, "0");

export function fmtClockMS(ms, digits = 3) {
  if (ms === null || ms === undefined) return "—";
  const d = new Date(ms);
  let s = `${p2(d.getHours())}:${p2(d.getMinutes())}:${p2(d.getSeconds())}`;
  if (digits === 3) s += "." + p3(d.getMilliseconds());
  else if (digits === 2) s += "." + p2(Math.floor(d.getMilliseconds() / 10));
  return s;
}

export function fmtClock(t, digits = 3) {
  return fmtClockMS(wallMS(t), digits);
}

export function fmtDateMS(ms) {
  if (ms === null || ms === undefined) return "—";
  const d = new Date(ms);
  return `${d.getFullYear()}-${p2(d.getMonth() + 1)}-${p2(d.getDate())}`;
}

export function sameDayMS(a, b) {
  if (a === null || b === null || a === undefined || b === undefined) return true;
  const x = new Date(a), y = new Date(b);
  return x.getFullYear() === y.getFullYear() && x.getMonth() === y.getMonth()
    && x.getDate() === y.getDate();
}

export function fmtDuration(sec) {
  if (sec === null || sec === undefined || !isFinite(sec)) return "—";
  if (sec < 60) return `${sec.toFixed(1)}秒`;
  const m = Math.floor(sec / 60);
  return `${m}分${(sec - m * 60).toFixed(1)}秒`;
}
