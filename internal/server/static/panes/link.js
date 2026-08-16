
"use strict";

import { fmtClock, fmtDateMS, wallMS, hasOrigin } from "../time.js";
import { fitCanvas, clear, polyline, thresholdLine, norm, cssVar } from "../draw.js";

const SPARK_POINTS = 60;
const RESET_BUSY_MS = 800;

function el(tag, cls, text) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text !== undefined) e.textContent = text;
  return e;
}

function num(v, digits, unit) {
  if (v === null || v === undefined || !isFinite(v)) return "—";
  return v.toFixed(digits) + (unit || "");
}

const METRICS = [
  {
    key: "cfo",
    cap: "CFO Hz",
    title: "残留搬送波周波数オフセット。後段のスロット単位補正が吸収できるのは ±140Hz 程度。",
    lo: -200, hi: 200,
    zero: true,
    color: "--accent",
    needsSync: true,
    value: (q, st) => (st.toggles.cfo ? q.cfo_hz : null),
    text: (q, st) => (st.toggles.cfo ? num(q.cfo_hz, 1) : "補正 OFF"),
    sev: (v, st) => {
      if (!st.toggles.cfo) return "warnv";
      if (v === null || v === undefined) return "";
      return Math.abs(v) >= 140 ? "bad" : Math.abs(v) >= 100 ? "warnv" : "";
    },
    hist: p => p.cfo,
  },
  {
    key: "evm",
    cap: "EVM中央 %",
    title: "スロット内シンボルの EVM 中央値。実波の良好時は 12〜18% 程度。",
    lo: 0, hi: 60,
    color: "--data",
    needsSync: true,
    value: q => q.evm_median,
    text: q => num(q.evm_median, 1),
    sev: v => (v === null || v === undefined ? "" : v >= 30 ? "bad" : v >= 22 ? "warnv" : ""),
    hist: p => p.evm,
  },
  {
    key: "evmBest",
    cap: "EVM最良 %",
    title: "最良スロットの EVM。中央値との開きが大きいときはスロットごとの当たり外れが大きい。",
    lo: 0, hi: 60,
    color: "--data",
    needsSync: true,
    value: q => q.evm_best,
    text: q => num(q.evm_best, 1),
    sev: v => (v === null || v === undefined ? "" : v >= 25 ? "bad" : v >= 15 ? "warnv" : ""),
    hist: p => p.evmBest,
  },
  {
    key: "crc",
    cap: "CRC16 %",
    title: "制御チャネル CRC16 の一致率。90% を割るなら同調か SNR を疑う。",
    lo: 0, hi: 100,
    color: "--ok",
    needsSync: true,
    value: q => (q.crc_ok_rate === null || q.crc_ok_rate === undefined
      ? null : q.crc_ok_rate * 100),
    text: q => (q.crc_ok_rate === null || q.crc_ok_rate === undefined
      ? "—" : num(q.crc_ok_rate * 100, 1)),
    sev: v => (v === null || v === undefined ? "" : v < 90 ? "bad" : v < 98 ? "warnv" : ""),
    hist: p => (p.crc === null || p.crc === undefined ? null : p.crc * 100),
  },
  {
    key: "msgs",
    cap: "MSG/S",
    title: "制御メッセージの受信レート（直近 10 秒平均）。待機中は 1 スロット/フレームぶん。",
    lo: 0, hi: 15,
    color: "--accent",
    value: q => q.msgs_per_s,
    text: q => num(q.msgs_per_s, 1),
    sev: v => (v === null || v === undefined ? "" : v === 0 ? "bad" : v < 2 ? "warnv" : ""),
    hist: p => p.msgs,
  },
  {
    key: "level",
    cap: "レベル dBFS",
    title: "入力 I/Q のレベル。0 dBFS 付近は飽和、−70 dBFS 以下は無入力に近い。",
    lo: -80, hi: 0,
    color: "--accent",
    value: q => q.level_dbfs,
    text: q => num(q.level_dbfs, 1),
    sev: v => (v === null || v === undefined ? ""
      : (v >= -1 || v <= -70) ? "bad" : v <= -50 ? "warnv" : ""),
    hist: p => p.level,
  },
  {
    key: "overflows",
    cap: "OVERFLOW 件",
    title: "取りこぼし（入力キューあふれ）の累計。1 件でも出たら処理が追いついていない。",
    lo: 0, hi: 10,
    color: "--crit",
    value: q => q.overflows,
    text: q => (q.overflows === null || q.overflows === undefined
      ? "—" : String(q.overflows)),
    sev: v => (v > 0 ? "bad" : ""),
    hist: p => p.overflows,
  },
];

