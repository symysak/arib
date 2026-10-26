import { hms } from '../time.js';
import { el, setText, setClass, syncList } from '../dom.js';

function nodeEl(n, depth) {
  const row = el('div', { class: 'tnode' });
  row.style.paddingLeft = `${depth * 14}px`;
  const has = n.children && n.children.length;
  const tw = el('span', { class: 'twist', text: has ? '▾' : '' });
  const label = el('span', { class: 'tlabel', text: n.label });
  const val = el('span', { class: 'tval', text: n.value || '' });
  const bits = el('span', { class: 'tbits', text: n.bits || '' });
  const rng = el('span', { class: 'trange', text: n.range || '' });
  const note = el('span', { class: 'tnote', text: n.note || '' });
  row.append(tw, label, val, bits, rng, note);
  const box = el('div', {});
  box.appendChild(row);
  if (has) {
    const kids = el('div', {});
    for (const c of n.children) kids.appendChild(nodeEl(c, depth + 1));
    box.appendChild(kids);
    tw.style.cursor = 'pointer';
    tw.addEventListener('click', () => {
      const open = kids.style.display !== 'none';
      kids.style.display = open ? 'none' : '';
      tw.textContent = open ? '▸' : '▾';
    });
  }
  return box;
}

export function renderTree(container, tree) {
  container.replaceChildren();
  for (const n of tree || []) container.appendChild(nodeEl(n, 0));
  if (!tree || !tree.length) container.appendChild(el('div', { class: 'muted tiny', text: '（中身なし）' }));
}

export function renderHex(container, hex) {
  container.replaceChildren();
  if (!hex) { container.appendChild(el('div', { class: 'muted tiny', text: '（生データなし）' })); return; }
  for (let i = 0; i < hex.length; i += 32) {
    const chunk = hex.slice(i, i + 32);
    const bytes = chunk.match(/../g) || [];
    container.appendChild(el('div', { class: 'hexline' }, [
      el('span', { class: 'hexoff', text: (i / 2).toString(16).padStart(4, '0') }),
      el('span', { class: 'hexb', text: bytes.join(' ') }),
    ]));
  }
}

export function makeLayerPane(store, opts) {
  const body = document.getElementById(`${opts.pane}-body`);
  const sub = document.getElementById(`${opts.pane}-sub`);
  const list = el('div', { class: 'scroll tall lp-list' });
  const empty = el('div', { class: 'muted lp-empty' });
  const left = el('div', { class: 'grow lp-left' }, [list, empty]);
  const tree = el('div', { class: 'scroll tall tree lp-tree' });
  const hex = el('div', { class: 'hexdump' });
  const title = el('div', { class: 'row lp-title' });
  const right = el('div', { class: 'grow' }, [title, tree, hex]);
  body.classList.add('split', 'layerpane');
  body.append(left, right);

  let selKey = null;

  function makeRow() {
    const row = el('div', { class: 'row pick' });
    row._cells = opts.columns.map((c) => {
      const e = el('span', { class: c.class || '' });
      row.appendChild(e);
      return e;
    });
    return row;
  }

  function draw() {
    const items = opts.items(store.state);
    setText(sub, `${items.length} 件`);
    const shown = items.slice(-400).reverse();
    syncList(list, shown, opts.key, makeRow, (node, it) => {
      opts.columns.forEach((c, i) => setText(node._cells[i], c.get(it)));
      setClass(node, opts.key(it) === selKey ? 'row pick sel' : 'row pick');
    });
    setText(empty, shown.length ? '' : opts.empty);
    empty.style.display = shown.length ? 'none' : '';
    drawDetail(items);
  }

  function drawDetail(items) {
    const hit = selKey ? items.find((i) => opts.key(i) === selKey)
      : (opts.autoPick ? opts.autoPick(items) : items[items.length - 1]);
    if (!hit) { title.replaceChildren(el('span', { class: 'muted tiny', text: '—' })); renderTree(tree, []); renderHex(hex, ''); return; }
    const d = opts.detail(hit);
    title.replaceChildren(
      el('span', { class: selKey ? 'badge warn' : 'badge info', text: selKey ? '選択中' : '最新（自動追従）' }),
      el('span', { class: 'grow tiny', text: d.title }),
      ...(d.links || []));
    renderTree(tree, d.tree);
    renderHex(hex, d.hex);
  }

  list.addEventListener('click', (e) => {
    const row = e.target.closest('[data-key]');
    if (row) { selKey = row.dataset.key; draw(); }
  });

  store.on(opts.topic, draw);
  store.on('selection', draw);
  draw();
  return { draw, follow: () => { selKey = null; draw(); } };
}

export const timeCol = { name: '時刻', class: 'muted tiny', get: (i) => hms(i.t_sec) };
export const freqCol = (store) => ({ name: '局', class: 'mono', get: (i) => {
  const st = store.station(i.idx);
  return st ? st.mhz.toFixed(4) : '?';
} });
