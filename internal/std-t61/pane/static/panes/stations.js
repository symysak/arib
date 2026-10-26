import { TOPIC } from '../store.js';
import { UI_TICK_MS } from '../ui.js';
import { hms, wallShort } from '../time.js';
import { el, setText, setClass, syncList } from '../dom.js';

const f1 = (v) => (Number.isFinite(v) ? v.toFixed(1) : '-');
const COLS = ['周波数', '状態', '種別', 'モード', '通信種別', 'RICH 17ビット', 'RICH一致', '位相誤差', 'レベル', 'TA', '識別番号', '最終'];

export function init(store) {
  const body = document.getElementById('stations-body');
  const sub = document.getElementById('stations-sub');
  const table = el('table', { class: 'stations' }, [
    el('thead', {}, [el('tr', {}, COLS.map((c) => el('th', { text: c })))]),
  ]);
  const tbody = el('tbody');
  table.appendChild(tbody);
  body.appendChild(el('div', { class: 'hscroll' }, [table]));

  const rows = new Map();

  function makeRow(st) {
    const cells = {};
    const tr = el('tr', { 'data-idx': String(st.idx) });
    const add = (key, cls) => {
      const td = el('td', cls ? { class: cls } : {});
      cells[key] = td;
      tr.appendChild(td);
      return td;
    };
    const chip = el('i', { class: `chip c${st.idx % 8}` });
    const freq = add('freq', 'freq left');
    freq.appendChild(chip);
    cells.freqText = el('span');
    cells.note = el('span', { class: 'muted' });
    cells.label = el('span', { class: 'pill info' });
    freq.append(cells.freqText, cells.label, cells.note);

    const state = add('state', 'left');
    cells.dot = el('i', { class: 'dot off' });
    cells.stateText = el('span');
    state.append(cells.dot, cells.stateText);

    add('kind', 'left');
    const mode = add('mode', 'left');
    cells.modeTag = el('span', { class: 'tag idle' });
    mode.appendChild(cells.modeTag);
    add('callType', 'left tiny');

    add('bits', 'bits left');
    const rich = add('rich');
    cells.richText = el('span');
    cells.bar = el('i');
    rich.append(cells.richText, el('div', { class: 'bar' }, [cells.bar]));

    add('phase');
    add('level');
    add('ta');
    const id = add('id', 'left');
    cells.idText = el('span');
    cells.idAge = el('span', { class: 'muted' });
    id.append(cells.idText, cells.idAge);
    add('last');
    return { tr, cells };
  }

  function update(st, selected) {
    const r = rows.get(st.idx);
    if (!r) {
      return;
    }
    const c = r.cells;
    setText(c.freqText, st.mhz.toFixed(4));
    setText(c.note, st.note ? ` ${st.note}` : '');
    const sysLabel = st.ext && st.ext.label;
    setText(c.label, sysLabel || '');
    c.label.style.display = sysLabel ? '' : 'none';
    c.label.style.marginLeft = '6px';
    const talking = !!(st.ext && st.ext.talking);
    const [dot, label] = st.ledger ? ['off', '前回まで']
      : st.watched === false ? ['off', '監視終了']
      : st.recording ? ['rec', '録音']
      : talking ? ['talk', '通話']
      : !st.synced ? ['off', '未同期']
      : st.rich_ok ? ['on', '同期'] : ['weak', '同期(RICH NG)'];
    setClass(c.dot, `dot ${dot}`);
    setText(c.stateText, label);
    setClass(r.tr, [selected ? 'sel' : '', st.watched === false ? 'idle-row' : ''].filter(Boolean).join(' '));
    setText(c.kind, (st.kind || '-') + (st.uplink ? '(上り)' : ''));
    const mode = st.mode || '-';
    setClass(c.modeTag, `tag ${mode === '音声' ? 'voice' : mode === 'FACCH' ? 'facch' : mode.startsWith('非音声') ? 'nv' : 'idle'}`);
    setText(c.modeTag, mode);
    setText(c.callType, st.call_type || '');
    if (c.callType.title !== (st.call_down || '')) c.callType.title = st.call_down || '';
    setText(c.bits, st.rich_bits || '-');
    const pct = st.frames ? (100 * st.rich_ok_num) / st.frames : 0;
    setText(c.richText, `${f1(pct)}%`);
    const w = `${Math.max(0, Math.min(100, pct)).toFixed(0)}%`;
    if (c.bar.style.width !== w) c.bar.style.width = w;
    setText(c.phase, `${f1(st.phase_err_deg)}°`);
    setText(c.level, `${f1(st.level_db)} dB`);
    setText(c.ta, st.sb_num ? String(st.ta) : '-');
    const stale = !!st.sb_id && st.sb_stale;
    setText(c.idText, st.sb_id || '-');
    setClass(c.idText, stale ? 'muted' : '');
    setText(c.idAge, stale && st.sb_t_sec ? ` （直近 ${hms(st.sb_t_sec)}）` : '');
    setText(c.last, st.ledger ? wallShort(st.ledger_last_ms) : (st.last_t_sec ? hms(st.last_t_sec) : '-'));
  }

  function draw(s) {
    syncList(tbody, s.stations, (st) => st.idx, (st) => {
      const r = makeRow(st);
      rows.set(st.idx, r);
      return r.tr;
    });
    if (rows.size !== s.stations.length) {
      const live = new Set(s.stations.map((x) => x.idx));
      for (const idx of [...rows.keys()]) if (!live.has(idx)) rows.delete(idx);
    }
    for (const st of s.stations) update(st, st.idx === s.selected);
    const sync = s.stations.filter((x) => x.synced).length;
    const rich = s.stations.filter((x) => x.synced && x.rich_ok).length;
    const past = s.stations.filter((x) => x.ledger).length;
    const live = s.stations.length - past;
    setText(sub, `${sync}/${live} 局が同期中（うち RICH 一致 ${rich}）`
      + (past ? `　台帳から ${past} 局` : ''));
  }

  tbody.addEventListener('click', (e) => {
    const tr = e.target.closest('tr');
    if (tr && tr.dataset.idx !== undefined) store.select(Number(tr.dataset.idx));
  });

  store.on(TOPIC.stations, draw);
  store.on(TOPIC.selection, draw);
  setInterval(() => draw(store.state), UI_TICK_MS);
}
