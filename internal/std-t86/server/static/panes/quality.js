
"use strict";

import { fmtClock, hasOrigin } from "../time.js";
import {
  fitCanvas, clear, gridLines, polyline, thresholdLine, marker, norm, cssVar,
} from "../draw.js";

const RANGES = [
  { id: "60s", label: "60秒", span: 60 },
  { id: "5m", label: "5分", span: 300 },
  { id: "all", label: "全体", span: null },
];

const GAP_SEC = 2.5;

const LANES = [
  {
    cap: "CFO [Hz]",
    lo: -200, hi: 200,
    color: "--accent",
    zero: true,
    get: p => p.cfo,
    fmt: v => `${v.toFixed(1)} Hz`,
  },
  {
    cap: "EVM中央 [%]",
    lo: 0, hi: 60,
    color: "--data",
    get: p => p.evm,
    fmt: v => `${v.toFixed(1)} %`,
  },
  {
    cap: "入力レベル [dBFS]",
    lo: -80, hi: 0,
    color: "--accent",
    get: p => p.level,
    fmt: v => `${v.toFixed(1)} dBFS`,
  },
  {
    cap: "CRC16一致率 [%]",
    lo: 0, hi: 100,
    color: "--ok",
    threshold: 90,
    get: p => (p.crc === null || p.crc === undefined ? null : p.crc * 100),
    fmt: v => `${v.toFixed(1)} %`,
  },
  {
    cap: "msg/s",
    lo: 0, hi: 15,
    color: "--accent",
    get: p => p.msgs,
    fmt: v => `${v.toFixed(1)} /s`,
  },
];

const MARKER_KINDS = [
  { kind: "broadcast", name: "通報", color: "--crit", soft: "--crit-soft" },
  { kind: "loss", name: "信号喪失", color: "--warn", soft: "--warn-soft" },
  { kind: "seed", name: "seed 確定", color: "--accent", soft: "--accent-soft" },
  { kind: "cfo", name: "CFO", color: "--data", soft: "--data-soft" },
];

function el(tag, cls, text) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text !== undefined) e.textContent = text;
  return e;
}

function reducePoints(pts, w) {
  const n = Math.max(1, Math.floor(w));
  if (pts.length <= n * 2) return pts;
  const out = [];
  const sum = new Array(n).fill(0);
  const cnt = new Array(n).fill(0);
  const seen = new Array(n).fill(false);
  for (const p of pts) {
    const i = Math.min(n - 1, Math.max(0, Math.floor(p.x * n)));
    seen[i] = true;
    if (p.y !== null && p.y !== undefined && isFinite(p.y)) { sum[i] += p.y; cnt[i]++; }
  }
  for (let i = 0; i < n; i++) {
    out.push({ x: (i + 0.5) / n, y: seen[i] && cnt[i] ? sum[i] / cnt[i] : null });
  }
  return out;
}

