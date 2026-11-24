import { configure } from './nxdn.js';

export function groupOf(st, uc) {
  return ((st && st.ext && st.ext.groups) || []).find((g) => g.uc === uc);
}

export function callCell(cipher, key, how) {
  if (cipher === undefined || cipher === null) return { text: '' };
  if (!cipher) return { text: '通常', cls: 'tag plain', title: '通常通信' };
  return key > 0 ? { text: `秘話（${key}）`, cls: 'tag cipher', title: `秘話通信（秘話スクランブル。§4.1.11）・秘話鍵 ${key}（${how}）` }
    : { text: '秘話（不明）', cls: 'tag cipher', title: '秘話通信・秘話鍵は分からない（-key で渡すか、長い通話で当たる）' };
}

export function cipherText(cipher, key, how) {
  if (cipher === undefined || cipher === null) return '-';
  if (!cipher) return '通常通信';
  return key > 0 ? `秘話通信・秘話鍵 ${key}（${how}）` : '秘話通信・秘話鍵は分からない';
}

export function wcCell(st) {
  if (!st || !(st.init >= 0)) return { text: '' };
  return { text: `0x${st.init.toString(16).toUpperCase().padStart(3, '0')}`, title: `ホワイトニングの初期値（${st.init_why || ''}）` };
}

export function licenseeCell(e) {
  if (!e || !e.licensee) return { text: '' };
  const place = e.location || e.area || '';
  const t = [`免許人 ${e.licensee}${e.corp_no ? `（法人番号 ${e.corp_no}）` : ''}`];
  if (e.location) t.push(`設置場所 ${e.location}`);
  if (e.area) t.push(`移動範囲 ${e.area}`);
  if (e.purpose) t.push(`目的 ${e.purpose}`);
  if (e.license_date) t.push(`免許の年月日 ${e.license_date}${e.valid_until ? `（${e.valid_until} まで）` : ''}`);
  if (e.bands) t.push(`周波数 ${e.bands.replaceAll(',', ' / ')} MHz`);
  if (e.bureau) t.push(`所轄 ${e.bureau}`);
  return { text: place ? `${e.licensee}（${place}）` : e.licensee, title: t.join('\n') };
}

function csmCell(csm, who) {
  if (!csm) return '';
  return who ? { text: `${csm} ${who}`, title: `呼出名称 ${csm}・免許人 ${who}` } : csm;
}

export function chText(ch) { return ch ? `${ch.class} ${ch.no}` : ''; }

function txKey(tx, st) {
  const g = groupOf(st, tx.u);
  return g ? [g.key, g.key_how] : [0, ''];
}

configure({
  hideQuiet: true,
  laneCols: [
    { name: 'チャネル', get: (st) => {
      const ch = st.ext && st.ext.ch;
      return { text: ch ? chText(ch) : '割当の外', title: ch && ch.note ? ch.note : '' };
    } },
    { name: 'WC', cls: 'mono', get: (st) => wcCell(st) },
    { name: 'UC', cls: 'mono', get: (st) => (st.u >= 0 ? String(st.u) : '') },
    { name: '通話識別', get: (st) => {
      const g = st.u >= 0 ? groupOf(st, st.u) : null;
      return g ? callCell(g.cipher, g.key, g.key_how) : { text: '' };
    } },
    { name: '呼出名称', cls: 'mono', get: (st) => csmCell(st.caller, st.ext && st.ext.caller_licensee) },
  ],
  txCols: [
    { name: 'チャネル', get: (tx) => { const e = tx.ext || {}; return e.ch ? `${e.class} ${e.ch}` : ''; } },
    { name: '呼出名称', cls: 'mono', get: (tx) => (tx.name ? csmCell(tx.name, (tx.ext || {}).licensee)
      : { text: '読めず', cls: 'muted tiny', title: '同期バーストの PICH を聞き逃したか、CRC が通らなかった' }) },
    { name: 'WC', cls: 'mono', get: (tx, st) => wcCell(st) },
    { name: 'UC', cls: 'mono', get: (tx) => (tx.u >= 0 ? String(tx.u) : '') },
    { name: '通話識別', get: (tx, st) => callCell((tx.ext || {}).cipher, ...txKey(tx, st)) },
    { name: '呼', get: (tx) => {
      const n = (tx.ext || {}).self_name;
      const t = [tx.call, n].filter(Boolean).join(' ');
      return t ? { text: t, title: `製造者定義から読んだ呼${tx.call ? `（${tx.call}）` : ''}${n ? `・自局名 ${n}` : ''}` } : '';
    } },
  ],
  txSearch: (tx, st) => {
    const e = tx.ext || {};
    return `${e.class || ''} ${e.ch || ''}ch ${tx.name || ''} ${e.licensee || ''} ${e.place || ''} uc${tx.u} ${tx.u} ${e.call || ''} ${tx.call || ''} ${e.self_name || ''} 製造者${tx.maker} 鍵${e.cipher ? txKey(tx, st)[0] : ''}`;
  },
  focusHead: (tx, st) => {
    const e = tx.ext || {};
    const ch = e.ch ? `${e.class} ${e.ch} ch` : '割当の外';
    return {
      big: tx.name || '呼出名称は読めず',
      sub: `${tx.name ? `${e.csm_class || ''}・` : ''}${e.licensee ? `${e.licensee}${e.place ? `（${e.place}）` : ''}・` : ''}${ch}${st ? `（${st.mhz.toFixed(5)} MHz）` : ''}`,
    };
  },
  focusRows: (tx, st) => {
    const e = tx.ext || {};
    const g = groupOf(st, tx.u);
    const [key, how] = txKey(tx, st);
    const rows = [
      ['WC（ホワイトニングの初期値）', wcCell(st).text ? `${wcCell(st).text}（${st.init_why || ''}）` : '-'],
      ['ユーザコード', tx.u >= 0 ? String(tx.u) : '-'],
      ['通話識別', e.call || '-'],
      ['秘話鍵', e.cipher ? (key > 0 ? `${key}（${how}）` : '分からない') : '-'],
      ['製造者', e.maker_name || (tx.maker > 0 ? String(tx.maker) : tx.maker === 0 ? '共通（0）' : '-')],
    ];
    if (tx.call) rows.push(['呼（製造者定義）', tx.call]);
    if (e.self_name) rows.push(['自局名（製造者定義）', e.self_name]);
    if (g && g.radios && g.radios.length) rows.push(['同じグループの無線機', g.radios.join(' ')]);
    return rows;
  },
});
