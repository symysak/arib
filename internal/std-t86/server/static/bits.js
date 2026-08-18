
"use strict";


export function hexToBits(hex) {
  const s = String(hex || "").trim().replace(/^0[xX]/, "").replace(/[\s_:-]/g, "");
  const bits = new Uint8Array(s.length * 4);
  let n = 0;
  for (const ch of s) {
    const v = parseInt(ch, 16);
    if (Number.isNaN(v)) continue;
    bits[n++] = (v >> 3) & 1;
    bits[n++] = (v >> 2) & 1;
    bits[n++] = (v >> 1) & 1;
    bits[n++] = v & 1;
  }
  return bits.subarray(0, n);
}

export function bitsToInt(bits, start, end) {
  let v = 0;
  for (let i = start; i < end; i++) v = v * 2 + ((bits[i] || 0) & 1);
  return v;
}

export function bitsToHex(bits, start, end) {
  const len = Math.max(0, end - start);
  if (len === 0) return "";
  const width = Math.ceil(len / 4);
  let out = "";
  let acc = 0;
  let n = width * 4 - len;
  for (let i = start; i < end; i++) {
    acc = (acc << 1) | ((bits[i] || 0) & 1);
    if (++n === 4) {
      out += "0123456789abcdef"[acc];
      acc = 0;
      n = 0;
    }
  }
  return out;
}

const MAX_INT_BITS = 53;


const MESSAGE_TYPES = {
  0x10: { name: "報知情報", channel: "BCCH", section: "§4.3.4.1" },
  0x20: { name: "アイドル信号(PCH)", channel: "PCH", section: "§4.3.5.1" },
  0x21: { name: "BCCH変更通知", channel: "PCH", section: "§4.3.5.2" },
  0x22: { name: "通報開始指示", channel: "PCH", section: "§4.3.5.3" },
  0x23: { name: "時差通報開始指示", channel: "PCH", section: "§4.3.5.4" },
  0x30: { name: "強制切断指示", channel: "PCH", section: "§4.3.5.5" },
  0x40: { name: "アイドル信号(SCCH)", channel: "SCCH", section: "§4.3.6.1" },
  0x63: { name: "番号通知", channel: "FACCH", section: "§4.3.7.1" },
  0x78: { name: "無線制御要求", channel: "SB", section: "§4.3.8.1" },
};

export function typeInfo(type) {
  const m = MESSAGE_TYPES[type];
  return m ? { name: m.name, section: m.section, channel: m.channel } : null;
}

const MANUFACTURERS = {
  2: "沖電気工業", 3: "東芝", 5: "日本電気(NEC)", 6: "日本無線(JRC)",
  7: "日立国際電気", 8: "富士通", 10: "松下電器産業(パナソニック)", 11: "三菱電機",
  13: "富士通ゼネラル", 171: "日立国際電気",
};

const PARENT_MODE = { 0b01: "間欠送信モード", 0b10: "通話時送信モード", 0b00: "予約", 0b11: "予約" };

const MEDIA_TYPE = {
  0: "予約/音声なし", 1: "音声", 2: "FAX", 3: "文字", 4: "画像", 5: "テレメータ",
  6: "その他", 7: "予備",
};

const TRANS_PROTOCOL = {
  0b00: "デジタル非音声", 0b01: "推奨規格拡声音声/連絡用音声1",
  0b10: "予約/連絡用音声2", 0b11: "予約",
};

const RELEASE_REASONS = {
  0b0000: "正常切断/正常解放", 0b0001: "親局からの強制切断", 0b0010: "ビジー",
  0b0011: "相手無応答", 0b0100: "通信時限満了", 0b0110: "チャネル使用不可",
  0b0111: "サービス利用不可", 0b1000: "通信不可", 0b1001: "緊急通話不可",
  0b1010: "無効メッセージ", 0b1011: "同期はずれ", 0b1100: "通信時限以外のタイマ満了",
  0b1110: "番号通知送受失敗/市区町村コード不一致", 0b1111: "その他異常時",
};

const VOLUME_SETTING = { 0: "通常", 1: "最小", 2: "最大", 3: "予約" };

