import { TOPIC } from '../store.js';
import { UI_TICK_MS } from '../ui.js';
import { hms } from '../time.js';
import { el, setText, setClass, syncList } from '../dom.js';

const COLS = ['周波数', '状態', 'スクランブル', 'CCH', 'TCH', '通報', 'レベル', '最終'];

const pct = (a, b) => (b > 0 ? `${(100 * a / b).toFixed(1)}%` : '-');
const hex4 = (v) => `0x${v.toString(16).toUpperCase().padStart(4, '0')}`;

export function init(store) {
  const body = document.getElementById('stations-body');
  const sub = document.getElementById('stations-sub');
  const tbody = el('tbody');
  body.appendChild(el('div', { class: 'hscroll' }, [el('table', { class: 'stations' }, [
    el('thead', {}, [el('tr', {}, COLS.map((c) => el('th', { text: c })))]), tbody])]));

  function makeRow(st) {
    const tr = el('tr', { 'data-idx': String(st.idx) });
    const td = (cls) => { const d = el('td', cls ? { class: cls } : {}); tr.appendChild(d); return d; };
    const freq = td('freq left');
    const freqText = el('span');
    freq.append(el('i', { class: `chip c${st.idx % 8}` }), freqText);
    const state = td('left');
    const dot = el('i', { class: 'dot off' });
    const stateText = el('span');
    state.append(dot, stateText);
    tr._parts = { freqText, dot, stateText, scr: td('left'), cch: td(), tch: td(), bc: td(), level: td(), last: td() };
    return tr;
  }

  function draw(s) {
    syncList(tbody, s.stations, (st) => st.idx, makeRow, (tr, st) => {
      const p = tr._parts;
      setText(p.freqText, st.mhz.toFixed(5));
      const talking = !!(st.ext && st.ext.talking);
      const [dot, label] = st.watched === false ? ['off', '監視終了']
        : st.current ? [talking ? 'talk' : 'rec', `通報中 #${st.current}`]
        : st.synced ? ['on', '受信中'] : ['off', '待機'];
      setClass(p.dot, `dot ${dot}`);
      setText(p.stateText, label);
      const sc = st.scramble || {};
      setText(p.scr, !sc.locked ? '判定中' : `${hex4(sc.init)}${sc.pinned ? '（固定）' : ''}`);
      setText(p.cch, pct(st.cch_ok, st.cch_total));
      setText(p.tch, pct(st.tch_ok, st.tch_total));
      setText(p.bc, `${st.broadcasts || 0}`);
      setClass(tr, [st.idx === s.selected ? 'sel' : '', st.watched === false ? 'idle-row' : ''].filter(Boolean).join(' '));
      setText(p.level, Number.isFinite(st.level_db) ? `${st.level_db.toFixed(1)} dB` : '-');
      setText(p.last, st.last_t_sec ? hms(st.last_t_sec) : '-');
    });
    const on = s.stations.filter((x) => x.current).length;
    setText(sub, `${s.stations.length} 局${on ? `（通報中 ${on}）` : ''}`);
  }

  tbody.addEventListener('click', (e) => {
    const tr = e.target.closest('tr');
    if (tr && tr.dataset.idx !== undefined) store.select(Number(tr.dataset.idx));
  });

  store.on(TOPIC.stations, draw);
  store.on(TOPIC.selection, draw);
  setInterval(() => draw(store.state), UI_TICK_MS);
}
