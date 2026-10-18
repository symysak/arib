import { TOPIC } from '../store.js';
import { UI_TICK_MS } from '../ui.js';
import { hms, wallShort } from '../time.js';
import { el, setText, setClass, syncList } from '../dom.js';

const COLS = ['周波数', '状態', 'レベル', '最終'];

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
    const chip = el('i', { class: `chip c${st.idx % 8}` });
    const freqText = el('span');
    const note = el('span', { class: 'muted' });
    freq.append(chip, freqText, note);
    const state = td('left');
    const dot = el('i', { class: 'dot off' });
    const stateText = el('span');
    state.append(dot, stateText);
    tr._parts = { freqText, note, dot, stateText, level: td(), last: td() };
    return tr;
  }

  function draw(s) {
    syncList(tbody, s.stations, (st) => st.idx, makeRow, (tr, st) => {
      const p = tr._parts;
      setText(p.freqText, st.mhz.toFixed(4));
      setText(p.note, st.note ? ` ${st.note}` : '');
      const talking = !!(st.ext && st.ext.talking);
      const [dot, label] = st.ledger ? ['off', '前回まで']
        : st.watched === false ? ['off', '監視終了']
        : st.recording ? ['rec', '録音']
        : talking ? ['talk', '通話']
        : st.synced ? ['on', '同期'] : ['off', '未同期'];
      setClass(p.dot, `dot ${dot}`);
      setText(p.stateText, label);
      setClass(tr, [st.idx === s.selected ? 'sel' : '', st.watched === false ? 'idle-row' : ''].filter(Boolean).join(' '));
      setText(p.level, Number.isFinite(st.level_db) ? `${st.level_db.toFixed(1)} dB` : '-');
      setText(p.last, st.ledger ? wallShort(st.ledger_last_ms) : (st.last_t_sec ? hms(st.last_t_sec) : '-'));
    });
    const live = s.stations.filter((x) => !x.ledger).length;
    const sync = s.stations.filter((x) => x.synced).length;
    setText(sub, `${sync}/${live} 局が同期中`);
  }

  tbody.addEventListener('click', (e) => {
    const tr = e.target.closest('tr');
    if (tr && tr.dataset.idx !== undefined) store.select(Number(tr.dataset.idx));
  });

  store.on(TOPIC.stations, draw);
  store.on(TOPIC.selection, draw);
  setInterval(() => draw(store.state), UI_TICK_MS);
}
