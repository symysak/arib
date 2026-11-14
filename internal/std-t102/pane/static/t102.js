import { configure } from './nxdn.js';

const hex3 = (v) => `0x${v.toString(16).toUpperCase().padStart(3, '0')}`;
const dirText = (d) => (d < 0 ? '' : d ? '下り' : '上り/直接');

function wcCell(st) {
  if (!st) return { text: '' };
  return st.init >= 0 ? { text: hex3(st.init), title: `ホワイトニングの初期値（${st.init_why || ''}）` } : { text: '未定', cls: 'muted', title: 'CRC と AMBE+2 の検査で当てている' };
}

function keyCell(st) {
  const k = ((st && st.profile && st.profile.keys) || []).find((x) => x.name.startsWith('音声秘話'));
  if (!k) return { text: '' };
  if (k.name === '音声秘話') return { text: '平文', cls: 'muted tiny', title: '音声秘話なし（平文と決まった）' };
  return { text: k.value, cls: 'mono', title: `${k.name}: ${k.value}` };
}

configure({
  hideQuiet: false,
  laneCols: [
    { name: 'WC', cls: 'mono', get: (st) => wcCell(st) },
    { name: '秘話鍵', get: (st) => keyCell(st) },
    { name: 'U', cls: 'mono', get: (st) => (st.u >= 0 ? String(st.u) : '') },
    { name: '製造者', cls: 'mono', get: (st) => (st.maker >= 0 ? String(st.maker) : '') },
    { name: '発信者名', get: (st) => st.caller || '' },
  ],
  txCols: [
    { name: 'WC', cls: 'mono', get: (tx, st) => wcCell(st) },
    { name: '秘話鍵', get: (tx, st) => (tx.voice ? keyCell(st) : { text: '' }) },
    { name: 'U', cls: 'mono', get: (tx) => (tx.u >= 0 ? String(tx.u) : '') },
    { name: '製造者', cls: 'mono', get: (tx) => (tx.maker >= 0 ? String(tx.maker) : '') },
    { name: '方向', cls: 'tiny', get: (tx) => dirText(tx.d) },
    { name: '呼（B54）', cls: 'tiny', get: (tx) => tx.call || '' },
    { name: '発信者名', get: (tx) => tx.name || '' },
  ],
  txSearch: (tx) => `u=${tx.u} u${tx.u} 製造者${tx.maker} ${dirText(tx.d)} ${tx.call || ''} ${tx.name || ''}`,
  focusHead: (tx, st) => ({
    big: tx.name ? `「${tx.name}」` : (tx.call || (tx.u >= 0 ? `U=${tx.u}` : tx.kind)),
    sub: `${st ? `${st.mhz.toFixed(5)} MHz` : ''}${dirText(tx.d) ? `・${dirText(tx.d)}` : ''}`,
  }),
  focusRows: (tx, st) => {
    const key = keyCell(st);
    const rows = [
      ['WC（ホワイトニングの初期値）', st && st.init >= 0 ? `${hex3(st.init)}（${st.init_why || ''}）` : '未定'],
      ['音声秘話の鍵', tx.voice && key.text ? (key.title || key.text) : '-'],
      ['ユーザコード U', tx.u >= 0 ? String(tx.u) : '-'],
      ['製造者番号', tx.maker >= 0 ? String(tx.maker) : '-'],
    ];
    if (tx.call) rows.push(['呼（ARIB STD-B54）', tx.call]);
    if (tx.name) rows.push(['発信者名（B54）', tx.name]);
    if (tx.data) rows.push(['データ', `${tx.data} 枚（UDCH1 / FACCH2 の CRC が通ったもの）`]);
    return rows;
  },
});
