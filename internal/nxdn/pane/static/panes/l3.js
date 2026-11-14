import { makeLayerPane, timeCol, freqCol } from './layers.js';

export function init(store) {
  const pane = makeLayerPane(store, {
    pane: 'l3',
    topic: 'nxdnmsgs',
    empty: 'この局のメッセージはまだありません',
    items: (s) => s.nxdnmsgs.filter((r) => store.inScope(r.idx) && store.inFocus(r)),
    key: (r) => `l3/${r.no}`,
    columns: [
      timeCol,
      freqCol(store),
      { name: 'チャネル', class: 'badge idle', get: (r) => r.msg.ch },
      { name: 'U', class: 'mono tiny', get: (r) => (r.u >= 0 ? `U=${r.u}` : '') },
      { name: '件数', class: 'pill info', get: (r) => (r.repeat > 1 ? `×${r.repeat}` : '') },
      { name: '種別', class: 'grow tiny', get: (r) => `${r.msg.name}${r.msg.maker >= 0 ? `・製造者 ${r.msg.maker}` : ''}` },
    ],
    detail: (r) => ({
      title: `${r.msg.ch} ${r.msg.name}${r.repeat > 1 ? `（×${r.repeat}）` : ''}`,
      tree: r.tree,
      hex: (r.msg.raw ? atob(r.msg.raw) : '').split('').map((c) => c.charCodeAt(0).toString(16).padStart(2, '0')).join(''),
    }),
  });
  const sub = document.getElementById('l3-sub');
  const note = () => { if (sub && store.focusedTx()) sub.textContent += '・選んだ送信だけ（Esc で全部へ）'; };
  store.on('nxdnfocus', () => { pane.follow(); note(); });
  store.on('nxdnmsgs', note);
  store.on('selection', note);
  note();
}
