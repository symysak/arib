import { makeLayerPane, timeCol, freqCol } from './layers.js';

export function init(store) {
  const pane = makeLayerPane(store, {
    pane: 'l1',
    topic: 'nxdnframes',
    empty: 'この局のフレームはまだありません',
    items: (s) => s.nxdnframes.filter((r) => store.inScope(r.idx) && store.inFocus(r)),
    key: (r) => `l1/${r.no}`,
    columns: [
      timeCol,
      freqCol(store),
      { name: '種別', class: 'badge idle', get: (r) => r.kind },
      { name: '理由', class: 'tiny muted', get: (r) => r.why },
      { name: '件数', class: 'pill info', get: (r) => (r.repeat > 1 ? `×${r.repeat}` : '') },
      { name: '概要', class: 'grow tiny', get: (r) => r.info },
    ],
    detail: (r) => ({
      title: `無線フレーム #${r.frame} ${r.kind}${r.repeat > 1 ? `（同じ中身 ×${r.repeat}）` : ''}`,
      tree: r.tree, hex: r.raw_hex,
    }),
  });
  const sub = document.getElementById('l1-sub');
  const note = () => { if (sub && store.focusedTx()) sub.textContent += '・選んだ送信だけ（Esc で全部へ）'; };
  store.on('nxdnfocus', () => { pane.follow(); note(); });
  store.on('nxdnframes', note);
  store.on('selection', note);
  note();
}