const TOGGLES = [
  {
    key: "squelch",
    name: "スケルチ",
    title: "電力スケルチ。無送信スロットは雑音でも同期相関が立つので、"
      + "最強バーストから −25dB 未満のバーストを捨てる。OFF にすると雑音を拾い始める。",
    call: s => s.toggleSquelch(),
  },
  {
    key: "broadcastStrict",
    name: "誤検知抑制",
    title: "通報検出の厳格判定。単発の通報開始指示を通報とみなさない。"
      + "OFF にすると 1 通でも通報として扱うので、取りこぼしは減るが誤検知が増える。",
    call: s => s.toggleBroadcastStrict(),
  },
  {
    key: "cfo",
    name: "CFO 補正",
    title: "前段の搬送波周波数追尾。OFF にするとスロット単位の残留補正だけで復号する。"
      + "雑音へ誤ロックして戻らないときの回避手段。",
    call: s => s.toggleCFO(),
  },
];

export function init(store) {
  const root = document.getElementById("pane-link");
  if (!root) return;
  root.textContent = "";

  const gBadges = el("div", "group");
  const bConn = el("span", "badge idle", "接続中…");
  const bSync = el("span", "badge warn", "同期 なし");
  const bCast = el("span", "badge idle", "待機中");
  bConn.title = "サーバとの WebSocket 接続状態";
  bSync.title = "スロット同期（同期ワード相関）が取れているか";
  bCast.title = "通報（放送）が進行中かどうか";
  gBadges.append(bConn, bSync, bCast);

  const gStation = el("div", "group");
  const mkField = (capText, titleText) => {
    const box = el("div", "metric");
    const k = el("span", "k", capText);
    const v = el("span", "v", "—");
    box.title = titleText;
    box.append(k, v);
    return { box, v };
  };
  const fMuni = mkField("自治体", "スクランブル値と FACCH 番号通知から同定した親局の自治体");
  const fSeed = mkField("SEED", "スクランブラの初期値（市区町村コード下位 9bit）");
  const fF0 = mkField("チャネル", "ベースバンド上のチャネルオフセット f0（起動時に固定）");
  gStation.append(fMuni.box, fSeed.box, fF0.box);

  const gClock = el("div", "group");
  const clock = el("div", "clock");
  const clockTime = document.createTextNode("—");
  const clockDate = el("span", "date", "—");
  clock.append(clockTime, clockDate);
  gClock.append(clock);

  const gMetrics = el("div", "group");
  const metricRefs = METRICS.map(m => {
    const box = el("div", "metric");
    box.title = m.title;
    const k = el("span", "k", m.cap);
    const v = el("span", "v", "—");
    const canvas = el("canvas");
    box.append(k, v, canvas);
    gMetrics.append(box);
    return { def: m, v, canvas };
  });

  const gToggles = el("div", "group");
  const toggleRefs = TOGGLES.map(t => {
    const b = el("button", "btn", t.name);
    b.type = "button";
    b.title = t.title;
    b.addEventListener("click", () => t.call(store));
    gToggles.append(b);
    return { def: t, btn: b };
  });
  const btnReset = el("button", "btn plain", "CFO 再捕捉");
  btnReset.type = "button";
  btnReset.title = "CFO の粗捕捉をやり直す。無信号区間のリセットで雑音へ誤ロックし、"
    + "自力で戻れなくなったときに押す。";
  let resetBusy = false;
  btnReset.addEventListener("click", () => {
    if (resetBusy) return;
    resetBusy = true;
    store.resetCFO();
    renderToggles();
    setTimeout(() => { resetBusy = false; renderToggles(); }, RESET_BUSY_MS);
  });
  gToggles.append(btnReset);

  root.append(gBadges, gStation, gClock, gMetrics, el("div", "spacer"), gToggles);


  function renderConn(st) {
    const map = {
      connecting: ["接続中…", "badge idle"],
      open: ["接続", "badge ok"],
      reconnecting: ["再接続中…", "badge warn"],
    };
    const [text, cls] = map[st.conn] || map.connecting;
    bConn.textContent = text;
    bConn.className = cls;
  }

  function renderQuality(st) {
    const q = st.quality || {};
    const locked = !!q.sync_locked;

    bSync.textContent = locked ? "同期 OK" : "同期 なし";
    bSync.className = locked ? "badge ok" : "badge warn";

    const hist = st.qualityHistory || [];
    const slice = hist.slice(-SPARK_POINTS);
    const off = SPARK_POINTS - slice.length;

    for (const ref of metricRefs) {
      const m = ref.def;
      const v = m.value(q, st);
      ref.v.textContent = m.text(q, st);
      let cls = m.sev(v, st);
      if (!cls && m.needsSync && !locked) cls = "warnv";
      ref.v.className = cls ? `v ${cls}` : "v";

      const pts = slice.map((p, i) => ({
        x: SPARK_POINTS > 1 ? (i + off) / (SPARK_POINTS - 1) : 1,
        y: norm(m.hist(p), m.lo, m.hi),
      }));
      const { ctx, w, h } = fitCanvas(ref.canvas);
      clear(ctx, w, h);
      if (m.zero) thresholdLine(ctx, w, h, norm(0, m.lo, m.hi), cssVar("--line-soft"));
      polyline(ctx, w, h, pts, { color: cssVar(m.color), width: 1.2 });
    }
  }

  function renderControl(st) {
    const c = st.control || {};
    if (c.municipality) fMuni.v.textContent = c.municipality;
    else if (c.searching) fMuni.v.textContent = "解析中…";
    else if (c.candidates && c.candidates.length) {
      fMuni.v.textContent = `候補 ${c.candidates.length} 件`;
    } else fMuni.v.textContent = "—";
    fMuni.v.className = c.municipality ? "v" : "v warnv";

    fSeed.v.textContent = (c.seed === null || c.seed === undefined) ? "—" : String(c.seed);

    const f0 = (st.tuning || {}).f0_hz;
    fF0.v.textContent = (f0 === null || f0 === undefined)
      ? "0.0 kHz（未指定）"
      : `${(f0 / 1000).toFixed(1)} kHz`;
  }

  function renderWindows(st) {
    const active = !!st.broadcastActive;
    bCast.textContent = active ? "通報中" : "待機中";
    bCast.className = active ? "badge crit live" : "badge idle";
  }

  function renderToggles(st) {
    const s = st || store.state;
    for (const ref of toggleRefs) {
      const on = !!s.toggles[ref.def.key];
      ref.btn.textContent = `${ref.def.name} ${on ? "ON" : "OFF"}`;
      ref.btn.className = on ? "btn" : "btn off";
    }
    btnReset.disabled = resetBusy || !s.toggles.cfo;
    btnReset.textContent = resetBusy ? "再捕捉中…" : "CFO 再捕捉";
  }

  function renderClock(st) {
    const s = st || store.state;
    clockTime.nodeValue = hasOrigin() ? fmtClock(s.t, 0) : "—";
    clockDate.textContent = hasOrigin() ? fmtDateMS(wallMS(s.t)) : "—";
    clock.className = s.t0Estimated ? "clock estimated" : "clock";
    clock.title = s.t0Source || "";
  }

  store.on("conn", renderConn);
  store.on("quality", st => { renderQuality(st); renderClock(st); });
  store.on("control", renderControl);
  store.on("toggles", renderToggles);
  store.on("windows", renderWindows);
  store.on("messages", renderClock);
  store.on("snapshot", st => {
    renderConn(st); renderQuality(st); renderControl(st);
    renderWindows(st); renderToggles(st); renderClock(st);
  });

  setInterval(() => renderClock(), 1000);

  renderConn(store.state);
  renderQuality(store.state);
  renderControl(store.state);
  renderWindows(store.state);
  renderToggles(store.state);
  renderClock(store.state);
}