const EMERGENCY_CALL_LIMIT = {
  0b000: "緊急通話不可", 0b001: "30秒", 0b010: "60秒", 0b011: "90秒",
  0b100: "120秒", 0b101: "150秒", 0b110: "180秒", 0b111: "無制限",
};

const VALID_NUMBER_ID = { 0b00: "番号指定なし", 0b01: "予約", 0b10: "予約", 0b11: "予約" };

const RELATED_SLOT_ID = {
  0b00: "識別なし", 0b01: "現スロット", 0b10: "全スロット", 0b11: "関連スロット指定",
};

const FOLLOWING_BURST = {
  0b000: "制御用(CCH)", 0b001: "制御用(FACCH)", 0b010: "制御用(TCH(B))",
  0b011: "同期バースト(残カウンタ制御無し)",
  0b100: "通信用(TCH(I))", 0b101: "通信用(FACCH)", 0b110: "通信用(TCH(B))",
  0b111: "同期バースト(残カウンタ制御有り)",
};

function lookup(map, key, dflt) {
  const v = map[key];
  return v === undefined ? dflt : v;
}

function tbl(map, dflt) {
  return v => lookup(map, v, typeof dflt === "function" ? dflt(v) : dflt);
}

function flag(on, off) {
  return v => (v ? on : off);
}

function slotsFromMask(mask) {
  const out = [];
  for (let i = 0; i < 6; i++) if ((mask >> (5 - i)) & 1) out.push(i);
  return out;
}

function slotMaskDisplay(prefix, none) {
  return v => {
    const s = slotsFromMask(v);
    return s.length ? `${prefix} ${s.map(i => "#" + i).join(",")}` : none;
  };
}

const manufacturerDisplay = v => lookup(MANUFACTURERS, v, `不明(${v})`);


const W_ROW = {
  pos: "W(8)", start: 0, end: 8, name: "W(8) 予備", kind: "spare",
  note: "104bit ブロック先頭の W フィールド（オクテット1 は b[8:16]）",
};

const CCH_HEADER = [
  {
    pos: "oct1 b8", start: 8, end: 9, name: "ビジーフラグ BI", kind: "field",
    fmt: flag("ビジー（子局は送信不可）", "アイドル"),
  },
  {
    pos: "oct1 b7-1", start: 9, end: 16, name: "メッセージ種別", kind: "field",
    fmt: v => `0x${v.toString(16).padStart(2, "0")} ${typeInfo(v) ? typeInfo(v).name : "不明"}`,
  },
  { pos: "oct2 b8-6", start: 16, end: 19, name: "予約 3bit", kind: "reserved" },
  {
    pos: "oct2 b5-1", start: 19, end: 24, name: "スーパーフレーム内のフレーム番号", kind: "field",
    note: "00h-1Fh = フレーム#1-#32（生値表示）",
  },
  {
    pos: "oct3 b8", start: 24, end: 25, name: "状況フラグ2 連絡音声通信", kind: "field",
    fmt: flag("可", "不可"),
  },
  {
    pos: "oct3 b7", start: 25, end: 26, name: "状況フラグ2 データ伝送", kind: "field",
    fmt: flag("可", "不可"),
  },
  { pos: "oct3 b6", start: 26, end: 27, name: "状況フラグ2 予備", kind: "spare" },
  {
    pos: "oct3 b5-1", start: 27, end: 32, name: "スーパーフレームのフレーム数", kind: "field",
    note: "09h-1Fh = 10-32フレーム（生値表示）",
  },
  {
    pos: "oct4 b8", start: 32, end: 33, name: "状況フラグ1 緊急通信", kind: "field",
    fmt: flag("可", "不可"),
  },
  {
    pos: "oct4 b7", start: 33, end: 34, name: "状況フラグ1 緊急通信以外", kind: "field",
    fmt: flag("可", "不可"),
  },
  {
    pos: "oct4 b6-1", start: 34, end: 40, name: "スロット使用状況", kind: "field",
    fmt: slotMaskDisplay("使用中", "使用中なし"),
    note: "b6→#0 … b1→#5、1=使用中(USC)",
  },
  {
    pos: "oct5 b8", start: 40, end: 41, name: "拡声通報中フラグ KH", kind: "field",
    fmt: flag("拡声通報中", "拡声通報なし"),
  },
  {
    pos: "oct5 b7-5", start: 41, end: 44, name: "メディア種別", kind: "field",
    fmt: tbl(MEDIA_TYPE, "予約"),
  },
  {
    pos: "oct5 b4-3", start: 44, end: 46, name: "伝送制御部プロトコル", kind: "field",
    fmt: tbl(TRANS_PROTOCOL, "予約"),
  },
  { pos: "oct5 b2-1", start: 46, end: 48, name: "予約 2bit", kind: "reserved" },
];

