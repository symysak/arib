import { TOPIC } from '../store.js';
import { UI_TICK_MS } from '../ui.js';
import { hms } from '../time.js';
import { el, setText, setClass, syncList } from '../dom.js';
import { cell } from '../nxdn.js';
import { callCell, groupOf } from '../t98.js';

const COLS = ['呼出名称', '区分', 'チャネル', 'UC', '通話識別', '製造者', '回数', '最終'];

export function init(store) {
  const body = document.getElementById('radios-body');
  const sub = document.getElementById('radios-sub');
  const tools = el('div', { class: 'toolbar' });
  const filter = el('input', { type: 'search', placeholder: '呼出名称・UC で絞る', class: 'tiny' });
  filter.style.cssText = 'flex:1;min-width:120px';
  const sortBtn = el('button', { class: 'btn', text: '新しい順' });
  tools.append(filter, sortBtn);
  const tbody = el('tbody');
  const empty = el('div', { class: 'muted tiny', text: '呼出名称はまだ読めていません（送信の頭の同期バーストに載ります）' });
  body.append(tools, el('div', { class: 'scroll tall' }, [el('table', { class: 't98table' }, [
    el('thead', {}, [el('tr', {}, COLS.map((c) => el('th', { text: c })))]), tbody])]), empty);
  let byTime = false;
  sortBtn.addEventListener('click', () => { byTime = !byTime; setText(sortBtn, byTime ? '番号順' : '新しい順'); draw(); });
  filter.addEventListener('input', () => draw());

  function makeRow() {
    const tr = el('tr');
    tr._c = COLS.map((_, i) => { const d = el('td', { class: i === 0 ? 'mono' : 'tiny' }); tr.appendChild(d); return d; });
    tr._tag = el('span', { class: 'tag' });
    tr._c[4].append(tr._tag);
    tr.addEventListener('click', () => { if (tr._idx !== undefined) store.pickStation(tr._idx); });
    return tr;
  }

  function draw() {
    const s = store.state;
    let rows = [];
    for (const st of s.stations) {
      if (!store.inScope(st.idx)) continue;
      const ch = st.ext && st.ext.ch;
      for (const r of (st.ext && st.ext.radios) || []) rows.push({ ...r, idx: st.idx, mhz: st.mhz, ch, group: r.uc >= 0 ? groupOf(st, r.uc) : null });
    }
    const total = rows.length;
    const q = filter.value.trim();
    if (q) rows = rows.filter((r) => r.csm.includes(q) || `UC ${r.uc}` === q || String(r.uc) === q);
    rows.sort(byTime ? (a, b) => b.last_t - a.last_t : (a, b) => a.csm.localeCompare(b.csm) || a.mhz - b.mhz);
    setText(sub, `${s.showAll ? '全局' : '選択局'}の ${new Set(rows.map((r) => r.csm)).size} 台${q ? `（${total} 行から絞り込み）` : ''}`);
    empty.style.display = total ? 'none' : '';
    syncList(tbody, rows, (r) => `${r.idx}/${r.csm}`, makeRow, (tr, r) => {
      tr._idx = r.idx;
      setClass(tr, store.marked(r.idx) ? 'sel' : '');
      const c = tr._c;
      setText(c[0], r.csm);
      setText(c[1], r.class);
      setText(c[2], r.ch ? `${r.ch.no} ch` : r.mhz.toFixed(4));
      const title = r.ch ? `${r.ch.class} ${r.ch.no} ch・${r.mhz.toFixed(4)} MHz` : `${r.mhz.toFixed(4)} MHz`;
      if (c[2].title !== title) c[2].title = title;
      setText(c[3], r.uc >= 0 ? String(r.uc) : '-');
      cell(tr._tag, r.uc >= 0 ? callCell(r.cipher, r.group ? r.group.key : 0, r.group ? r.group.key_how : '') : { text: '-' });
      setText(c[5], r.maker > 0 ? String(r.maker) : r.maker === 0 ? '共通' : '-');
      setText(c[6], String(r.n));
      setText(c[7], hms(r.last_t));
    });
  }

  store.on(TOPIC.stations, draw);
  store.on(TOPIC.selection, draw);
  setInterval(draw, UI_TICK_MS * 5);
  draw();
}
