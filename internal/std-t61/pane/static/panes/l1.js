import { TOPIC } from '../store.js';
import { makeLayerPane, timeCol, freqCol } from './layers.js';
import { el } from '../dom.js';

export function init(store) {
  const bar = document.getElementById('l1-toolbar');
  const all = el('label', { class: 'btn' });
  const allBox = el('input', { type: 'checkbox' });
  all.append(allBox, el('span', { text: ' 選択局の全フレームを出す' }));
  const follow = el('button', { class: 'btn sel', text: '最新へ追従' });
  const note = el('span', { class: 'tiny muted' });
  bar.append(all, follow, note);

  const pane = makeLayerPane(store, {
    pane: 'l1',
    topic: TOPIC.l1,
    empty: 'この局の L1 フレームはまだありません',
    items: (s) => s.l1.filter((r) => store.inScope(r.idx)),
    key: (r) => `l1/${r.no}`,
    columns: [
      { name: '番号', class: 'mono tiny', get: (r) => `#${r.no}` },
      timeCol,
      freqCol(store),
      { name: '理由', class: 'badge idle', get: (r) => r.why },
      { name: '概要', class: 'grow tiny', get: (r) => r.info },
    ],
    detail: (r) => ({
      title: `#${r.no} ${r.kind}${r.uplink ? '(上り)' : ''} SF内位置 ${r.sf_pos >= 0 ? r.sf_pos : '不明'}　RICH ${r.rich_ok ? r.rich_bits : 'NG'}`,
      tree: r.tree, hex: r.raw_hex,
      links: r.l2_no ? [el('span', { class: 'pill info', text: `→ L2 #${r.l2_no} で完結` })] : [],
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
      ? '選択局の全フレーム（25 枚/秒）を出しています'
      : '変化のあった枚だけ（RICH の変化・ユニット完結・同期バースト）。1 行 = 無線フレーム 40 ms';
  }
  store.on(TOPIC.meta, syncToggle);
  syncToggle(store.state);
}