const CCH_DERIVED = [
  {
    pos: "oct3 b8-7 / oct4 b8-7", start: 24, end: 34, derived: true,
    range: "b[24:26]+b[32:34]",
    name: "通信統制中（派生）", kind: "field",
    fmt: (_v, c) => {
      const restricted = !c.bits[24] && !c.bits[25] && !c.bits[32] && !c.bits[33];
      return restricted ? "通信統制中（親局が全ての通信を不可に設定）" : "統制なし";
    },
    note: "§4.3.3.1⑤a「親局は統制を行う場合、状況フラグ１および２ともに全てを不可とする」",
  },
];

const BCCH_BODY = [
  { pos: "oct6 b8-5", start: 48, end: 52, name: "予備 4bit", kind: "spare" },
  {
    pos: "oct6 b4-1", start: 52, end: 56, name: "報知情報更新番号", kind: "field",
    note: "0-15。変化＝親局が設定を書き換えた",
  },
  {
    pos: "oct7 b8-7", start: 56, end: 58, name: "親局送信モード", kind: "field",
    fmt: tbl(PARENT_MODE, "?"),
  },
  { pos: "oct7 b6", start: 58, end: 59, name: "予約 1bit", kind: "reserved" },
  {
    pos: "oct7 b5-1", start: 59, end: 64, name: "スーパーフレーム長 S", kind: "field",
    note: "09h-1Fh = 10-32フレーム（生値表示）",
  },
  {
    pos: "oct8 b8-5", start: 64, end: 68, name: "免許人固有情報（上位4bit）", kind: "field",
    fmt: (v, c) => {
      const all = (v << 4) | bitsToInt(c.bits, 72, 76);
      return `${v}（下位と合わせて 0x${all.toString(16).padStart(2, "0")}）`;
    },
    note: "製造者識別番号ごとに任意定義",
  },
  { pos: "oct8 b4-1", start: 68, end: 72, name: "PCH数 P", kind: "field" },
  {
    pos: "oct9 b8-5", start: 72, end: 76, name: "免許人固有情報（下位4bit）", kind: "field",
    fmt: (v, c) => {
      const all = (bitsToInt(c.bits, 64, 68) << 4) | v;
      return `${v}（上位と合わせて 0x${all.toString(16).padStart(2, "0")}）`;
    },
  },
  { pos: "oct9 b4-1", start: 76, end: 80, name: "PCH前のSCCH数 Sp", kind: "field" },
  {
    pos: "oct10 b8-5", start: 80, end: 84, name: "子局識別番号有効ビット数", kind: "field",
    fmt: v => (v <= 8 ? `${8 + v}bit（LSB詰め）` : `予約(${v})`),
    note: "生値 0000=8bit 〜 1000=16bit",
  },
  {
    pos: "oct10 b4", start: 84, end: 85, name: "上り折返識別", kind: "field",
    fmt: flag("折返あり", "折返なし"),
  },
  {
    pos: "oct10 b3-1", start: 85, end: 88, name: "緊急連絡通話通信時限", kind: "field",
    fmt: tbl(EMERGENCY_CALL_LIMIT, "?"),
  },
  {
    pos: "oct11-12", start: 88, end: 104, name: "製造者識別番号", kind: "field",
    fmt: manufacturerDisplay,
  },
];

