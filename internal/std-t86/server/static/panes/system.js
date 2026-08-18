
"use strict";

import { fmtClock, fmtClockMS, fmtDateMS } from "../time.js";

const esc = s => String(s).replace(/[&<>"]/g,
  c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));

const CONN = {
  connecting: { cls: "idle", text: "接続中…" },
  open: { cls: "ok", text: "接続" },
  reconnecting: { cls: "warn", text: "再接続中…" },
};

export function init(store) {
  const body = document.getElementById("system-body");
  const sub = document.getElementById("system-sub");
  if (!body) return;

  function render() {
    const s = store.state;
    const q = s.quality || {};
    const c = s.control || {};
    const conn = CONN[s.conn] || CONN.connecting;

    let origin = s.t0Source ? esc(s.t0Source) : "—";
    if (s.t0Estimated) origin += ` <span class="badge warn">推定</span>`;
    const base = s.t0WallMS === null || s.t0WallMS === undefined
      ? "—" : `${fmtDateMS(s.t0WallMS)} ${fmtClockMS(s.t0WallMS, 3)}`;

    const over = q.overflows ?? 0;
    const overCell = over
      ? `<span class="badge crit">${over}</span>`
      : `<span class="badge ok">0</span>`;
    const sync = q.sync_locked
      ? `<span class="badge ok">同期 OK</span>`
      : `<span class="badge warn">同期 なし</span>`;

    const total = c.total ?? 0, valid = c.valid ?? 0, crcOK = c.crc_ok ?? 0;
    const crcRate = total ? ` <span class="votes">${(crcOK / total * 100).toFixed(1)}%</span>` : "";

    const rows = [
      ["ソース", esc(s.source || "—")],
      ["時刻基準", origin],
      ["基準時刻", base],
      ["処理位置", fmtClock(s.t, 0)],
      ["同期", sync],
      ["overflow 累計", overCell],
      ["受信メッセージ", `${valid} / ${total}`],
      ["CRC 一致", `${crcOK}${crcRate}`],
      ["接続", `<span class="badge ${conn.cls}">${conn.text}</span>`],
    ];
    body.innerHTML = `<table class="kv">`
      + rows.map(([k, v]) => `<tr><th>${k}</th><td>${v}</td></tr>`).join("")
      + `</table>`;

    if (sub) {
      sub.textContent = over ? `取りこぼし ${over}`
        : (s.conn !== "open" ? conn.text
          : (q.sync_locked ? "正常" : "同期待ち"));
    }
  }

  for (const topic of ["quality", "control", "conn", "snapshot"]) store.on(topic, render);
  render();
}
