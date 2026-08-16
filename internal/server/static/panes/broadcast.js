
"use strict";

import { fmtClock, fmtClockMS, fmtDuration, wallMS } from "../time.js";
import { cssVar, fitCanvas } from "../draw.js";

const esc = s => String(s).replace(/[&<>"]/g,
  c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));

const NOTE = "S-Codec 伝送路 FEC は実運用波の実装を同定済み。"
  + "CRC 落ちフレームは PLC で補間して復号を続け、CRC7 統計を併記する。";

const BAR_NOTE = "品質帯は 1 フレーム = 20ms。横幅よりフレーム数が多いときは"
  + "1 ピクセルに複数フレームが入るので、最悪値（ミュート > 欠落 > CRC 不一致 > 正常）"
  + "を優先して描く"
  + "（短い異常が平均で消えないようにするため）。帯にマウスを乗せるとその位置の"
  + "フレーム番号・状態・実時刻が出る。";

const FRAME_MS = 20;

const GLYPHS = {
  "o": { rank: 0, css: "--ok-soft", label: "正常（CRC7 一致）" },
  "x": { rank: 2, css: "--warn", label: "CRC7 不一致 → PLC 反復" },
  "X": { rank: 4, css: "--crit", label: "CRC7 不一致 → ミュート" },
  "-": { rank: 3, css: "--data", label: "欠落（取りこぼし）→ PLC 反復" },
  "_": { rank: 5, css: "--ink", label: "欠落 → ミュート" },
  "?": { rank: 1, css: "--grid", label: "不明（列が届かなかった区間）" },
};
const UNKNOWN = "?";

const LEGEND = ["o", "x", "X", "-", "_"].map(ch => {
  const g = GLYPHS[ch];
  return `<span><i style="background:var(${g.css})"></i>`
    + `<b class="mono">${esc(ch)}</b> ${esc(g.label)}</span>`;
}).join("");

function num(v, unit = "", digits = 1) {
  return (v === null || v === undefined || !isFinite(v))
    ? "—" : `${Number(v).toFixed(digits)}${unit}`;
}

function clockOf(wall, t) {
  if (typeof wall === "number") return fmtClockMS(wall, 2);
  if (typeof t === "number") return fmtClock(t, 2);
  return "…";
}

function startMSOf(w) {
  if (typeof w.wall_start === "number") return w.wall_start;
  return wallMS(w.t_start);
}

function targetBadge(tg) {
  if (!tg || !tg.label) return "";
  const cls = tg.kind === "selective" ? "warn"
    : tg.kind === "all" ? "info" : "idle";
  let out = ` <span class="badge ${cls}">対象: ${esc(tg.label)}</span>`;
  if (tg.call_no !== null && tg.call_no !== undefined) {
    out += ` <span class="badge idle">呼番号 ${esc(tg.call_no)}</span>`;
  }
  return out;
}

function audioLine(id, a) {
  if (!a || !a.frames) return `<div class="meta">音声フレーム待ち…</div>`;
  const r = typeof a.crc7_rate === "number" ? a.crc7_rate : a.crc7_ok / a.frames;
  const parts = [
    `音声 ${a.frames} フレーム`,
    `CRC7一致 ${a.crc7_ok} (${(r * 100).toFixed(1)}%)`,
  ];
  if (a.filled) parts.push(`欠落補間 ${a.filled}`);
  if (a.stale) parts.push(`遅延排出 ${a.stale}`);
  if (a.plc_repeat) parts.push(`PLC反復 ${a.plc_repeat}`);
  parts.push(`復号 ${num(a.decoded_seconds, "s")}`);
  let line = `<div class="meta">${parts.join(" / ")}`;
  if (a.decode_attempted) line += ` — <a href="/api/audio/${id}.wav">WAV</a>`;
  line += `</div>`;
  if (a.plc_mute) {
    line += `<div class="meta"><span class="badge warn">`
      + `長区間の欠落: ミュート ${a.plc_mute} フレーム`
      + ` (${(a.plc_mute * FRAME_MS / 1000).toFixed(1)}s)</span></div>`;
  }
  if (a.note) line += `<div class="meta">${esc(a.note)}</div>`;
  if (a.wav_path) line += `<div class="meta">保存先: ${esc(a.wav_path)}</div>`;
  return line;
}

function iqLine(id, iq) {
  if (!iq || !iq.path) return "";
  const sec = iq.seconds === null || iq.seconds === undefined ? "?" : num(iq.seconds, "", 1);
  const fs = iq.fs === null || iq.fs === undefined ? "?" : String(iq.fs);
  let line = `<div class="meta">IQ 録音: `
    + `<a href="/api/iq/${id}.wav">${esc(sec)}s @ ${esc(fs)}Hz</a>`;
  if (!iq.done) line += "（録音中…）";
  line += `</div>`;
  if (iq.note) line += `<div class="meta">${esc(iq.note)}</div>`;
  return line;
}

