import { TOPIC } from '../store.js';
import { makeLayerPane, timeCol, freqCol } from './layers.js';
import { el } from '../dom.js';

export function init(store) {
  const bar = document.getElementById('l1-toolbar');
  const all = el('label', { class: 'btn' });
  const allBox = el('input', { type: 'checkbox' });
  all.append(allBox, el('span', { text: ' 選択局の全スロットを出す' }));
  const follow = el('button', { class: 'btn sel', text: '最新へ追従' });
  const note = el('span', { class: 'tiny muted' });
  bar.append(all, follow, note);

  const pane = makeLayerPane(store, {
    pane: 'l1',
    topic: TOPIC.l1,
    empty: 'この局のスロットはまだありません',
    items: (s) => s.l1.filter((r) => store.inScope(r.idx)),
    key: (r) => `l1/${r.no}`,
    columns: [
      { name: '番号', class: 'mono tiny', get: (r) => `#${r.no}` },
      timeCol,
      freqCol(store),
      { name: 'ST', class: 'mono tiny', get: (r) => `ST#${r.slot}` },
      { name: '理由', class: 'badge idle', get: (r) => r.why },
      { name: '件数', class: 'pill info', get: (r) => (r.repeat > 1 ? `×${r.repeat}` : '') },
      { name: '概要', class: 'grow tiny', get: (r) => r.info },
    ],
    detail: (r) => ({
      title: `#${r.no} ST#${r.slot} 無線フレーム #${r.frame} ${r.sw} ${r.kind}${r.repeat > 1 ? `（同じ中身 ×${r.repeat}）` : ''}`,
      tree: r.tree, hex: r.raw_hex,
    }),
  });

  allBox.addEventListener('change', (e) => {
    fetch(`api/l1all?on=${e.target.checked ? 1 : 0}`, { method: 'POST' }).catch(() => {});
  });
  follow.addEventListener('click', () => pane.follow());

  function syncToggle(s) {
    const on = !!(s.meta && s.meta.l1_all);
    if (allBox.checked !== on) allBox.checked = on;
    all.classList.toggle('sel', on);
    note.textContent = on
      ? '選択局の全スロット（100 枚/秒）を出しています'
      : '中身が変わったスロットとユニットが復号できたスロットだけ（同じ中身は ×N）。1 行 = スロット 10 ms';
  }
  store.on(TOPIC.meta, syncToggle);
  syncToggle(store.state);
}
