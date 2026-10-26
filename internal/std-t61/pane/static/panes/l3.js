import { TOPIC } from '../store.js';
import { makeLayerPane, timeCol, freqCol } from './layers.js';
import { el } from '../dom.js';

export function init(store) {
  const bar = document.getElementById('l3-toolbar');
  bar.append(el('span', { class: 'tiny muted', text: '1 行 = RT / MM / CC メッセージ。T61 の表で解釈できたものだけ並ぶ' }));

  makeLayerPane(store, {
    pane: 'l3',
    topic: TOPIC.l3,
    empty: store.text('t61.l3.empty', 'T61 の表で解釈できたレイヤ 3 はまだありません。'
      + 'レイヤ 3 を T61 の表とは別に規定している系統では、ここは空のままです。'
      + '生オクテットは L2 側で見られます。'),
    items: (s) => s.l3.filter((r) => store.inScope(r.idx)),
    key: (r) => `l3/${r.no}`,
    columns: [
      { name: '番号', class: 'mono tiny', get: (r) => `#${r.no}` },
      timeCol,
      freqCol(store),
      { name: '実体', class: 'badge info', get: (r) => r.entity },
      { name: '種別', class: 'grow tiny', get: (r) => r.name },
    ],
    detail: (r) => ({
      title: `#${r.no} ${r.entity} ${r.name}${r.err ? '　' + r.err : ''}`,
      tree: r.tree, hex: r.raw_hex,
      links: [el('span', { class: 'pill info', text: `← L2 #${r.l2_no}` })],
    }),
  });
}
