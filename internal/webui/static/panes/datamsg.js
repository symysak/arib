import { hms } from '../time.js';
import { el, setText, syncList } from '../dom.js';

export function init(store) {
  const pane = document.getElementById('pane-datamsg');
  const body = document.getElementById('datamsg-body');
  const sub = document.getElementById('datamsg-sub');
  if (!pane || !body) return;
  pane.style.display = 'none';
  const list = el('div', { class: 'scroll tall' });
  body.append(list);
  const open = new Set();

  function makeRow() {
    const box = el('div', { class: 'datamsg' });
    const row = el('div', { class: 'row' });
    const t = el('span', { class: 'muted tiny' });
    const m = el('span', { class: 'pill info' });
    const f = el('span', { class: 'badge idle' });
    const x = el('span', { class: 'grow tiny' });
    row.append(t, m, f, x);
    const dl = el('dl', { class: 'kv' });
    box.append(row, dl);
    box._parts = { t, m, f, x, dl };
    row.addEventListener('click', () => {
      const k = box._key;
      if (open.has(k)) open.delete(k); else open.add(k);
      fill(box);
    });
    return box;
  }

  function fill(box) {
    const p = box._p, dl = box._parts.dl;
    dl.replaceChildren();
    dl.style.display = open.has(box._key) ? '' : 'none';
    if (!open.has(box._key)) return;
    for (const r of p.rows || []) dl.append(el('dt', { text: r.name }), el('dd', { text: [r.bits, r.text].filter(Boolean).join('  ') }));
  }

  const keyOf = (p) => `${p.idx}/${p.no}/${p.t_sec.toFixed(3)}/${p.label}`;

  function draw() {
    const all = store.state.ext.message || [];
    if (!all.length) return;
    pane.style.display = '';
    setText(sub, `${all.length} 件`);
    syncList(list, all.slice(-300).reverse(), keyOf, makeRow, (node, p) => {
      node._p = p;
      node._key = keyOf(p);
      setText(node._parts.t, hms(p.t_sec));
      setText(node._parts.m, `${p.mhz.toFixed(4)}`);
      setText(node._parts.f, p.from || p.format);
      setText(node._parts.x, p.label);
      fill(node);
    });
  }

  store.on('message', draw);
  draw();
}
