import { TOPIC } from '../store.js';
import { makeLayerPane, timeCol, freqCol } from './layers.js';
import { el } from '../dom.js';

export function init(store) {
  const bar = document.getElementById('l3-toolbar');
  const follow = el('button', { class: 'btn sel', text: '最新へ追従' });
  const ents = { RT: true, MM: true, CC: true };
  const boxes = Object.keys(ents).map((k) => {
    const lb = el('label', { class: 'btn sel' });
    const cb = el('input', { type: 'checkbox' });
    cb.checked = true;
    cb.addEventListener('change', () => { ents[k] = cb.checked; lb.classList.toggle('sel', cb.checked); pane.draw(); });
    lb.append(cb, el('span', { text: ` ${k}` }));
    return lb;
  });
  bar.append(follow, ...boxes, el('span', { class: 'tiny muted', text: '1 行 = RT / MM / CC メッセージ 1 通（同じものは ×N）' }));
  const pane = makeLayerPane(store, {
    pane: 'l3',
    topic: TOPIC.l3,
    empty: 'この局の L3 メッセージはまだありません',
    items: (s) => s.l3.filter((r) => store.inScope(r.idx) && ents[r.entity] !== false),
    key: (r) => `l3/${r.no}`,
    columns: [
      { name: '番号', class: 'mono tiny', get: (r) => `#${r.no}` },
      timeCol,
      freqCol(store),
      { name: 'ST', class: 'mono tiny', get: (r) => `ST#${r.slot} ${r.channel}` },
      { name: '実体', class: 'badge info', get: (r) => r.entity },
      { name: '種別', class: 'tiny', get: (r) => r.name },
      { name: '件数', class: 'pill info', get: (r) => (r.repeat > 1 ? `×${r.repeat}` : '') },
      { name: '要点', class: 'grow tiny', get: (r) => r.summary + (r.err ? `　（${r.err}）` : '') },
    ],
    detail: (r) => ({
      title: `#${r.no} ${r.entity} ${r.name}${r.err ? '　' + r.err : ''}${r.repeat > 1 ? `　×${r.repeat}` : ''}`,
      tree: r.tree, hex: r.raw_hex,
      links: [el('span', { class: 'pill info', text: `← L2 #${r.l2_no}` })],
    }),
  });
  follow.addEventListener('click', () => pane.follow());
}
