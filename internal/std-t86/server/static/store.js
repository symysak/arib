
"use strict";

import { setOrigin } from "./time.js";

const MSG_CAP = 50000;
const LOG_CAP = 50000;
const QUALITY_CAP = 10800;
const SEED_HISTORY_CAP = 8;
const SEED_PIN_HOLD_MS = 2000;
export const SEED_AUTO = -1;

function initialState() {
  return {
    conn: "connecting",
    source: "",

    t0WallMS: null,
    t0Estimated: false,
    t0Source: "",
    t: 0,

    control: null,
    tuning: {},

    seedHistory: [],
    pinnedSeed: null,

    quality: {},
    qualityHistory: [],
    markers: [],

    windows: {},
    broadcastActive: false,

    messages: [],
    msgSeq: 0,
    selectedSeq: null,

    tch: {},

    logEntries: [],

    toggles: { squelch: true, broadcastStrict: true, cfo: true },

    constellation: [],
  };
}

function createStore() {
  const state = initialState();
  const subs = new Map();
  let pending = new Set();
  let flushQueued = false;

  function on(topic, fn) {
    if (!subs.has(topic)) subs.set(topic, new Set());
    subs.get(topic).add(fn);
    return () => subs.get(topic).delete(fn);
  }

  function touch(topic) {
    pending.add(topic);
    if (flushQueued) return;
    flushQueued = true;
    requestAnimationFrame(() => {
      flushQueued = false;
      const topics = pending;
      pending = new Set();
      for (const t of topics) {
        const set = subs.get(t);
        if (!set) continue;
        for (const fn of set) {
          try { fn(state); } catch (e) { console.error(`pane[${t}]`, e); }
        }
      }
    });
  }

  const logGroup = type =>
    type === "control_msg" ? "control"
      : (type === "broadcast_start" || type === "broadcast_end"
        || type === "broadcast_update") ? "broadcast"
        : (type === "audio_status" || type === "iq_status") ? "audio"
          : "system";
  const isCrcFail = ev => ev.type === "control_msg" && !ev.crc_ok;

  function addLog(t, text, group = "system", crcFail = false) {
    state.logEntries.push({ t, group, text, crcFail });
    if (state.logEntries.length > LOG_CAP) {
      state.logEntries.splice(0, state.logEntries.length - LOG_CAP);
    }
    touch("log");
  }

  function recordSeed(ev) {
    if (typeof ev.seed !== "number" || ev.seed < 0) return;
    const e = state.seedHistory.find(x => x.seed === ev.seed);
    if (e) {
      e.count++;
      e.lastT = ev.t;
      if (ev.candidates && ev.candidates.length) e.candidates = ev.candidates;
    } else {
      state.seedHistory.push({
        seed: ev.seed, count: 1, firstT: ev.t, lastT: ev.t,
        candidates: ev.candidates || [],
      });
    }
    state.seedHistory.sort((a, b) => b.lastT - a.lastT);
    if (state.seedHistory.length > SEED_HISTORY_CAP) {
      state.seedHistory.length = SEED_HISTORY_CAP;
    }
    touch("seed");
  }

  function addMarker(t, kind, text) {
    state.markers.push({ t, kind, text });
    if (state.markers.length > 2000) state.markers.shift();
    touch("markers");
  }

  function mergeQuality(cum, from, add) {
    if (typeof add !== "string" || add === "") return cum;
    const at = typeof from === "number" && from >= 0 ? from : cum.length;
    if (at > cum.length) cum += "?".repeat(at - cum.length);
    return cum.slice(0, at) + add;
  }

  function win(id) {
    return (state.windows[id] ||= {
      t_start: null, t_end: null, wall_start: null, wall_end: null,
      target: null, audio: null, iq: null,
    });
  }

  function apply(ev) {
    switch (ev.type) {
      case "snapshot": applySnapshot(ev); break;

      case "control_msg": {
        state.t = ev.t;
        const m = { ...ev, seq: state.msgSeq++ };
        state.messages.push(m);
        if (state.messages.length > MSG_CAP) {
          state.messages.splice(0, state.messages.length - MSG_CAP);
        }
        touch("messages");
        break;
      }

      case "control_summary":
        state.control = ev.control;
        state.t = ev.t;
        syncPinnedSeed(ev.control);
        touch("control");
        break;

      case "quality": {
        state.quality = ev;
        state.qualityHistory.push({
          t: ev.t,
          cfo: ev.cfo_hz, evm: ev.evm_median, evmBest: ev.evm_best,
          crc: ev.crc_ok_rate, msgs: ev.msgs_per_s, level: ev.level_dbfs,
          overflows: ev.overflows, locked: ev.sync_locked,
        });
        if (state.qualityHistory.length > QUALITY_CAP) state.qualityHistory.shift();
        touch("quality");
        break;
      }

      case "constellation":
        state.constellation = ev.points || [];
        touch("constellation");
        break;

      case "seed_detected": {
        const names = (ev.candidates || []).map(x => x[1]).join("、");
        recordSeed(ev);
        addLog(ev.t, `★ スクランブル値自動判定: ${ev.seed}（候補: ${names}）`, "system");
        addMarker(ev.t, "seed", `seed ${ev.seed} 確定`);
        break;
      }

      case "tch_second":
        for (const [k, v] of Object.entries(ev.counts || {})) {
          state.tch[k] = (state.tch[k] || 0) + v;
        }
        touch("tch");
        break;

      case "broadcast_start": {
        const w = win(ev.window_id);
        w.t_start = ev.t; w.t_end = null;
        w.wall_start = ev.wall_ms; w.wall_end = null;
        w.target = ev.target || null;
        state.broadcastActive = true;
        addMarker(ev.t, "broadcast", `通報 #${ev.window_id} 開始`);
        touch("windows");
        break;
      }
      case "broadcast_update":
        win(ev.window_id).target = ev.target || null;
        touch("windows");
        break;
      case "broadcast_end": {
        const w = win(ev.window_id);
        w.t_end = ev.t; w.wall_end = ev.wall_ms;
        state.broadcastActive = false;
        addMarker(ev.t, "broadcast", `通報 #${ev.window_id} 終了`);
        touch("windows");
        break;
      }
      case "audio_status": {
        const w = win(ev.window_id);
        const cum = typeof w.audio?.quality === "string" ? w.audio.quality : "";
        w.audio = { ...ev, quality: mergeQuality(cum, ev.quality_from, ev.quality) };
        touch("windows");
        break;
      }
      case "iq_status":
        win(ev.window_id).iq = ev;
        touch("windows");
        break;

      case "log":
        if (/信号喪失|途絶|喪失/.test(ev.text)) addMarker(ev.t, "loss", ev.text);
        else if (/CFO/.test(ev.text)) addMarker(ev.t, "cfo", ev.text);
        break;
    }

    const line = logLine(ev);
    if (line) addLog(ev.t ?? state.t, line, logGroup(ev.type), isCrcFail(ev));
  }

  function applySnapshot(s) {
    setOrigin(s.t0_wall_ms, s.t0_estimated, s.t0_source);
    state.t0WallMS = s.t0_wall_ms ?? null;
    state.t0Estimated = !!s.t0_estimated;
    state.t0Source = s.t0_source || "";
    state.t = s.t ?? 0;
    state.source = s.source || "";
    state.control = s.control;
    state.tuning = s.tuning || {};
    syncPinnedSeed(s.control);
    state.quality = s.quality || {};
    state.tch = s.tch_counts || {};
    state.windows = {};
    for (const [k, v] of Object.entries(s.windows || {})) state.windows[Number(k)] = v;
    state.broadcastActive = !!(s.broadcast && s.broadcast.active);
    if (s.broadcast && s.broadcast.active && s.broadcast.window_id != null
      && !state.windows[s.broadcast.window_id]) {
      win(s.broadcast.window_id).t_start = s.broadcast.started_t;
    }
    if (s.squelch_enabled !== undefined) state.toggles.squelch = !!s.squelch_enabled;
    if (s.broadcast_strict !== undefined) state.toggles.broadcastStrict = !!s.broadcast_strict;
    if (s.cfo_enabled !== undefined) state.toggles.cfo = !!s.cfo_enabled;

    state.logEntries = [];
    state.messages = [];
    state.msgSeq = 0;
    for (const ev of s.recent_log || []) {
      if (ev.type === "control_msg") {
        state.messages.push({ ...ev, seq: state.msgSeq++ });
      }
      const text = logLine(ev);
      if (text) {
        state.logEntries.push({
          t: ev.t ?? 0, group: logGroup(ev.type), text, crcFail: isCrcFail(ev),
        });
      }
    }
    for (const topic of ["snapshot", "control", "quality", "windows", "messages",
      "log", "tch", "toggles", "conn", "seed"]) touch(topic);
  }

  function logLine(ev) {
    switch (ev.type) {
      case "control_msg":
        return `[${ev.channel}] ${ev.name}${ev.crc_ok ? "" : " (CRC×)"} ${ev.raw_hex}`;
      case "broadcast_start":
        return `◆ 通報開始 #${ev.window_id}`
          + (ev.target && ev.target.label ? `（報知対象: ${ev.target.label}）` : "");
      case "broadcast_end": return `◇ 通報終了 #${ev.window_id}`;
      case "broadcast_update":
        return `◆ 通報 #${ev.window_id} 報知対象を確定: ${ev.target?.label ?? "—"}`;
      case "audio_status":
        return `♪ 通報#${ev.window_id} frames=${ev.frames} crc7ok=${ev.crc7_ok}`
          + (ev.filled ? ` filled=${ev.filled}` : "") + ` ${ev.note || ""}`;
      case "iq_status":
        return `◎ 通報#${ev.window_id} IQ録音 ${ev.seconds}s @ ${ev.fs}Hz`
          + (ev.done ? " (確定)" : "") + (ev.note ? ` ${ev.note}` : "");
      case "log": return ev.text;
      default: return null;
    }
  }

  let backoff = 500;
  function connect() {
    const proto = location.protocol === "https:" ? "wss" : "ws";
    const ws = new WebSocket(`${proto}://${location.host}/ws`);
    ws.onopen = () => {
      backoff = 500;
      state.conn = "open";
      touch("conn");
    };
    ws.onmessage = e => {
      let ev;
      try { ev = JSON.parse(e.data); } catch { return; }
      apply(ev);
    };
    ws.onclose = () => {
      state.conn = "reconnecting";
      touch("conn");
      setTimeout(connect, backoff = Math.min(backoff * 2, 10000));
    };
    ws.onerror = () => ws.close();
  }

  function select(seq) {
    state.selectedSeq = seq;
    touch("selection");
  }
  function selectedMessage() {
    if (state.selectedSeq === null) return null;
    for (let i = state.messages.length - 1; i >= 0; i--) {
      if (state.messages[i].seq === state.selectedSeq) return state.messages[i];
    }
    return null;
  }

  const busy = {};
  async function toggle(key, url, respKey) {
    if (busy[key]) return;
    busy[key] = true;
    const next = !state.toggles[key];
    state.toggles[key] = next;
    touch("toggles");
    try {
      const r = await fetch(`${url}?enabled=${next}`, { method: "POST" });
      const j = await r.json();
      if (typeof j[respKey] === "boolean") state.toggles[key] = j[respKey];
    } catch {
    } finally {
      busy[key] = false;
      touch("toggles");
    }
  }

  async function resetCFO() {
    try { await fetch("/api/cfo/reset", { method: "POST" }); } catch {  }
    addMarker(state.t, "cfo", "CFO 手動再捕捉");
  }


  let pinHoldUntil = 0;

  function syncPinnedSeed(control) {
    if (Date.now() < pinHoldUntil) return;
    const v = control && control.seed_pinned && typeof control.seed === "number"
      ? control.seed : null;
    if (v === state.pinnedSeed) return;
    state.pinnedSeed = v;
    touch("seed");
  }

  async function pinSeed(seed) {
    const v = typeof seed === "number" && seed >= 0 && seed < 512 ? seed : SEED_AUTO;
    state.pinnedSeed = v === SEED_AUTO ? null : v;
    pinHoldUntil = Date.now() + SEED_PIN_HOLD_MS;
    touch("seed");
    try {
      const r = await fetch(`/api/seed?value=${v}`, { method: "POST" });
      const j = await r.json();
      if (typeof j.seed === "number") state.pinnedSeed = j.seed_pinned ? j.seed : null;
    } catch {
    }
    addMarker(state.t, "seed",
      v === SEED_AUTO ? "スクランブル値を自動判定へ戻す" : `スクランブル値 ${v} を手動固定`);
    touch("seed");
  }

  return {
    state, on, connect, select, selectedMessage, addLog, addMarker,
    toggleSquelch: () => toggle("squelch", "/api/squelch", "squelch_enabled"),
    toggleBroadcastStrict: () =>
      toggle("broadcastStrict", "/api/broadcast_strict", "broadcast_strict"),
    toggleCFO: () => toggle("cfo", "/api/cfo", "cfo_enabled"),
    resetCFO,
    pinSeed,
  };
}

export const store = createStore();
