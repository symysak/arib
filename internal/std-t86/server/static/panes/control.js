
"use strict";

import { fmtClock } from "../time.js";
import { SEED_AUTO } from "../store.js";

const BCCH_TYPES = new Set([0x10, 0x20, 0x21, 0x40]);
const NOTIFY_TYPE = 0x63;

const SCAN_LIMIT = 3000;
const RECOMPUTE_MS = 1000;

const UPDATE_HL_SEC = 30;

const OTHERS_MAX = 8;

const GROUPS = [
  {
    title: "下り CCH 共通ヘッダ（§4.3.3.1 オクテット1-5）",
    keys: [
      "ビジーフラグ",
      "SF内フレーム番号",
      "状況フラグ1",
      "緊急通信可否",
      "緊急通信以外可否",
      "状況フラグ2",
      "連絡音声通信可否",
      "データ伝送可否",
      "予備(状況フラグ2)",
      "通信統制中",
      "スーパーフレームのフレーム数",
      "スロット使用状況",
      "使用スロット",
      "拡声中放送中",
      "メディア種別",
      "伝送制御部プロトコル",
      "予約(oct2)",
      "予約(oct5)",
    ],
  },
  {
    title: "報知情報 本体（§4.3.4.1 オクテット6-12）",
    keys: [
      "報知情報更新番号",
      "親局送信モード",
      "スーパーフレーム長S",
      "免許人固有情報",
      "PCH数",
      "PCH前のSCCH数",
      "子局識別番号有効ビット数",
      "上り折返識別",
      "緊急連絡通話通信時限",
      "製造者コード",
      "製造者名",
      "予備(oct6)",
      "予約(oct7)",
    ],
  },
];

const ALL_KEYS = GROUPS.flatMap(g => g.keys);


