import { TOPIC } from '../store.js';
import { makeLayerPane, timeCol, freqCol } from './layers.js';
import { el } from '../dom.js';

export function init(store) {
  const bar = document.getElementById('l2-toolbar');
  const follow = el('button', { class: 'btn sel', text: '最新へ追従' });
  bar.append(follow, el('span', { class: 'tiny muted',
    text: '1 行 = LAPDM フレーム（CAC / SACCH / FACCH のユニットを W で組み上げたもの）。DLCI = SAPI + SMSI / MSI（MSI7F = 全局宛て）' }));
  const pane = makeLayerPane(store, {
    pane: 'l2',
    topic: TOPIC.l2,
    empty: 'この局の L2 フレームはまだありません',
    items: (s) => s.l2.filter((r) => store.inScope(r.idx)),
    key: (r) => `l2/${r.no}`,
    columns: [
      { name: '番号', class: 'mono tiny', get: (r) => `#${r.no}` },
      timeCol,
      freqCol(store),
      { name: 'ST', class: 'mono tiny', get: (r) => `ST#${r.slot}` },
      { name: 'チャネル', class: 'badge info', get: (r) => r.channel },
      { name: 'DLCI', class: 'mono tiny', get: (r) => r.dlci },
      { name: '団体（MSI）', class: 'tiny', get: (r) => r.org || '' },
      { name: '制御', class: 'pill', get: (r) => r.control },
      { name: '件数', class: 'pill info', get: (r) => (r.repeat > 1 ? `×${r.repeat}` : '') },
      { name: '概要', class: 'grow tiny', get: (r) => r.summary },
    ],
    detail: (r) => ({
      title: `#${r.no} ST#${r.slot} ${r.channel} ${r.state}${r.err ? '　形式異常: ' + r.err : ''}${r.repeat > 1 ? `　×${r.repeat}` : ''}`,
      tree: r.tree, hex: r.raw_hex,
      links: (r.l3_nos || []).map((n) => el('span', { class: 'pill ok', text: `→ L3 #${n}` })),
    }),
  });
  follow.addEventListener('click', () => pane.follow());
}