const BROADCAST_BODY = [
  {
    pos: "oct6 b8-5", start: 48, end: 52,
    name: c => (c.type === 0x23 ? "分割番号" : "予備 4bit"),
    kind: c => (c.type === 0x23 ? "field" : "spare"),
    note: c => (c.type === 0x23 ? "時差通報開始指示のみ分割番号" : undefined),
  },
  {
    pos: "oct6 b4-1", start: 52, end: 56, name: "呼番号", kind: "field",
    fmt: v => (v === 0 ? "0h（子局発呼）" : `${v}h`),
    note: "1h-Fh。0x22→0x63→0x30 を束ねる鍵",
  },
  {
    pos: "oct7-8", start: 56, end: 72, name: "子局識別番号1", kind: "field",
    fmt: (v, c) => `${v}（0x${c.hex}）${c.bits[97] ? "" : " ※N1=0 のため無効"}`,
  },
  {
    pos: "oct9-10", start: 72, end: 88, name: "子局識別番号2", kind: "field",
    fmt: (v, c) => `${v}（0x${c.hex}）${c.bits[96] ? "" : " ※N2=0 のため無効"}`,
  },
  { pos: "oct11 b8-5", start: 88, end: 92, name: "予備 4bit", kind: "spare" },
  { pos: "oct11 b4", start: 92, end: 93, name: "予約 1bit", kind: "reserved" },
  {
    pos: "oct11 b3", start: 93, end: 94, name: "戸別受信機強制音量", kind: "field",
    fmt: flag("強制最大音量", "通常"),
  },
  {
    pos: "oct11 b2-1", start: 94, end: 96, name: "音量設定値", kind: "field",
    fmt: tbl(VOLUME_SETTING, "予約"),
  },
  {
    pos: "oct12 b8", start: 96, end: 97, name: "N2（子局識別番号2 有効）", kind: "field",
    fmt: flag("有効", "無効"),
  },
  {
    pos: "oct12 b7", start: 97, end: 98, name: "N1（子局識別番号1 有効）", kind: "field",
    fmt: flag("有効", "無効"),
  },
  {
    pos: "oct12 b6-1", start: 98, end: 104, name: "通報開始指示位置", kind: "field",
    fmt: v => (v === 0 ? "現PCHより開始または実施中" : `${v} PCH後より開始`),
  },
];

const BROADCAST_DERIVED = [
  {
    pos: "oct7-12", start: 56, end: 98, derived: true, range: "b[56:88]+b[96:98]",
    name: "報知対象（派生）", kind: "field",
    fmt: (_v, c) => {
      const t = targetVerdict(c.msg, c.ctx);
      return t ? t.label : "—";
    },
    note: c => {
      const t = targetVerdict(c.msg, c.ctx);
      return t ? t.reason : undefined;
    },
  },
];

const RELEASE_BODY = [
  { pos: "oct6 b8-5", start: 48, end: 52, name: "予備 4bit", kind: "spare" },
  {
    pos: "oct6 b4-1", start: 52, end: 56, name: "呼番号", kind: "field",
    fmt: v => (v === 0 ? "0h（全呼切断）" : `${v}h`),
  },
  { pos: "oct7 b8-5", start: 56, end: 60, name: "予備 4bit", kind: "spare" },
  {
    pos: "oct7 b4-1", start: 60, end: 64, name: "切断理由", kind: "field",
    fmt: tbl(RELEASE_REASONS, v => `予約(${v})`),
  },
  {
    pos: "oct8-12", start: 64, end: 104, name: "予備 40bit", kind: "spare",
    note: "全部 0 とは限らないので生値を出す",
  },
];

const NOTIFY_HEAD = [
  { pos: "oct1 b8", start: 8, end: 9, name: "予備 1bit", kind: "spare", note: "BI ではない" },
  {
    pos: "oct1 b7-1", start: 9, end: 16, name: "メッセージ種別", kind: "field",
    fmt: v => `0x${v.toString(16).padStart(2, "0")} ${typeInfo(v) ? typeInfo(v).name : "不明"}`,
  },
];

