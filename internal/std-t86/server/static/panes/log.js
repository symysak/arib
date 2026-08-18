
"use strict";

import { fmtClock, fmtDateMS, sameDayMS, wallMS } from "../time.js";

const MAX_SHOWN = 2000;

export function init(store) {
  const pre = document.getElementById("log-body");
  const filtersEl = document.getElementById("log-filters");
  const sub = document.getElementById("log-sub");
  if (!pre) return;

  const filters = {
    control: true, broadcast: true, audio: true, system: true, crcfail: false,
  };
  if (filtersEl) {
    for (const inp of filtersEl.querySelectorAll("input[type=checkbox][data-group]")) {
      filters[inp.dataset.group] = inp.checked;
    }
    filtersEl.addEventListener("change", e => {
      const g = e.target && e.target.dataset ? e.target.dataset.group : null;
      if (!g) return;
      filters[g] = e.target.checked;
      render();
    });
  }

  const match = e => !!filters[e.group] && (!e.crcFail || filters.crcfail);

  let follow = true;
  const atBottom = () => pre.scrollHeight - pre.scrollTop - pre.clientHeight <= 6;
  pre.addEventListener("scroll", () => {
    const now = atBottom();
    if (now !== follow) {
      follow = now;
      renderSub(lastShown, lastTotal);
    }
  });

  let lastShown = 0, lastTotal = 0;

  function renderSub(shown, total) {
    if (!sub) return;
    const parts = [`${shown} / ${total} 行`];
    if (shown < total) parts.push(`末尾 ${MAX_SHOWN} 行のみ表示`);
    if (!follow) parts.push("追従停止中（最下部へ戻ると再開）");
    sub.textContent = parts.join("｜");
  }

  function render() {
    const all = store.state.logEntries || [];
    const tail = [];
    let total = 0;
    for (let i = all.length - 1; i >= 0; i--) {
      const e = all[i];
      if (!match(e)) continue;
      total++;
      if (tail.length < MAX_SHOWN) tail.push(e);
    }
    tail.reverse();

    const lines = [];
    let prevMS = null;
    for (const e of tail) {
      const ms = wallMS(e.t);
      if (ms !== null) {
        if (prevMS === null || !sameDayMS(prevMS, ms)) {
          lines.push(`───── ${fmtDateMS(ms)} ─────`);
        }
        prevMS = ms;
      }
      lines.push(`${fmtClock(e.t)} ${e.text}`);
    }

    const keep = pre.scrollTop;
    pre.textContent = lines.join("\n");
    if (follow) pre.scrollTop = pre.scrollHeight;
    else pre.scrollTop = keep;

    lastShown = tail.length;
    lastTotal = total;
    renderSub(lastShown, lastTotal);
  }

  store.on("log", render);
  render();
}
