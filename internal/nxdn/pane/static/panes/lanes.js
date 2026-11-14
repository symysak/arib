import { TOPIC } from '../store.js';
import { fit, clear, COLOR } from '../draw.js';
import { UI_TICK_MS } from '../ui.js';
import { hms } from '../time.js';
import { el, setText, setClass, syncList } from '../dom.js';
import { cfg, cell } from '../nxdn.js';

const SPANS = [[300, '直近 5 分'], [1800, '30 分'], [7200, '2 時間']];
const ROW_H = 22;
const AXIS_H = 16;

const TX_BANDS = [
  { label: '音声', color: '#d05a45', match: (k) => k === '音声' },
  { label: 'データ', color: '#2f9e6e', match: (k) => (k || '').startsWith('データ') },
  { label: '送信（その他）', color: '#8a5cc2', match: () => true },
];
const BASE = [
  { label: '同期だけ（読めず・外れる前）', color: '#c9b07a' },
  { label: '送信なし', color: '#e6e8ec' },
];
const pick = (list, x) => list.find((b) => b.match(x)).color;

function swatch(color) {
  const s = el('i', { class: 'sw' });
  s.style.background = color;
  return s;
}

export function init(store) {
  const body = document.getElementById('lanes-body');
  const sub = document.getElementById('lanes-sub');
  if (!body) return;
  let span = SPANS[0][0];
  let showQuiet = !cfg.hideQuiet;
  let rows = [];
  let geom = null;

  const tools = el('div', { class: 'toolbar' });
  const spanBtns = SPANS.map(([sec, label]) => {
    const b = el('button', { class: 'btn', type: 'button', text: label });
    b.addEventListener('click', () => { span = sec; draw(); });
    tools.append(b);
    return [sec, b];
  });
  const quietBtn = el('button', { class: 'btn', type: 'button', text: '静かな局も出す' });
  quietBtn.addEventListener('click', () => { showQuiet = !showQuiet; draw(); });
  tools.append(el('span', { class: 'sep' }), quietBtn);
  const legend = el('span', { class: 'legend' });
  legend.style.cssText = 'margin-left:auto;padding:0';
  for (const b of [...TX_BANDS, ...BASE]) legend.append(el('span', { class: 'lg' }, [swatch(b.color), el('span', { text: b.label })]));
  tools.append(legend);

  const cols = cfg.laneCols;
  const headRow = el('div', { class: 'lanerow lanehead' }, [el('span'), el('span', { text: '周波数' }), ...cols.map((c) => el('span', { text: c.name }))]);
  const heads = el('div', { class: 'laneheads' });
  const left = el('div', { class: 'laneleft' }, [headRow, heads]);
  left.style.gridTemplateColumns = `repeat(${cols.length + 2}, max-content)`;
  const cv = document.createElement('canvas');
  cv.style.cssText = 'width:100%;display:block';
  const right = el('div', { class: 'laneright' }, [cv]);
  const empty = el('div', { class: 'muted tiny' });
  body.append(tools, el('div', { class: 'lanes' }, [left, right]), empty);

  function makeHead() {
    const r = el('div', { class: 'lanerow' });
    const dot = el('i', { class: 'dot off' });
    const freq = el('span', { class: 'mono' });
    r._c = cols.map(() => el('span'));
    r.append(el('span', {}, [dot]), freq, ...r._c);
    r._dot = dot; r._freq = freq;
    r.addEventListener('click', () => { if (r._idx !== undefined) store.pickStation(r._idx); });
    return r;
  }

  function visible(s) {
    const heard = new Set(s.nxdntxs.map((t) => t.idx));
    return s.stations.filter((st) => showQuiet || heard.has(st.idx) || (st.txs || 0) > 0 || st.synced || store.marked(st.idx));
  }

  function nowOf(s) {
    let now = s.lastT || 0;
    for (const st of s.stations) now = Math.max(now, st.last_t_sec || 0);
    for (const t of s.nxdntxs) now = Math.max(now, t.end_sec || t.t_sec || 0);
    return now;
  }

  function draw() {
    const s = store.state;
    for (const [sec, b] of spanBtns) setClass(b, sec === span ? 'btn sel' : 'btn');
    setClass(quietBtn, showQuiet ? 'btn sel' : 'btn');
    rows = visible(s);
    const focus = store.focusedTx();
    syncList(heads, rows, (st) => st.idx, makeHead, (r, st) => {
      r._idx = st.idx;
      setClass(r, `lanerow pick${store.marked(st.idx) ? ' sel' : ''}${st.watched === false ? ' idle-row' : ''}`);
      const talking = !!(st.ext && st.ext.talking);
      setClass(r._dot, `dot ${st.ledger ? 'off' : st.recording ? 'rec' : talking ? 'talk' : st.synced ? 'on' : 'off'}`);
      setText(r._freq, st.mhz.toFixed(4));
      cols.forEach((c, i) => cell(r._c[i], c.get(st, s), c.cls || ''));
    });
    const watched = s.stations.filter((x) => !x.ledger && x.watched !== false).length;
    const quiet = s.stations.length - rows.length;
    setText(sub, `${watched} 局を監視・${rows.length} 局を表示${quiet > 0 ? `（静かな ${quiet} 局は隠している）` : ''}`);
    setText(empty, rows.length ? '' : 'まだ送信を聞いていません（「静かな局も出す」で監視中の局を全部出せます）');

    const hCss = rows.length * ROW_H + AXIS_H;
    if (cv.style.height !== `${hCss}px`) cv.style.height = `${hCss}px`;
    const { g, w, h } = fit(cv);
    clear(g, w, h);
    geom = null;
    if (!rows.length || w < 8) return;
    const accent = COLOR.accent();
    const now = nowOf(s);
    const t0 = now - span;
    const px = (t) => ((t - t0) / span) * (w - 4);
    geom = { t0, span, now };

    const step = span <= 300 ? 60 : span <= 1800 ? 300 : 1200;
    g.strokeStyle = COLOR.grid();
    g.lineWidth = 1;
    g.font = '10px system-ui, sans-serif';
    for (let t = Math.ceil(t0 / step) * step; t <= now; t += step) {
      const x = Math.round(px(t)) + 0.5;
      g.beginPath(); g.moveTo(x, 0); g.lineTo(x, rows.length * ROW_H); g.stroke();
      g.fillStyle = COLOR.ink3();
      g.fillText(hms(t), x + 2, rows.length * ROW_H + 11);
    }

    const txs = new Map();
    for (const t of s.nxdntxs) {
      if (!txs.has(t.idx)) txs.set(t.idx, []);
      txs.get(t.idx).push(t);
    }
    rows.forEach((st, i) => {
      const y = i * ROW_H;
      const bar = (ta, tb, col) => {
        if (tb < t0) return;
        const x1 = px(Math.max(ta, t0));
        g.fillStyle = col;
        g.fillRect(x1, y + 4, Math.max(1, px(tb) - x1 + 0.6), ROW_H - 8);
      };
      bar(t0, now, BASE[1].color);
      const pts = s.trend.get(st.idx) || [];
      for (let k = 0; k < pts.length; k++) {
        const p = pts[k];
        if (p.ledger || !p.synced) continue;
        const next = k + 1 < pts.length ? pts[k + 1].t : (st.watched ? now : p.t);
        bar(p.t, next, BASE[0].color);
      }
      for (const t of txs.get(st.idx) || []) {
        const te = t.open ? now : t.end_sec;
        if (te < t0) continue;
        bar(t.t_sec, te, pick(TX_BANDS, t.kind));
        if (t.t_sec >= t0) { g.fillStyle = accent; g.fillRect(px(t.t_sec) - 0.5, y + 4, 1.5, ROW_H - 8); }
        if (focus && focus.idx === t.idx && focus.id === t.id) {
          const x1 = px(Math.max(t.t_sec, t0));
          g.strokeStyle = COLOR.ink();
          g.lineWidth = 2;
          g.strokeRect(x1 - 1, y + 2.5, Math.max(3, px(te) - x1 + 2), ROW_H - 5);
          g.lineWidth = 1;
        }
      }
      if (store.marked(st.idx)) {
        g.strokeStyle = accent;
        g.strokeRect(0.5, y + 1.5, w - 5, ROW_H - 3);
      }
    });
  }

  cv.addEventListener('click', (e) => {
    const r = cv.getBoundingClientRect();
    const st = rows[Math.floor((e.clientY - r.top) / ROW_H)];
    if (!st || !geom) return;
    const tAt = geom.t0 + ((e.clientX - r.left) / (r.width - 4)) * geom.span;
    const slack = (4 / (r.width - 4)) * geom.span;
    let hit = null;
    for (const t of store.state.nxdntxs) {
      if (t.idx !== st.idx) continue;
      const te = t.open ? geom.now : t.end_sec;
      if (tAt >= t.t_sec - slack && tAt <= te + slack) hit = t;
    }
    if (hit) store.focusTx(hit);
    else store.pickStation(st.idx);
  });

  store.on(TOPIC.selection, draw);
  store.on(TOPIC.stations, draw);
  store.on('nxdntxs', draw);
  store.on('nxdnfocus', draw);
  window.addEventListener('resize', draw);
  setInterval(draw, UI_TICK_MS * 2);
  draw();
}