const NOTIFY_BODY = [
  { pos: "oct2-5", start: 16, end: 48, name: "予備 32bit", kind: "spare" },
  { pos: "oct6 b8-5", start: 48, end: 52, name: "予約 4bit", kind: "reserved" },
  {
    pos: "oct6 b4-1", start: 52, end: 56, name: "呼番号", kind: "field",
    fmt: v => (v === 0 ? "0h（子局発呼）" : `${v}h`),
  },
  {
    pos: "oct7-8", start: 56, end: 72, name: "子局識別番号", kind: "field",
    fmt: (v, c) => `${v}（0x${c.hex}）`,
  },
  {
    pos: "oct9-10", start: 72, end: 88, name: "市区町村コード", kind: "field",
    note: "完全な16bit。局を一意特定できる唯一の値（CAC の seed は下位9bit のみ）",
  },
  {
    pos: "oct11-12", start: 88, end: 104, name: "製造者識別番号", kind: "field",
    fmt: manufacturerDisplay,
  },
  {
    pos: "oct13-14", start: 104, end: 120, name: "免許人固有情報長", kind: "field",
    fmt: v => `${v} オクテット`,
    note: "一括通報時は 0-14",
  },
  {
    pos: "oct15-28", start: 120, end: 232, name: "免許人固有情報", kind: "field",
    note: "最大14オクテット=112bit。製造者ごとに任意定義",
  },
];

const SYNC_TRANS_CTRL = [
  { pos: "TC b8", start: 0, end: 1, name: "予備 1bit", kind: "spare" },
  {
    pos: "TC b7-5", start: 1, end: 4, name: "現スロット番号【必須】", kind: "field",
    fmt: v => (v <= 5 ? `#${v}` : "予約"),
    note: "000=#0 … 101=#5、110・111=予約",
  },
  {
    pos: "TC b4", start: 4, end: 5, name: "同期バースト残カウンタ制御有無【必須】", kind: "field",
    fmt: flag("制御有り（オプション）", "制御無し（必須）"),
  },
  {
    pos: "TC b3-1", start: 5, end: 8, name: "後続バースト識別【オプション】", kind: "field",
    fmt: tbl(FOLLOWING_BURST, "予約"),
  },
];

const RADIO_BODY = [
  { pos: "oct1 b8", start: 8, end: 9, name: "予備 1bit", kind: "spare", note: "BI ではない" },
  {
    pos: "oct1 b7-1", start: 9, end: 16, name: "メッセージ種別", kind: "field",
    fmt: v => `0x${v.toString(16).padStart(2, "0")} ${typeInfo(v) ? typeInfo(v).name : "不明"}`,
  },
  { pos: "oct2-4", start: 16, end: 40, name: "予備 24bit", kind: "spare" },
  { pos: "oct5 b8-3", start: 40, end: 46, name: "予備 6bit", kind: "spare" },
  {
    pos: "oct5 b2-1", start: 46, end: 48, name: "有効番号識別子【必須】", kind: "field",
    fmt: tbl(VALID_NUMBER_ID, "予約"),
  },
  { pos: "oct6-8", start: 48, end: 72, name: "予約・予備 24bit", kind: "reserved" },
  { pos: "oct9 b8", start: 72, end: 73, name: "予備 1bit", kind: "spare" },
  {
    pos: "oct9 b7-1", start: 73, end: 80, name: "同期バースト残カウンタ【オプション】", kind: "field",
    fmt: v => (v >= 121 ? `${v}（121以上）` : `${v}`),
    note: "0-120、1111001=121以上",
  },
  {
    pos: "oct10 b8-7", start: 80, end: 82, name: "関連スロット識別【必須】", kind: "field",
    fmt: tbl(RELATED_SLOT_ID, "?"),
  },
  {
    pos: "oct10 b6-1", start: 82, end: 88, name: "関連スロット【オプション】", kind: "field",
    fmt: slotMaskDisplay("対象", "指定なし"),
    note: "b6→#0 … b1→#5",
  },
  { pos: "oct11-12", start: 88, end: 104, name: "予備 16bit", kind: "spare" },
];

function unknownRows(totalBits) {
  return [
    W_ROW,
    {
      pos: "oct1 b8", start: 8, end: 9, name: "予備 1bit", kind: "spare",
      note: "既知種別でないので BI とは解釈しない",
    },
    {
      pos: "oct1 b7-1", start: 9, end: 16, name: "メッセージ種別", kind: "field",
      fmt: v => `0x${v.toString(16).padStart(2, "0")} 不明`,
    },
    {
      pos: `oct2-${totalBits / 8 - 1}`, start: 16, end: totalBits,
      name: "未定義（規格に定義の無い種別）", kind: "field", unnamed: true,
    },
  ];
}