export function init(store) {
  const body = document.getElementById("quality-body");
  const sub = document.getElementById("quality-sub");
  if (!body) return;
  body.textContent = "";

  let rangeID = "60s";

  const bar = el("div", "toolbar");
  const chips = RANGES.map(r => {
    const c = el("span", "chip", r.label);
    c.title = r.span === null
      ? "履歴の先頭から現在まで"
      : `直近 ${r.label}`;
    c.addEventListener("click", () => { rangeID = r.id; syncChips(); render(); });
    bar.append(c);
    return { def: r, node: c };
  });
  function syncChips() {
    for (const c of chips) c.node.className = c.def.id === rangeID ? "chip on" : "chip";
  }
  syncChips();

  const legend = el("div", "note");
  legend.append(document.createTextNode("マーカー: "));
  for (const k of MARKER_KINDS) {
    const pill = el("span", "pill", `▏${k.name}`);
    pill.style.background = `var(${k.soft})`;
    pill.style.color = `var(${k.color})`;
    pill.style.marginRight = "5px";
    legend.append(pill);
  }

  const trend = el("div", "trend");
  const laneRefs = LANES.map(def => {
    const lane = el("div", "lane");
    const canvas = el("canvas");
    const cap = el("span", "cap", def.cap);
    const cur = el("span", "cur", "—");
    lane.append(canvas, cap, cur);
    trend.append(lane);
    return { def, canvas, cur };
  });

  const axis = el("div", "axis");
  const axL = el("span", "", "—");
  const axC = el("span", "", "—");
  const axR = el("span", "", "—");
  axis.append(axL, axC, axR);
  trend.append(axis);

  body.append(bar, legend, trend);


  function window_(st) {
    const hist = st.qualityHistory || [];
    if (!hist.length) return null;
    const t1 = hist[hist.length - 1].t;
    const span = RANGES.find(r => r.id === rangeID).span;
    let t0 = span === null ? hist[0].t : t1 - span;
    if (!(t1 > t0)) t0 = t1 - 1;
    return { t0, t1 };
  }

  function render() {
    const st = store.state;
    const win = window_(st);
    const hist = st.qualityHistory || [];

    if (!win) {
      for (const ref of laneRefs) {
        const { ctx, w, h } = fitCanvas(ref.canvas);
        clear(ctx, w, h);
        gridLines(ctx, w, h, { rows: 4 });
        ref.cur.textContent = "—";
      }
      axL.textContent = axC.textContent = axR.textContent = "—";
      if (sub) sub.textContent = "履歴なし";
      return;
    }

    const { t0, t1 } = win;
    const dur = t1 - t0;
    const inWin = hist.filter(p => p.t >= t0 && p.t <= t1);
    const marks = (st.markers || []).filter(m => m.t >= t0 && m.t <= t1);

    for (const ref of laneRefs) {
      const def = ref.def;
      const { ctx, w, h } = fitCanvas(ref.canvas);
      clear(ctx, w, h);
      gridLines(ctx, w, h, { rows: 4 });
      if (def.zero) thresholdLine(ctx, w, h, norm(0, def.lo, def.hi), cssVar("--line"));
      if (def.threshold !== undefined) {
        thresholdLine(ctx, w, h, norm(def.threshold, def.lo, def.hi), cssVar("--warn"));
      }

      const pts = [];
      let prevT = null;
      for (const p of inWin) {
        if (prevT !== null && p.t - prevT > GAP_SEC) {
          pts.push({ x: (prevT - t0) / dur, y: null });
        }
        pts.push({ x: (p.t - t0) / dur, y: norm(def.get(p), def.lo, def.hi) });
        prevT = p.t;
      }

      for (const m of marks) {
        const k = MARKER_KINDS.find(x => x.kind === m.kind);
        marker(ctx, w, h, (m.t - t0) / dur, cssVar(k ? k.color : "--ink-3"));
      }
      polyline(ctx, w, h, reducePoints(pts, w), { color: cssVar(def.color), width: 1.4 });

      const last = inWin.length ? def.get(inWin[inWin.length - 1]) : null;
      ref.cur.textContent = (last === null || last === undefined || !isFinite(last))
        ? "—" : def.fmt(last);
    }

    axL.textContent = fmtClock(t0, 0);
    axC.textContent = fmtClock((t0 + t1) / 2, 0);
    axR.textContent = fmtClock(t1, 0);

    if (sub) {
      const r = RANGES.find(x => x.id === rangeID);
      const span = r.span === null ? `${Math.round(dur)}秒` : r.label;
      sub.textContent = `${span} · ${inWin.length}点 · マーカー ${marks.length}`
        + (hasOrigin() ? "" : "（時刻基準 未着）");
    }
  }

  let raf = 0;
  const schedule = () => {
    if (raf) return;
    raf = requestAnimationFrame(() => { raf = 0; render(); });
  };
  if (typeof ResizeObserver === "function") {
    new ResizeObserver(schedule).observe(trend);
  } else {
    window.addEventListener("resize", schedule);
  }

  store.on("quality", render);
  store.on("markers", render);
  store.on("snapshot", render);

  render();
}
