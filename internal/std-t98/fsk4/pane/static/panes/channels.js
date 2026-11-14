import { TOPIC } from '../store.js';
import { UI_TICK_MS } from '../ui.js';
import { hms } from '../time.js';
import { el, setText, setClass } from '../dom.js';

const EDGE = 0.45;
const HALF = 0.003125;
const CELL_W = 28;
const NAME_W = 158;

export function init(store) {
  const body = document.getElementById('channels-body');
  const sub = document.getElementById('channels-sub');
  const allBox = el('input', { type: 'checkbox' });
  const allLabel = el('label', { class: 'chall tiny' }, [allBox, el('span', { text: '帯域の外の区分も出す' })]);
  const msg = el('span', { class: 'tiny muted' });
  const grid = el('div', { class: 'chplan' });
  const info = el('div', { class: 'chinfo tiny' });
  const legend = el('div', { class: 'legend chlegend' });
  for (const [cls, text] of [['cell live', '送信中'], ['cell heard', '聞いた 1〜2 本'], ['cell heard h1', '3〜9 本'], ['cell heard h2', '10 本〜'], ['cell watch', '監視中（静か）'], ['cell out', '帯域の外'], ['cell watch sel', '選択中']]) {
    const sw = el('i', { class: cls, text: '' });
    sw.style.cssText = 'width:14px;height:10px;min-width:0;display:inline-block;animation:none;cursor:default';
    legend.append(el('span', { class: 'lg' }, [sw, el('span', { text })]));
  }
  legend.append(msg, allLabel);
  body.append(grid, info, legend);
  let showAll = false;
  let built = '';
  let cells = new Map();
  let hover = null;
  const open = new Set();

  allBox.addEventListener('change', () => { showAll = allBox.checked; built = ''; draw(); });

  const chansOf = (s) => (s.meta && s.meta.ext && s.meta.ext.channels) || [];

  function covered(meta, mhz) {
    for (const i of meta.inputs || []) {
      if (Math.abs(mhz - i.center_mhz) <= (i.sample_rate / 1e6) * EDGE) return true;
    }
    return false;
  }

  function stationAt(s, mhz) {
    let best = null;
    for (const st of s.stations) {
      if (st.ledger || Math.abs(st.mhz - mhz) > HALF) continue;
      if (!best || (st.watched !== false && best.watched === false)) best = st;
    }
    return best;
  }

  function build(s) {
    const chans = (s.meta && s.meta.ext && s.meta.ext.channels) || [];
    const classes = [];
    for (const c of chans) if (!classes.includes(c.class)) classes.push(c.class);
    const order = ['登録局', '登録局（上空）', '免許局', '免許局（中継・上り）', '免許局（中継・下り）', '免許局 150 MHz 帯'];
    classes.sort((a, b) => order.indexOf(a) - order.indexOf(b));
    grid.replaceChildren();
    cells = new Map();
    for (const cl of classes) {
      const list = chans.filter((c) => c.class === cl);
      if (!showAll && !list.some((c) => covered(s.meta, c.mhz) || stationAt(s, c.mhz))) continue;
      list.sort((a, b) => Number(a.no.replace(/\D/g, '')) - Number(b.no.replace(/\D/g, '')));
      const lo = Math.min(...list.map((c) => c.mhz)), hi = Math.max(...list.map((c) => c.mhz));
      const box = el('div', { class: `cells${open.has(cl) ? ' wrap' : ''}` });
      const show = open.has(cl) ? list.map((_, i) => i) : fitRow(s, list);
      let prev = -1;
      const gap = (n) => {
        const g = el('div', { class: 'cell more', text: '…' });
        g.title = `隠している ${n} 波を出す`;
        g.addEventListener('click', () => { open.add(cl); built = ''; draw(); });
        box.append(g);
      };
      for (const i of show) {
        if (i > prev + 1) gap(i - prev - 1);
        prev = i;
        const c = list[i];
        const cell = el('div', { class: 'cell', text: c.no });
        cell._ch = c;
        cell.addEventListener('mouseenter', () => { hover = c; drawInfo(store.state); });
        cell.addEventListener('click', () => onClick(c));
        box.append(cell);
        cells.set(c.class + c.no, cell);
      }
      if (open.has(cl) && fitRow(s, list).length < list.length) {
        const g = el('div', { class: 'cell more fold', text: '畳む' });
        g.addEventListener('click', () => { open.delete(cl); built = ''; draw(); });
        box.append(g);
      }
      grid.append(el('div', { class: 'band' }, [
        el('div', { class: 'name' }, [el('span', { text: `${cl}（${list.length} 波）` }), el('span', { class: 'range', text: `${lo.toFixed(5)}〜${hi.toFixed(5)}` })]),
        box]));
    }
    if (!grid.children.length) grid.append(el('div', { class: 'muted tiny', text: '入力の帯域に規格の割当周波数が入っていません（「帯域の外の区分も出す」で表だけ見られます）' }));
  }

  function keep(s, c) {
    const st = stationAt(s, c.mhz);
    if (!st) return false;
    return store.marked(st.idx) || (st.txs || 0) > 0 || s.nxdntxs.some((t) => t.idx === st.idx);
  }

  function rowCap() { return Math.max(4, Math.floor((grid.clientWidth - NAME_W - 4 + 2) / CELL_W)); }

  function fitRow(s, list) {
    const cap = rowCap();
    if (list.length <= cap) return list.map((_, i) => i);
    const on = new Array(list.length).fill(false);
    on[0] = true; on[list.length - 1] = true;
    list.forEach((c, i) => { if (keep(s, c)) on[i] = true; });
    const width = () => {
      let n = 0;
      for (let i = 0; i < on.length; i++) if (on[i]) n += 1 + (i > 0 && !on[i - 1] ? 1 : 0);
      return n;
    };
    for (let i = 1; i < on.length - 1; i++) {
      if (on[i]) continue;
      on[i] = true;
      if (width() > cap) { on[i] = false; break; }
    }
    return on.map((v, i) => (v ? i : -1)).filter((i) => i >= 0);
  }

  function onClick(c) {
    const st = stationAt(store.state, c.mhz);
    if (st) { store.pickStation(st.idx); return; }
    setText(msg, `${c.class} ${c.no} ch（${c.mhz.toFixed(5)} MHz）は${covered(store.state.meta, c.mhz) ? 'まだ監視していない' : '入力の帯域の外'}`);
  }

  function statOf(s, st) {
    const txs = s.nxdntxs.filter((t) => t.idx === st.idx);
    const groups = (st.ext && st.ext.groups) || [];
    return {
      txs: Math.max(st.txs || 0, txs.length), open: txs.some((t) => t.open), groups,
      cipher: groups.some((g) => g.cipher), radios: ((st.ext && st.ext.radios) || []).length,
    };
  }

  function drawInfo(s) {
    if (!hover) { setText(info, 'マスに触れるとそのチャネルの様子を出します'); return; }
    const c = hover;
    const st = stationAt(s, c.mhz);
    let text = `${c.class} ${c.no} ch・${c.mhz.toFixed(6)} MHz`;
    if (c.note) text += `・${c.note}`;
    if (!st) {
      text += covered(s.meta, c.mhz) ? '・監視していない' : '・入力の帯域の外';
    } else {
      const x = statOf(s, st);
      text += `・送信 ${x.txs} 本・無線機 ${x.radios} 台`;
      if (x.groups.length) text += '・' + x.groups.map((g) => `UC ${g.uc}${g.cipher ? `（秘話${g.key > 0 ? `・鍵 ${g.key}` : ''}）` : ''}`).join(' / ');
      if (st.last_t_sec) text += `・最終 ${hms(st.last_t_sec)}`;
    }
    setText(info, text);
  }

  function draw() {
    const s = store.state;
    if (!s.meta) return;
    const key = `${showAll}|${(s.meta.inputs || []).map((i) => `${i.center_mhz}/${i.sample_rate}`).join(',')}|${((s.meta.ext && s.meta.ext.channels) || []).length}|${s.stations.length}|${rowCap()}|${[...open].join(',')}|${chansOf(s).filter((c) => keep(s, c)).map((c) => c.class + c.no).join(',')}`;
    if (key !== built) { build(s); built = key; }
    let nHeard = 0, nLive = 0, nWatch = 0;
    for (const cell of cells.values()) {
      const c = cell._ch;
      const st = stationAt(s, c.mhz);
      let cls = 'cell';
      let lock = false;
      if (st) {
        const x = statOf(s, st);
        lock = x.cipher;
        if (st.watched !== false) nWatch++;
        if (x.open || (st.synced && st.recording)) { cls += ' live'; nLive++; nHeard++; }
        else if (x.txs > 0) { cls += ` heard${x.txs >= 10 ? ' h2' : x.txs >= 3 ? ' h1' : ''}`; nHeard++; }
        else cls += st.watched !== false ? ' watch' : ' out';
        if (store.marked(st.idx)) cls += ' sel';
      } else {
        cls += ' out';
      }
      if (c.note) cls += ' special';
      setClass(cell, cls);
      const want = lock ? '🔒' : '';
      let mark = cell._lock;
      if (want && !mark) { mark = el('span', { class: 'lock', text: want }); cell.append(mark); cell._lock = mark; }
      if (!want && mark) { mark.remove(); cell._lock = null; }
    }
    setText(sub, `割当 ${((s.meta.ext && s.meta.ext.channels) || []).length} 波のうち 監視 ${nWatch}（走査なし）・使われた ${nHeard}・送信中 ${nLive}`);
    drawInfo(s);
  }

  store.on(TOPIC.meta, draw);
  store.on(TOPIC.stations, draw);
  store.on(TOPIC.selection, draw);
  store.on('nxdntxs', draw);
  window.addEventListener('resize', draw);
  setInterval(draw, UI_TICK_MS * 5);
  draw();
}
