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
    { name: '呼出名称', cls: 'mono', get: (st) => st.caller || '' },
  ],
  txCols: [
    { name: 'チャネル', get: (tx) => { const e = tx.ext || {}; return e.ch ? `${e.class} ${e.ch}` : ''; } },
    { name: '呼出名称', cls: 'mono', get: (tx) => (tx.name ? tx.name
      : { text: '読めず', cls: 'muted tiny', title: '同期バーストの PICH を聞き逃したか、CRC が通らなかった' }) },
    { name: 'WC', cls: 'mono', get: (tx, st) => wcCell(st) },
    { name: 'UC', cls: 'mono', get: (tx) => (tx.u >= 0 ? String(tx.u) : '') },
    { name: '通話識別', get: (tx, st) => callCell((tx.ext || {}).cipher, ...txKey(tx, st)) },
  ],
  txSearch: (tx, st) => {
    const e = tx.ext || {};
    return `${e.class || ''} ${e.ch || ''}ch ${tx.name || ''} uc${tx.u} ${tx.u} ${e.call || ''} 製造者${tx.maker} 鍵${e.cipher ? txKey(tx, st)[0] : ''}`;
  },
  focusHead: (tx, st) => {
    const e = tx.ext || {};
    const ch = e.ch ? `${e.class} ${e.ch} ch` : '割当の外';
    return {
      big: tx.name || '呼出名称は読めず',
      sub: `${tx.name ? `${e.csm_class || ''}・` : ''}${ch}${st ? `（${st.mhz.toFixed(5)} MHz）` : ''}`,
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
      ['製造者', tx.maker > 0 ? String(tx.maker) : tx.maker === 0 ? '共通（0）' : '-'],
    ];
    if (g && g.radios && g.radios.length) rows.push(['同じグループの無線機', g.radios.join(' ')]);
    return rows;
  },
});
