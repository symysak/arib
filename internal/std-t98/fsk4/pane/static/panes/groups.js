import { TOPIC } from '../store.js';
import { UI_TICK_MS } from '../ui.js';
import { hms, dur } from '../time.js';
import { el, setText, setClass, syncList } from '../dom.js';
import { cell } from '../nxdn.js';
import { callCell } from '../t98.js';

const COLS = ['チャネル', 'UC', '通話識別', '製造者', 'メッセージ種別', '送信', '合計 / 音声', '無線機（呼出名称）', '最終'];

export function init(store) {
  const body = document.getElementById('groups-body');
  const sub = document.getElementById('groups-sub');
  const tbody = el('tbody');
  const empty = el('div', { class: 'muted tiny', text: 'まだ送信を聞いていません' });
  body.append(el('div', { class: 'scroll tall' }, [el('table', { class: 't98table' }, [
    el('thead', {}, [el('tr', {}, COLS.map((c) => el('th', { text: c })))]), tbody])]), empty);

  function makeRow() {
    const tr = el('tr');
    tr._c = COLS.map((_, i) => { const d = el('td', { class: i === 7 ? 'wrap mono tiny' : i === 1 ? 'mono' : 'tiny' }); tr.appendChild(d); return d; });
    tr._tag = el('span', { class: 'tag' });
    tr._c[2].append(tr._tag);
    tr.addEventListener('click', () => { if (tr._idx !== undefined) store.pickStation(tr._idx); });
    return tr;
  }

  function draw() {
    const s = store.state;
    const rows = [];
    for (const st of s.stations) {
      if (!store.inScope(st.idx)) continue;
      const ch = st.ext && st.ext.ch;
      for (const g of (st.ext && st.ext.groups) || []) rows.push({ ...g, idx: st.idx, mhz: st.mhz, ch });
    }
    rows.sort((a, b) => b.last_t - a.last_t);
    const cipher = rows.filter((r) => r.cipher).length;
    const keyed = rows.filter((r) => r.cipher && r.key > 0).length;
    setText(sub, `${s.showAll ? '全局' : '選択局'}の ${rows.length} 組（秘話 ${cipher}・うち鍵が分かった ${keyed}）`);
    empty.style.display = rows.length ? 'none' : '';
    syncList(tbody, rows, (r) => `${r.idx}/${r.uc}`, makeRow, (tr, r) => {
      tr._idx = r.idx;
      setClass(tr, store.marked(r.idx) ? 'sel' : '');
      const c = tr._c;
      setText(c[0], r.ch ? `${r.ch.class} ${r.ch.no} ch` : r.mhz.toFixed(4));
      const title = `${r.mhz.toFixed(4)} MHz${r.ch && r.ch.note ? `・${r.ch.note}` : ''}`;
      if (c[0].title !== title) c[0].title = title;
      setText(c[1], String(r.uc));
      cell(tr._tag, callCell(r.cipher, r.key, r.key_how));
      setText(c[3], r.maker > 0 ? String(r.maker) : r.maker === 0 ? '共通' : '-');
      setText(c[4], (r.types || []).join('・') || '-');
      setText(c[5], String(r.txs));
      setText(c[6], `${dur(r.sec)} / ${dur(r.voice_sec)}`);
      setText(c[7], (r.radios || []).join(' ') || '-');
      setText(c[8], hms(r.last_t));
    });
  }

  store.on(TOPIC.stations, draw);
  store.on(TOPIC.selection, draw);
  setInterval(draw, UI_TICK_MS * 5);
  draw();
}
