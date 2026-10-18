import { store, TOPIC } from './store.js';
import { createStationState, SEED_AUTO } from './t86/state.js';
import { setOrigin } from './t86/time.js';

export { SEED_AUTO };

const stations = new Map();
const subs = new Map();
let pending = new Set();
let flushQueued = false;
const blank = createStationState(-1, () => {});

function stationState(idx) {
  let s = stations.get(idx);
  if (!s) {
    s = createStationState(idx, (topic) => { if (idx === store.state.selected) queue(topic); });
    s.state.conn = store.state.conn;
    stations.set(idx, s);
  }
  return s;
}

function cur() { return stations.get(store.state.selected) || blank; }

function queue(topic) {
  pending.add(topic);
  if (flushQueued) return;
  flushQueued = true;
  requestAnimationFrame(() => {
    flushQueued = false;
    const topics = pending;
    pending = new Set();
    const st = cur().state;
    for (const t of topics) {
      const set = subs.get(t);
      if (!set) continue;
      for (const fn of set) {
        try { fn(st); } catch (e) { console.error(`t86 pane[${t}]`, e); }
      }
    }
  });
}

function touchAll() {
  for (const t of subs.keys()) queue(t);
  queue('snapshot');
}

export const t86 = {
  get state() { return cur().state; },
  on(topic, fn) {
    if (!subs.has(topic)) subs.set(topic, new Set());
    subs.get(topic).add(fn);
    return () => subs.get(topic).delete(fn);
  },
  get idx() { return stations.has(store.state.selected) ? store.state.selected : -1; },
  select: (seq) => cur().select(seq),
  selectedMessage: () => cur().selectedMessage(),
  addLog: (...a) => cur().addLog(...a),
  addMarker: (...a) => cur().addMarker(...a),
  toggleSquelch: () => cur().toggleSquelch(),
  toggleBroadcastStrict: () => cur().toggleBroadcastStrict(),
  toggleCFO: () => cur().toggleCFO(),
  resetCFO: () => cur().resetCFO(),
  pinSeed: (seed) => cur().pinSeed(seed),
  station: (idx) => (stations.get(idx) || null),
};

function latestWindow(st) {
  let best = null;
  for (const [id, w] of Object.entries(st.windows || {})) {
    if (!best || Number(id) > best.id) best = { id: Number(id), w };
  }
  return best;
}

store.extend({
  topics: { t86: 't86' },
  snapshot(s) {
    if (s.meta) setOrigin(s.meta.t0_wall_ms, s.meta.t0_estimated, s.meta.t0_source);
    for (const [k, v] of Object.entries(s.t86 || {})) stationState(Number(k)).applySnapshot(v);
    if (!stations.has(store.state.selected) && store.state.stations.length) {
      store.select(store.state.stations[0].idx);
    }
    touchAll();
  },
  events: {
    t86(ev) {
      const fresh = !stations.has(ev.idx);
      stationState(ev.idx).apply(ev.ev);
      if (fresh && !stations.has(store.state.selected)) store.select(ev.idx);
    },
  },
  selected() { touchAll(); },
  trend: (st) => ({ slots: (st.slots || []).slice(), synced: !!st.synced }),
  audio: {
    rate: 16000,
    jitterSec: 0.6,
    maxBufferSec: 8.0,
    rows: [
      { id: 'au-t86-win', label: '通報' },
      { id: 'au-t86-crc', label: 'CRC7 一致' },
      { id: 'au-t86-fill', label: '欠落補間' },
    ],
    draw(st) {
      const s = st && stations.get(st.idx);
      const lw = s && latestWindow(s.state);
      if (!lw || !lw.w.audio) return { 'au-t86-win': lw ? `#${lw.id}` : '-' };
      const a = lw.w.audio;
      return {
        'au-t86-win': `#${lw.id}${lw.w.t_end == null ? '（通報中）' : ''}`,
        'au-t86-crc': a.frames ? `${a.crc7_ok}/${a.frames}（${(100 * a.crc7_ok / a.frames).toFixed(1)}%）` : '-',
        'au-t86-fill': String(a.filled ?? 0),
      };
    },
  },
});

store.on(TOPIC.conn, (s) => {
  for (const st of stations.values()) st.state.conn = s.conn;
  blank.state.conn = s.conn;
  queue('conn');
});