export function init(store) {
  const body = document.getElementById("broadcast-body");
  const sub = document.getElementById("broadcast-sub");
  if (!body) return;

  body.innerHTML = `<div data-b="list"></div>`
    + `<div class="note" data-b="note"></div>`;
  const list = body.querySelector('[data-b="list"]');
  const note = body.querySelector('[data-b="note"]');
  note.textContent = NOTE;

  const qualityOf = a => (a && typeof a.quality === "string") ? a.quality : "";


  const rankOf = ch => (GLYPHS[ch] || GLYPHS[UNKNOWN]).rank;

  function drawBar(canvas) {
    const st = canvas.__q;
    if (!st) return;
    const { ctx, w, h } = fitCanvas(canvas);
    ctx.fillStyle = cssVar("--surface", "#ffffff");
    ctx.fillRect(0, 0, w, h);
    const q = st.q, n = q.length;
    if (!n) return;

    const colors = {};
    for (const [ch, g] of Object.entries(GLYPHS)) colors[ch] = cssVar(g.css, "#888");

    let runStart = 0, runCh = null;
    for (let x = 0; x < w; x++) {
      const i0 = Math.floor(x * n / w);
      const i1 = Math.min(n, Math.max(i0 + 1, Math.floor((x + 1) * n / w)));
      let ch = q[i0], best = rankOf(ch);
      for (let i = i0 + 1; i < i1; i++) {
        const r = rankOf(q[i]);
        if (r > best) { best = r; ch = q[i]; }
      }
      if (runCh === null) { runCh = ch; runStart = x; continue; }
      if (ch !== runCh) {
        ctx.fillStyle = colors[runCh] || colors[UNKNOWN];
        ctx.fillRect(runStart, 0, x - runStart, h);
        runCh = ch; runStart = x;
      }
    }
    if (runCh !== null) {
      ctx.fillStyle = colors[runCh] || colors[UNKNOWN];
      ctx.fillRect(runStart, 0, w - runStart, h);
    }
  }

  function hover(canvas, ev) {
    const st = canvas.__q;
    if (!st || !st.q.length) return;
    const rect = canvas.getBoundingClientRect();
    if (rect.width <= 0) return;
    const x = Math.max(0, Math.min(rect.width - 1, ev.clientX - rect.left));
    const i = Math.min(st.q.length - 1, Math.floor(x * st.q.length / rect.width));
    const ch = st.q[i];
    const g = GLYPHS[ch] || GLYPHS[UNKNOWN];
    const clock = st.startMS === null || st.startMS === undefined
      ? "" : ` ${fmtClockMS(st.startMS + i * FRAME_MS, 2)}`;
    canvas.title = `フレーム ${i} / ${st.q.length}`
      + `（先頭から ${(i * FRAME_MS / 1000).toFixed(2)}s）${clock}`
      + ` — ${ch} ${g.label}`;
  }

  const cards = new Map();

  const ro = typeof ResizeObserver === "function"
    ? new ResizeObserver(entries => { for (const e of entries) drawBar(e.target); })
    : null;
  if (!ro) {
    window.addEventListener("resize", () => {
      for (const c of cards.values()) drawBar(c.canvas);
    });
  }

  function ensureCard(id) {
    let c = cards.get(id);
    if (c) return c;
    const root = document.createElement("div");
    root.className = "win";
    const text = document.createElement("div");

    const bar = document.createElement("div");
    bar.style.display = "none";
    const canvas = document.createElement("canvas");
    canvas.className = "qbar";
    const legend = document.createElement("div");
    legend.className = "qlegend";
    legend.innerHTML = LEGEND;
    bar.append(canvas, legend);
    root.append(text, bar);

    canvas.addEventListener("mousemove", ev => hover(canvas, ev));
    if (ro) ro.observe(canvas);

    c = { root, text, bar, canvas };
    cards.set(id, c);
    return c;
  }

  function dropCard(id) {
    const c = cards.get(id);
    if (!c) return;
    if (ro) ro.unobserve(c.canvas);
    c.root.remove();
    cards.delete(id);
  }

  function render() {
    const wins = store.state.windows || {};
    const ids = Object.keys(wins).map(Number).filter(n => !isNaN(n))
      .sort((a, b) => b - a);
    const active = ids.filter(id => wins[id] && wins[id].t_end == null).length;

    if (sub) {
      sub.textContent = ids.length
        ? `${ids.length} 件${active ? ` / 受信中 ${active}` : ""}` : "待機中";
    }

    for (const id of [...cards.keys()]) if (!wins[id]) dropCard(id);

    if (!ids.length) {
      list.innerHTML = `<div class="note">通報開始指示(0x22) 待ち…</div>`;
      note.textContent = NOTE;
      return;
    }
    if (list.firstElementChild && !list.firstElementChild.classList.contains("win")) {
      list.innerHTML = "";
    }

    for (const id of ids) {
      const w = wins[id] || {};
      const live = w.t_end == null;
      const start = clockOf(w.wall_start, w.t_start);
      const end = live ? "…" : clockOf(w.wall_end, w.t_end);
      const dur = (!live && typeof w.t_start === "number" && typeof w.t_end === "number")
        ? `<span class="votes">（${fmtDuration(w.t_end - w.t_start)}）</span>` : "";
      const liveBadge = live ? ` <span class="badge crit live">受信中</span>` : "";

      const c = ensureCard(id);
      c.root.className = "win" + (live ? " active" : "");
      c.text.innerHTML = `<b>通報 #${id}</b>${liveBadge}${targetBadge(w.target)}`
        + `<div class="times">${start} → ${end}${dur}</div>`
        + audioLine(id, w.audio) + iqLine(id, w.iq);

      const q = qualityOf(w.audio);
      if (q.length) {
        c.bar.style.display = "";
        c.canvas.__q = { q, startMS: startMSOf(w) };
        drawBar(c.canvas);
      } else {
        c.bar.style.display = "none";
        c.canvas.__q = null;
      }

      list.append(c.root);
    }

    note.textContent = `${NOTE} ${BAR_NOTE}`;
  }

  store.on("snapshot", render);

  store.on("windows", render);
  render();
}