function esc(s) {
  return String(s)
    .replace(/&/g, "&amp;").replace(/</g, "&lt;")
    .replace(/>/g, "&gt;").replace(/"/g, "&quot;");
}

function valueKey(v) {
  try { return JSON.stringify(v); } catch { return String(v); }
}

function fmtValue(key, v) {
  if (v === null || v === undefined) return "—";
  if (typeof v === "boolean") {
    if (key.endsWith("可否")) return v ? "可" : "不可";
    return v ? "はい" : "いいえ";
  }
  if (Array.isArray(v)) return v.length ? v.join(", ") : "なし";
  if (typeof v === "number") {
    if (key === "製造者コード") return `${v}（0x${v.toString(16).toUpperCase().padStart(4, "0")}）`;
    if (key === "免許人固有情報") return `${v}（0x${v.toString(16).toUpperCase().padStart(2, "0")}）`;
    if (key === "スロット使用状況") return `${v}（0b${(v & 0x3F).toString(2).padStart(6, "0")}）`;
    return String(v);
  }
  return String(v);
}

function slotsHTML(used) {
  const set = new Set(Array.isArray(used) ? used : []);
  let h = '<div class="slots">';
  for (let i = 0; i < 6; i++) {
    h += `<span class="${set.has(i) ? "used" : ""}">${i}</span>`;
  }
  return h + "</div>";
}

function isAbolished(name) {
  return typeof name === "string" && name.startsWith("旧 ");
}


let stats = null;

let updateNo = { value: null, t: null, changed: false };

function collect(messages) {
  const dist = new Map();
  let counted = 0;
  let scanned = 0;
  let latestT = null;

  let prevUpd = null;
  let lastUpd = null;
  let lastUpdT = null;
  let changeT = null;

  const start = Math.max(0, messages.length - SCAN_LIMIT);
  for (let i = start; i < messages.length; i++) {
    const m = messages[i];
    scanned++;
    if (!m || !m.crc_ok || !m.fields) continue;

    if (m.msg_type === NOTIFY_TYPE) continue;
    if (!BCCH_TYPES.has(m.msg_type)) continue;

    counted++;
    latestT = m.t;
    for (const key of ALL_KEYS) {
      const v = m.fields[key];
      if (v === undefined) continue;
      let byValue = dist.get(key);
      if (!byValue) { byValue = new Map(); dist.set(key, byValue); }
      const vk = valueKey(v);
      const e = byValue.get(vk);
      if (e) { e.count++; e.lastT = m.t; } else { byValue.set(vk, { raw: v, count: 1, lastT: m.t }); }
    }

    const upd = m.fields["報知情報更新番号"];
    if (typeof upd === "number") {
      if (prevUpd !== null && upd !== prevUpd) changeT = m.t;
      prevUpd = upd;
      lastUpd = upd;
      lastUpdT = m.t;
    }
  }

  if (lastUpd !== null) {
    if (changeT !== null) {
      updateNo = { value: lastUpd, t: changeT, changed: true };
    } else if (updateNo.value !== null && updateNo.value !== lastUpd) {
      updateNo = { value: lastUpd, t: lastUpdT, changed: true };
    } else if (updateNo.value === null) {
      updateNo = { value: lastUpd, t: lastUpdT, changed: false };
    }
  }

  const fields = new Map();
  for (const [key, byValue] of dist) {
    const entries = [...byValue.values()].sort((a, b) => b.count - a.count);
    const total = entries.reduce((s, e) => s + e.count, 0);
    const top = entries[0];
    fields.set(key, {
      top: { raw: top.raw, text: fmtValue(key, top.raw), count: top.count, lastT: top.lastT },
      total,
      others: entries.slice(1).map(e => ({ text: fmtValue(key, e.raw), count: e.count })),
    });
  }
  return { counted, scanned, fields, latestT };
}


function fieldRow(key, st, latestT) {
  const parts = [];
  if (key === "使用スロット") parts.push(slotsHTML(st.top.raw));
  else parts.push(`<span>${esc(st.top.text)}</span>`);

  let cls = "";
  let titleAttr = "";
  if (st.others.length) {
    const shown = st.others.slice(0, OTHERS_MAX);
    let list = shown.map(o => `${o.text}×${o.count}`).join("、");
    if (st.others.length > shown.length) list += `、…他 ${st.others.length - shown.length} 種`;
    titleAttr = ` title="${esc("他: " + list)}"`;
    parts.push(`<span class="pill bad"${titleAttr}>揺</span>`);
  }

  if (key === "報知情報更新番号" && updateNo.changed && updateNo.t !== null) {
    parts.push(`<span class="stamp">更新 ${esc(fmtClock(updateNo.t, 0))}</span>`);
    if (latestT !== null && latestT - updateNo.t <= UPDATE_HL_SEC) cls = " hl";
  }

  parts.push(`<span class="votes">${st.top.count}/${st.total}</span>`);
  parts.push(`<span class="stamp">${esc(fmtClock(st.top.lastT, 0))}</span>`);

  return `<tr class="${cls.trim()}"${titleAttr}><th>${esc(key)}</th>`
    + `<td>${parts.join(" ")}</td></tr>`;
}

function fieldsHTML(st) {
  if (!st || !st.fields.size) {
    return '<div class="note">BCCH 系メッセージ（報知情報 0x10 / アイドル信号 0x20・0x40 /'
      + " BCCH変更通知 0x21）をまだ CRC 一致で受信していません。</div>";
  }
  let h = '<table class="kv">';
  for (const g of GROUPS) {
    const rows = g.keys.filter(k => st.fields.has(k));
    if (!rows.length) continue;
    h += `<tr class="hl"><td colspan="2">${esc(g.title)}</td></tr>`;
    for (const key of rows) h += fieldRow(key, st.fields.get(key), st.latestT);
  }
  return h + "</table>";
}

function confirmed(state) {
  const control = state.control || {};
  if (control.municipality_source === "facch"
    && typeof control.municipality_confirmed === "number") {
    return {
      code: control.municipality_confirmed,
      via: "FACCH 番号通知 0x63 による（受信内容から一意に確定）",
    };
  }
  if (control.municipality_source === "flag"
    && typeof control.municipal_code === "number" && control.municipal_code !== 0) {
    return {
      code: control.municipal_code,
      via: "起動時の --municipal-code 指定による（受信内容ではなく運用者の申告）",
    };
  }
  return null;
}

function confirmedName(state, code) {
  const control = state.control || {};
  if (control.municipality) return control.municipality;
  for (const c of control.candidates || []) {
    if (c[0] === code) return c[1];
  }
  return "";
}

function seedPanelHTML(state) {
  const hist = state.seedHistory || [];
  const pinned = state.pinnedSeed;
  const active = state.control ? state.control.seed : null;

  const hasPin = pinned !== null && pinned !== undefined;
  let h = '<div class="seedpin">';
  h += '<span class="title">スクランブル値の直近判定</span>';
  h += hasPin
    ? `<span class="badge ok">固定中 ${esc(pinned)}</span>`
    : '<span class="badge idle">自動判定</span>';
  h += `<button class="btn plain" data-seed-pin="${SEED_AUTO}"`
    + `${hasPin ? "" : " disabled"}>自動に戻す</button></div>`;

  if (!hist.length) {
    return h + '<div class="note">まだ自動判定の確定がありません。'
      + "確定するたびにここへ溜まり、同じ値は件数でまとめます。</div>";
  }

  h += '<table class="cand"><thead><tr><th>seed</th><th>回数</th><th>市区町村候補</th>'
    + "<th>最終判定</th><th></th></tr></thead><tbody>";
  for (const e of hist) {
    const names = (e.candidates || []).map(c => c[1]).join("、") || "—";
    const rowPinned = pinned === e.seed;
    h += `<tr class="${e.seed === active ? "confirmed" : ""}">`
      + `<td class="code">${esc(e.seed)}</td>`
      + `<td class="code">${esc(e.count)}</td>`
      + `<td class="name">${esc(names)}</td>`
      + `<td class="code">${esc(fmtClock(e.lastT, 0))}</td>`
      + `<td><button class="btn plain${rowPinned ? " on" : ""}" `
      + `data-seed-pin="${esc(e.seed)}">${rowPinned ? "固定中" : "固定"}</button></td>`
      + "</tr>";
  }
  return h + "</tbody></table>";
}

function candidatesHTML(state) {
  const control = state.control || {};
  const cands = control.candidates || [];
  const conf = confirmed(state);

  let h = "";
  if (conf) {
    const name = confirmedName(state, conf.code);
    h += `<div class="note"><span class="badge ok">確定</span> `
      + `${esc(name || "（名称不明）")}（${conf.code}）— ${esc(conf.via)}</div>`;
  }
  if (control.searching) {
    return h + '<div class="note">スクランブル値を自動探索中です。</div>';
  }
  if (!cands.length) {
    return h + '<div class="note">スクランブル値に一致する市区町村候補がありません。</div>';
  }

  h += '<table class="cand"><thead><tr><th>コード</th><th>名称</th><th>現行・廃止</th>'
    + "</tr></thead><tbody>";
  for (const c of cands) {
    const code = c[0], name = c[1];
    const abol = isAbolished(name);
    const hit = conf && conf.code === code;
    h += `<tr class="${hit ? "confirmed" : ""}">`
      + `<td class="code">${esc(code)}</td>`
      + `<td class="name">${esc(name)}</td>`
      + `<td><span class="tag ${abol ? "abol" : "cur"}">${abol ? "廃止" : "現行"}</span></td>`
      + "</tr>";
  }
  h += "</tbody></table>";

  h += '<div class="note">候補は電波に乗るスクランブル値（市区町村コードの下位 9bit）から'
    + "逆引きしたもので、一意には特定できない。親局は免許時のコードを使い続けるため"
    + "廃止コードで運用している局が実在するので、廃止された旧市町村も候補に含めている。</div>";
  h += '<div class="note">廃止コードの詳細（廃止年月日・改正事由）はサーバ側 citycodes が'
    + "保持しているが、候補一覧が読めなくなるため表示しない。</div>";
  return h;
}

function summaryHTML(state, st) {
  const c = state.control || {};
  const rate = c.total ? (100 * (c.crc_ok || 0) / c.total).toFixed(1) + "%" : "—";
  const seed = c.searching ? "探索中" : (c.seed !== null && c.seed !== undefined ? c.seed : "—");
  const pinNote = c.seed_pinned ? '<span class="votes">手動固定</span>' : "";
  const rows = [
    ["スクランブル値（下位9bit）", esc(seed) + pinNote],
    ["CRC16 一致 / 有効 / 総数",
      `${c.crc_ok || 0} / ${c.valid || 0} / ${c.total || 0}`
      + `<span class="votes">一致率 ${rate}</span>`],
    ["BCCH 系（CRC一致・集計対象）",
      st ? `${st.counted} 通<span class="votes">直近 ${st.scanned} 通を走査</span>` : "—"],
  ];
  return '<table class="kv">'
    + rows.map(([k, v]) => `<tr><th>${esc(k)}</th><td>${v}</td></tr>`).join("")
    + "</table>";
}


export function init(store) {
  const body = document.getElementById("control-body");
  const sub = document.getElementById("control-sub");
  if (!body) return;

  body.addEventListener("click", ev => {
    const btn = ev.target.closest("[data-seed-pin]");
    if (!btn || btn.disabled) return;
    store.pinSeed(Number(btn.dataset.seedPin));
  });

  function render() {
    const state = store.state;
    body.innerHTML = summaryHTML(state, stats)
      + seedPanelHTML(state)
      + candidatesHTML(state)
      + fieldsHTML(stats);
    if (sub) {
      const c = state.control || {};
      const rate = c.total ? (100 * (c.crc_ok || 0) / c.total).toFixed(1) + "%" : "—";
      sub.textContent = `BCCH ${stats ? stats.counted : 0} 通 / CRC ${rate}`;
    }
  }

  let timer = null;
  let lastRun = 0;
  function recompute() {
    lastRun = Date.now();
    stats = collect(store.state.messages || []);
    render();
  }
  function schedule(force) {
    if (force) {
      if (timer) { clearTimeout(timer); timer = null; }
      recompute();
      return;
    }
    if (timer) return;
    const wait = Math.max(0, RECOMPUTE_MS - (Date.now() - lastRun));
    timer = setTimeout(() => { timer = null; recompute(); }, wait);
  }

  store.on("messages", () => schedule(false));
  store.on("control", render);
  store.on("seed", render);
  store.on("snapshot", () => {
    updateNo = { value: null, t: null, changed: false };
    schedule(true);
  });

  render();
}