function layoutFor(type, totalBits) {
  const info = typeInfo(type);
  const sec = info ? info.section : "";
  const name = info ? info.name : `不明(0x${Number(type).toString(16).padStart(2, "0")})`;

  switch (type) {
    case 0x10: case 0x20: case 0x21: case 0x40:
      return [
        { title: "W + 下り CCH 共通ヘッダ（オクテット1-5, §4.3.3.1）", defs: [W_ROW, ...CCH_HEADER] },
        { title: `${name}（オクテット6-12, §4.3.4.1）`, defs: BCCH_BODY },
        { title: "派生表示", defs: CCH_DERIVED },
      ];
    case 0x22: case 0x23:
      return [
        { title: "W + 下り CCH 共通ヘッダ（オクテット1-5, §4.3.3.1）", defs: [W_ROW, ...CCH_HEADER] },
        { title: `${name}（オクテット6-12, ${sec}）`, defs: BROADCAST_BODY },
        { title: "派生表示", defs: [...CCH_DERIVED, ...BROADCAST_DERIVED] },
      ];
    case 0x30:
      return [
        { title: "W + 下り CCH 共通ヘッダ（オクテット1-5, §4.3.3.1）", defs: [W_ROW, ...CCH_HEADER] },
        { title: `${name}（オクテット6-12, §4.3.5.5）`, defs: RELEASE_BODY },
        { title: "派生表示", defs: CCH_DERIVED },
      ];
    case 0x63:
      return [
        { title: "W + オクテット1（§4.3.7.1。BI は無い）", defs: [W_ROW, ...NOTIFY_HEAD] },
        { title: "番号通知（オクテット2-28, §4.3.7.1）", defs: NOTIFY_BODY },
      ];
    case 0x78:
      return [
        { title: "同期バーストの伝送制御情報部（§4.1.8.2）", defs: SYNC_TRANS_CTRL },
        { title: "無線制御要求 通信制御情報部（オクテット1-12, §4.3.8.1）", defs: RADIO_BODY },
      ];
    default:
      return [{ title: "未知のメッセージ種別", defs: unknownRows(totalBits) }];
  }
}

function callable(x, c) {
  return typeof x === "function" ? x(c) : x;
}

function buildRow(def, base) {
  const { start, end } = def;
  const len = end - start;
  const hex = bitsToHex(base.bits, start, end);
  const value = len > 0 && len <= MAX_INT_BITS ? bitsToInt(base.bits, start, end) : null;
  const c = { ...base, start, end, hex, value };
  const kind = callable(def.kind, c) || "field";
  const display = def.fmt
    ? def.fmt(value, c)
    : value === null ? `0x${hex}` : len <= 4 ? `${value}` : `${value}（0x${hex}）`;
  const row = {
    pos: def.pos,
    range: def.range || `b[${start}:${end}]`,
    start, end,
    name: callable(def.name, c),
    value,
    hex,
    display: String(display),
    kind,
  };
  const note = callable(def.note, c);
  if (note) row.note = note;
  if (def.derived) row.derived = true;
  if (def.unnamed) row.unnamed = true;
  return row;
}

function fitBits(bits, totalBits) {
  if (bits.length === totalBits) return bits;
  const out = new Uint8Array(totalBits);
  out.set(bits.subarray(0, Math.min(bits.length, totalBits)));
  return out;
}

export function decompose(msg, ctx = {}) {
  const m = msg || {};
  const type = Number(m.msg_type) & 0x7f;
  const raw = hexToBits(m.raw_hex || "");
  const isFACCH = type === 0x63 || m.channel === "FACCH" || raw.length > 104;
  const totalBits = isFACCH ? 232 : 104;
  const bits = fitBits(raw, totalBits);

  const info = typeInfo(type);
  const base = { bits, ctx: ctx || {}, msg: m, type };

  const groups = layoutFor(type, totalBits).map(g => ({
    title: g.title,
    rows: g.defs.filter(d => d.start < totalBits).map(d => buildRow(d, base)),
  }));

  const solid = [];
  for (const g of groups) for (const r of g.rows) if (!r.derived) solid.push(r);
  solid.sort((a, b) => a.start - b.start || a.end - b.end);

  const segments = [];
  let named = 0;
  let cur = 0;
  for (const r of solid) {
    if (r.start > cur) {
      segments.push({ start: cur, end: r.start, kind: "undefined", name: "（未割当）" });
    }
    const start = Math.max(cur, r.start);
    const end = Math.min(r.end, totalBits);
    if (end > start) {
      const kind = r.unnamed ? "undefined" : r.kind;
      segments.push({ start, end, kind, name: r.name });
      if (!r.unnamed) named += end - start;
      cur = end;
    }
  }
  if (cur < totalBits) {
    segments.push({ start: cur, end: totalBits, kind: "undefined", name: "（未割当）" });
  }

  return {
    type,
    name: info ? info.name : `不明(0x${type.toString(16).padStart(2, "0")})`,
    section: info ? info.section : "",
    totalBits,
    groups,
    segments,
    coverage: { named, total: totalBits },
  };
}

