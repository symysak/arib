import { TOPIC } from '../store.js';
import { UI_TICK_MS } from '../ui.js';
import { hms, wallShort } from '../time.js';
import { el, setText, setClass, syncList } from '../dom.js';

const f1 = (v) => (Number.isFinite(v) ? v.toFixed(1) : '-');
const COLS = ['周波数', '状態', '団体', '種別', 'スロット', '通信種別・発 → 着', '同期ワード', '位相誤差', 'レベル', '符号器', '最終'];

const SLOT = {
  '音声': ['音', 'ok'], 'FACCH': ['F', 'warn'], '同期バースト': ['SB', 'warn'], '制御': ['制', 'info'],
  '空線': ['空', 'idle'], '慣性': ['…', 'idle'], '未同期': ['×', 'crit'],
  '制御（上り）': ['制↑', 'info'], '送信なし': ['−', 'idle'],
};

function slotBadge(kind) {
  const call = kind.startsWith('通話:');
  const k = call ? kind.slice(3) : kind;
  const [t, c] = SLOT[k] || ['?', 'idle'];
  return { text: t, cls: `badge ${c}${call ? ' live' : ''}`, title: kind };
}

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
    const note = el('span', { class: 'muted' });
    freq.append(el('i', { class: `chip c${st.idx % 8}` }), freqText, note);
    const state = td('left');
    const dot = el('i', { class: 'dot off' });
    const stateText = el('span');
    state.append(dot, stateText);
    const org = td('left');
    const kind = td('left');
    const slots = td('left');
    const badges = [0, 1, 2, 3].map(() => el('span', { class: 'badge idle' }));
    slots.append(...badges);
    tr._p = { freqText, note, dot, stateText, org, kind, badges, peer: td('left tiny'), sw: td(), phase: td(), level: td(),
      codec: td('left tiny'), last: td() };
    return tr;
  }

  function draw(s) {
    syncList(tbody, s.stations, (st) => st.idx, makeRow, (tr, st) => {
      const p = tr._p;
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
      setText(p.org, st.org || '-');
      setText(p.kind, st.kind ? `${st.kind.replace('キャリア', '')} CC${st.cc}` : '-');
      p.kind.title = st.pair_mhz ? `${st.dir === '上り' ? '下り' : '上り'}は ${st.pair_mhz.toFixed(4)} MHz` : '';
      (st.slots || []).forEach((k, n) => {
        const b = slotBadge(k || '未同期');
        setText(p.badges[n], `${n}:${b.text}`);
        setClass(p.badges[n], b.cls);
        p.badges[n].title = `ST#${n} ${b.title}`;
      });
      setText(p.peer, [0, 1, 2, 3].map((n) => {
        const m = (st.modes || [])[n] || '';
        const x = (st.peers || [])[n] || '';
        return m || x ? `ST#${n} ${[m, x].filter(Boolean).join(' ')}` : '';
      }).filter(Boolean).join(' / ') || '-');
      setText(p.sw, `${f1(st.sw_pct)}%`);
      setText(p.phase, `${f1(st.phase_err_deg)}°`);
      setText(p.level, Number.isFinite(st.level_db) ? `${st.level_db.toFixed(1)} dB` : '-');
      setText(p.codec, st.codec || '-');
      setText(p.last, st.ledger ? wallShort(st.ledger_last_ms) : (st.last_t_sec ? hms(st.last_t_sec) : '-'));
    });
    const live = s.stations.filter((x) => !x.ledger).length;
    const sync = s.stations.filter((x) => x.synced).length;
    const ctl = s.stations.filter((x) => x.kind === '制御キャリア').length;
    const talk = s.stations.filter((x) => x.recording).length;
    setText(sub, `${sync}/${live} 局が同期中（制御キャリア ${ctl}）　通話中 ${talk}`);
  }

  tbody.addEventListener('click', (e) => {
    const tr = e.target.closest('tr');
    if (tr && tr.dataset.idx !== undefined) store.select(Number(tr.dataset.idx));
  });

  store.on(TOPIC.stations, draw);
  store.on(TOPIC.selection, draw);
  setInterval(() => draw(store.state), UI_TICK_MS);
}
