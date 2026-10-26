import { TOPIC } from '../store.js';
import { hms } from '../time.js';
import { el, setText, syncList } from '../dom.js';

const GROUPS = [['station', '局状態'], ['call', '通話'], ['record', '録音'], ['system', 'システム']];

export function init(store) {
  const bar = document.getElementById('log-toolbar');
  const body = document.getElementById('log-body');
  const sub = document.getElementById('log-sub');
  for (const [k, n] of GROUPS) bar.appendChild(el('button', { class: 'btn sel', 'data-g': k, text: n }));
  const list = el('div', { class: 'scroll tall' });
  const empty = el('div', { class: 'muted' });
  body.append(list, empty);

  bar.addEventListener('click', (e) => {
    const b = e.target.closest('[data-g]');
    if (!b) return;
    const on = !b.classList.contains('sel');
    b.classList.toggle('sel', on);
    store.setLogGroup(b.dataset.g, on);
  });

  function makeRow() {
    const row = el('div', { class: 'row' });
    const t = el('span', { class: 'muted tiny' });
    const g = el('span', { class: 'badge idle' });
    const c = el('span', { class: 'pill info' });
    const x = el('span', { class: 'grow tiny' });
    row.append(t, g, c, x);
    row._parts = { t, g, c, x };
    return row;
  }

  function draw() {
    const items = store.collapsedLogs();
    setText(sub, `${items.length} 行`);
    syncList(list, items.slice(-300).reverse(), (i) => i.key, makeRow, (node, i) => {
      const p = node._parts;
      setText(p.t, hms(i.lastT));
      setText(p.g, i.l.group);
      setText(p.c, i.count > 1 ? `×${i.count}` : '');
      p.c.style.display = i.count > 1 ? '' : 'none';
      setText(p.x, i.l.text);
    });
    setText(empty, items.length ? '' : 'この絞り込みに合うログはありません');
    empty.style.display = items.length ? 'none' : '';
  }

  store.on(TOPIC.logs, draw);
  store.on(TOPIC.selection, draw);
  draw();
}