export function targetVerdict(msg, ctx = {}) {
  const m = msg || {};
  const type = Number(m.msg_type) & 0x7f;
  if (type !== 0x22 && type !== 0x23) return null;

  const bits = fitBits(hexToBits(m.raw_hex || ""), 104);
  const n1 = bits[97] === 1;
  const n2 = bits[96] === 1;
  const id1 = bitsToInt(bits, 56, 72);
  const id2 = bitsToInt(bits, 72, 88);

  const raw = ctx && Number(ctx.idValidBits);
  const learned = Number.isInteger(raw) && raw >= 8 && raw <= 16;
  const validBits = learned ? raw : 16;
  const mask = validBits >= 16 ? 0xffff : (1 << validBits) - 1;
  const maskHex = mask.toString(16).padStart(4, "0");

  const detail = [
    learned
      ? `有効ビット数: BCCH「子局識別番号有効ビット数」から学習した ${validBits}bit（LSB詰め, §4.3.4.1⑥）でマスク（0x${maskHex}）`
      : `有効ビット数: 未学習のため既定の 16bit でマスク（0x${maskHex}）`,
    `N1=${n1 ? 1 : 0} 子局識別番号1 = ${id1}（0x${bitsToHex(bits, 56, 72)}）→ マスク後 ${id1 & mask}`,
    `N2=${n2 ? 1 : 0} 子局識別番号2 = ${id2}（0x${bitsToHex(bits, 72, 88)}）→ マスク後 ${id2 & mask}`,
  ];

  const active = [];
  if (n1) active.push({ label: "子局識別番号1", eff: id1 & mask });
  if (n2) active.push({ label: "子局識別番号2", eff: id2 & mask });

  if (active.length === 0) {
    return {
      kind: "unknown",
      label: "不明（有効な子局識別番号なし）",
      reason: "N1・N2 がともに 0 で、有効な子局識別番号が 1 つも無い（§4.3.5.3②: N1/N2 が"
        + "立っている識別番号だけが有効）。値そのものは載っているが参照してはいけないので、"
        + "報知対象は判定しない。",
      detail,
    };
  }

  const effList = active.map(a => a.eff);
  const allZero = effList.every(v => v === 0);
  const allOnes = effList.every(v => v === mask);

  if (allZero) {
    return {
      kind: "all",
      label: "一斉（全子局）",
      reason: `有効ビット数 ${validBits} でマスクした結果が全 0（${active.map(a => a.label).join("・")}）`
        + " → §2.5 番号計画『16ビット全て0を一括通報、子局一括制御等の呼出先指定なしの場合に"
        + "使用する』に照らして一斉。",
      detail,
    };
  }

  const verdict = {
    kind: "selective",
    label: "子局/群 " + effList.join("・"),
    reason: `有効ビット数 ${validBits} でマスクした結果が 0 以外（${effList.join("・")}）`
      + " → §2.5 が全0 のみを一括通報と規定しているので、これは群呼出または個別呼出。"
      + "群/個別の番号割当はシステム設計依存（付属資料7）なので、番号の意味は推定しない。",
    detail,
  };
  if (allOnes) {
    verdict.detail = detail.concat(
      "§4.3.7 に『一括番号（システム内の全１）』の記載があり一斉の可能性があるが、"
      + "§2.5 が全0 のみを規定しており原文が曖昧なため断定しない"
    );
  }
  return verdict;
}
