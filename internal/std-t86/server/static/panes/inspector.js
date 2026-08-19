
"use strict";

import { fmtClock, fmtClockMS, wallMS, sameDayMS, fmtDateMS } from "../time.js";
import { decompose, targetVerdict } from "../bits.js";

const ROW_H_INIT = 20;
const OVERSCAN = 12;

const esc = s => String(s).replace(/[&<>"]/g,
  c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));

const CHANNELS = ["BCCH", "PCH", "SCCH", "FACCH", "SB", "CAC"];

const COLGROUP = "<colgroup>"
  + '<col style="width:98px"><col style="width:42px"><col style="width:56px">'
  + '<col style="width:52px"><col style="width:170px"><col style="width:44px">'
  + '<col style="width:52px"><col style="width:120px"><col></colgroup>';
const HEADER_CELLS = ["時刻", "SW", "EVM", "CH", "種別", "CRC", "BI", "対象", "HEX"];

export function init(store) {
  const toolbar = document.getElementById("inspector-toolbar");
  const scroll = document.getElementById("inspector-scroll");
  const spacer = document.getElementById("inspector-spacer");
  const rowsEl = document.getElementById("inspector-rows");
  const detailEl = document.getElementById("inspector-detail");
  const subEl = document.getElementById("inspector-sub");
  const headEl = document.getElementById("inspector-head");
  if (!toolbar || !scroll || !rowsEl) return;

  if (headEl) {
    headEl.innerHTML = '<table class="msgs">' + COLGROUP + "<thead><tr>"
      + HEADER_CELLS.map(h => `<th>${h}</th>`).join("") + "</tr></thead></table>";
  }

  const ui = {
    channels: new Set(CHANNELS),
    showCRCFail: true,
    query: "",
    follow: true,
    allBits: false,
  };

  let filtered = [];
  let dirty = true;

  toolbar.innerHTML =
    CHANNELS.map(c => `<span class="chip on" data-ch="${c}">${c}</span>`).join("")
    + `<span class="chip on" data-crc="1">CRC×</span>`
    + `<span style="flex:1"></span>`
    + `<input type="search" id="insp-q" placeholder="検索: hex / 種別 / フィールド値">`
    + `<span class="chip on" data-follow="1">FOLLOW</span>`
    + `<span class="chip" data-allbits="1">全ビット</span>`
    + `<span class="chip" data-export="csv">CSV</span>`
    + `<span class="chip" data-export="jsonl">JSONL</span>`;

  toolbar.addEventListener("click", e => {
    const el = e.target.closest(".chip");
    if (!el) return;
    if (el.dataset.ch) {
      const c = el.dataset.ch;
      if (ui.channels.has(c)) ui.channels.delete(c); else ui.channels.add(c);
      el.classList.toggle("on", ui.channels.has(c));
      dirty = true; render();
    } else if (el.dataset.crc) {
      ui.showCRCFail = !ui.showCRCFail;
      el.classList.toggle("on", ui.showCRCFail);
      dirty = true; render();
    } else if (el.dataset.follow) {
      setFollow(!ui.follow);
      render();
    } else if (el.dataset.allbits) {
      ui.allBits = !ui.allBits;
      el.classList.toggle("on", ui.allBits);
      renderDetail();
    } else if (el.dataset.export) {
      exportRows(el.dataset.export);
    }
  });

  const qEl = toolbar.querySelector("#insp-q");
  let qTimer = 0;
  qEl.addEventListener("input", () => {
    clearTimeout(qTimer);
    qTimer = setTimeout(() => {
      ui.query = qEl.value.trim().toLowerCase();
      dirty = true; render();
    }, 150);
  });

  let selfTop = -1;
  let lastTop = 0;

  function scrollToBottom() {
    scroll.scrollTop = scroll.scrollHeight;
    selfTop = scroll.scrollTop;
    lastTop = selfTop;
  }

  function setFollow(on) {
    if (ui.follow === on) return;
    ui.follow = on;
    const chip = toolbar.querySelector("[data-follow]");
    if (chip) chip.classList.toggle("on", on);
    updateSub();
  }

  scroll.addEventListener("scroll", () => {
    const top = scroll.scrollTop;
    const atBottom = scroll.scrollHeight - top - scroll.clientHeight < 4;
    const bySelf = Math.abs(top - selfTop) < 2;
    if (!bySelf) {
      if (atBottom) setFollow(true);
      else if (top < lastTop - 1) setFollow(false);
    }
    lastTop = top;
    renderRows();
  });

  rowsEl.addEventListener("pointerdown", e => {
    const tr = e.target.closest("tr[data-seq]");
    if (!tr) return;
    store.select(Number(tr.dataset.seq));
    setFollow(false);
  });

  function matches(m) {
    if (!ui.channels.has(m.channel || "CAC")) return false;
    if (!m.crc_ok && !ui.showCRCFail) return false;
    if (ui.query) {
      const hay = [
        m.raw_hex, m.name, m.channel, m.sw,
        "0x" + Number(m.msg_type).toString(16).padStart(2, "0"),
        ...Object.entries(m.fields || {}).map(([k, v]) => `${k}=${v}`),
      ].join(" ").toLowerCase();
      if (!hay.includes(ui.query)) return false;
    }
    return true;
  }

  function refilter(state) {
    filtered = state.messages.filter(matches);
    dirty = false;
  }

  function updateSub() {
    if (!subEl) return;
    subEl.textContent =
      `${filtered.length} / ${store.state.messages.length} 通`
      + (ui.follow ? "" : "　追従停止中（FOLLOW で再開）");
  }

  let rowH = ROW_H_INIT;

  function measureRowH() {
    const tr = rowsEl.querySelector("tbody tr[data-seq]");
    if (!tr) return false;
    const h = tr.getBoundingClientRect().height;
    if (h > 4 && Math.abs(h - rowH) > 0.5) {
      rowH = h;
      return true;
    }
    return false;
  }

  function layout() {
    spacer.style.height = (filtered.length * rowH) + "px";
    if (ui.follow) scrollToBottom();
    renderRows();
  }

  function render() {
    const st = store.state;
    if (dirty) refilter(st);
    layout();
    if (measureRowH()) layout();
    updateSub();
  }

  function renderRows() {
    const top = scroll.scrollTop;
    const h = scroll.clientHeight || 420;
    let a = Math.max(0, Math.floor(top / rowH) - OVERSCAN);
    let b = Math.min(filtered.length, Math.ceil((top + h) / rowH) + OVERSCAN);
    rowsEl.style.transform = `translateY(${a * rowH}px)`;

    const sel = store.state.selectedSeq;
    const out = [];
    out.push('<table class="msgs">' + COLGROUP + "<tbody>");
    let prevWall = a > 0 ? wallMS(filtered[a - 1]?.t) : null;
    for (let i = a; i < b; i++) {
      const m = filtered[i];
      const w = wallMS(m.t);
      if (prevWall !== null && w !== null && !sameDayMS(prevWall, w)) {
        out.push(`<tr class="daybreak"><td colspan="9">───── ${fmtDateMS(w)} ─────</td></tr>`);
      }
      prevWall = w;
      const tv = targetVerdict(m, { idValidBits: idValidBits() });
      out.push(
        `<tr data-seq="${m.seq}" class="${m.seq === sel ? "sel" : ""}`
        + `${m.crc_ok ? "" : " crcfail"}">`
        + `<td>${fmtClock(m.t)}</td>`
        + `<td>${esc(m.sw || "—")}</td>`
        + `<td>${m.evm ? m.evm.toFixed(0) + "%" : "—"}</td>`
        + `<td>${esc(m.channel || "—")}</td>`
        + `<td>0x${Number(m.msg_type).toString(16).padStart(2, "0")} ${esc(m.name)}</td>`
        + `<td><span class="pill ${m.crc_ok ? "ok" : "bad"}">${m.crc_ok ? "OK" : "×"}</span></td>`
        + `<td>${m.busy ? '<span class="pill bad">BI</span>' : "—"}</td>`
        + `<td>${tv ? esc(tv.label) : "—"}</td>`
        + `<td>${esc(hexSpaced(m.raw_hex))}</td>`
        + `</tr>`);
    }
    out.push("</tbody></table>");
    rowsEl.innerHTML = out.join("");
  }

  const hexSpaced = h => String(h || "").replace(/(..)/g, "$1 ").trim();

  let learnedValidBits = 0;
  function idValidBits() { return learnedValidBits; }
  function learn(state) {
    for (let i = state.messages.length - 1, n = 0; i >= 0 && n < 200; i--, n++) {
      const m = state.messages[i];
      if (!m.crc_ok) continue;
      const v = m.fields && m.fields["子局識別番号有効ビット数"];
      if (typeof v === "number" && v >= 0 && v <= 8) { learnedValidBits = 8 + v; return; }
    }
  }

  function renderDetail() {
    const m = store.selectedMessage();
    if (!m) {
      detailEl.innerHTML = '<div class="note">行を選ぶと 104bit のフィールド分解を出します。'
        + '<br>予備・予約は既定で畳み、「全ビット」で展開します（0 以外なら畳んだままでも印を出します）。</div>';
      return;
    }
    const ctx = { idValidBits: idValidBits() };
    let d;
    try {
      d = decompose(m, ctx);
    } catch (e) {
      detailEl.innerHTML = `<div class="note">分解できません: ${esc(String(e))}</div>`;
      return;
    }
    const tv = targetVerdict(m, ctx);
    const wms = wallMS(m.t);

    const out = [];
    out.push(`<div class="dh">0x${Number(m.msg_type).toString(16).padStart(2, "0")} `
      + `${esc(m.name)}${m.section ? " — " + esc(m.section) : ""}</div>`);
    out.push(`<div class="dsub">${fmtClockMS(wms)} ${fmtDateMS(wms)}`
      + ` · ${esc(m.channel || "—")} · SW ${esc(m.sw || "—")}`
      + ` · CRC16 ${m.crc_ok ? "一致 ✓" : "不一致 ×"}`
      + ` · EVM ${m.evm != null ? m.evm.toFixed(1) + "%" : "—"}`
      + ` · 電力 ${m.power_db != null ? m.power_db.toFixed(1) + "dB" : "—"}`
      + ` · 相関 ${m.corr != null ? m.corr.toFixed(3) : "—"}</div>`);

    if (tv) {
      out.push(`<div class="why"><b>報知対象: ${esc(tv.label)}</b><br>${esc(tv.reason)}`
        + (tv.detail && tv.detail.length
          ? "<br>" + tv.detail.map(x => "※ " + esc(x)).join("<br>") : "")
        + `</div>`);
    }
    if (!m.crc_ok) {
      out.push('<div class="why" style="background:var(--crit-soft);'
        + 'border-left-color:var(--crit);color:var(--crit)">'
        + 'CRC16 不一致。ビタビ後も誤りが残っているので、以下のフィールド値は'
        + '信用できません（ビット位置の参考としてのみ読むこと）。</div>');
    }

    let hiddenNonZero = 0, hidden = 0;
    for (const g of d.groups) {
      const rows = [];
      for (const r of g.rows) {
        const isSpare = r.kind === "spare" || r.kind === "reserved";
        const nonZero = isSpare && r.value !== 0 && r.value !== null
          && !/^0*$/.test(r.hex || "0");
        if (isSpare && !ui.allBits) {
          hidden++;
          if (nonZero) hiddenNonZero++;
          continue;
        }
        rows.push(`<div class="row${isSpare ? " spare" : ""}${nonZero ? " nonzero" : ""}"`
          + ` title="${esc(r.range)}${r.note ? " / " + esc(r.note) : ""}">`
          + `<span class="pos">${esc(r.pos)}</span>`
          + `<span class="f">${esc(r.name)}${nonZero ? " ⚠" : ""}</span>`
          + `<span class="v">${esc(r.display)}</span></div>`);
      }
      if (rows.length) {
        out.push(`<div class="grp">${esc(g.title)}</div>`);
        out.push(rows.join(""));
      }
    }
    if (hidden) {
      out.push(`<div class="note" style="margin-top:6px">`
        + `予備・予約 ${hidden} 項目を畳んでいます`
        + (hiddenNonZero
          ? `（うち <b style="color:var(--warn)">${hiddenNonZero} 項目が 0 以外</b>`
          + ` — 局独自の使用の可能性。「全ビット」で展開）`
          : `（すべて 0）`)
        + `。</div>`);
    }

    out.push(`<div class="bitstrip">${bitStrip(m, d)}</div>`);
    out.push(`<div class="note" style="margin-top:4px">`
      + `全 ${d.totalBits} bit のうち ${d.coverage.named} bit に名前が付いています`
      + `（残り ${d.totalBits - d.coverage.named} bit は未定義）。`
      + ` ストリーム時刻 t=${m.t.toFixed(3)}s / 標本位置 ${m.pos}</div>`);

    detailEl.innerHTML = out.join("");
  }

  function bitStrip(m, d) {
    const hex = String(m.raw_hex || "");
    const out = [];
    for (const seg of d.segments) {
      const a = Math.floor(seg.start / 4), b = Math.ceil(seg.end / 4);
      const part = esc(hex.slice(a, b));
      if (!part) continue;
      if (seg.kind === "spare" || seg.kind === "reserved") out.push(`<i>${part}</i>`);
      else out.push(`<b>${part}</b>`);
    }
    return out.join(" ");
  }

  function exportRows(kind) {
    const rows = filtered;
    let text;
    if (kind === "csv") {
      const head = ["時刻", "日付", "t", "pos", "SW", "EVM", "電力dB", "相関",
        "チャネル", "種別コード", "種別名", "CRC", "BI", "対象", "hex"];
      const q = v => `"${String(v ?? "").replace(/"/g, '""')}"`;
      text = head.map(q).join(",") + "\n" + rows.map(m => {
        const w = wallMS(m.t);
        const tv = targetVerdict(m, { idValidBits: idValidBits() });
        return [fmtClockMS(w), fmtDateMS(w), m.t, m.pos, m.sw, m.evm, m.power_db, m.corr,
          m.channel, "0x" + Number(m.msg_type).toString(16).padStart(2, "0"), m.name,
          m.crc_ok ? "OK" : "NG", m.busy ? 1 : 0, tv ? tv.label : "", m.raw_hex]
          .map(q).join(",");
      }).join("\n");
    } else {
      text = rows.map(m => JSON.stringify({
        wall_ms: wallMS(m.t), t: m.t, pos: m.pos,
        sw: m.sw, evm: m.evm, power_db: m.power_db, corr: m.corr,
        channel: m.channel, msg_type: m.msg_type, name: m.name, section: m.section,
        crc_ok: m.crc_ok, busy: m.busy, raw_hex: m.raw_hex, fields: m.fields,
      })).join("\n");
    }
    const stamp = fmtDateMS(Date.now()) + "_" + fmtClockMS(Date.now(), 0).replace(/:/g, "");
    download(`t86_messages_${stamp}.${kind === "csv" ? "csv" : "jsonl"}`, text);
  }

  function download(name, text) {
    const blob = new Blob([text], { type: "text/plain;charset=utf-8" });
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = name;
    a.click();
    setTimeout(() => URL.revokeObjectURL(a.href), 1000);
  }

  // ---- 購読 ----
  store.on("messages", st => { learn(st); dirty = true; render(); });
  store.on("snapshot", st => { learn(st); dirty = true; render(); renderDetail(); });
  store.on("selection", () => { renderRows(); renderDetail(); });

  render();
  renderDetail();
}
