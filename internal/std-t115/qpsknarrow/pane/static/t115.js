import { store } from './store.js';

const LIMIT = {
  frames: 1200,
  controls: 600,
  trend: 600,
  constellations: 150,
  markers: 200,
};

const TOPICS = {
  frame: 't115.frame',
  control: 't115.control',
  quality: 't115.quality',
  signal: 't115.signal',
  constellation: 't115.constellation',
  scramble: 't115.scramble',
  windows: 't115.windows',
  insel: 't115.insel',
  insfilter: 't115.insfilter',
};

function emptyView() {
  return {
    quality: null,
    signal: null,
    scramble: null,
    constellation: { t_sec: 0, points: [] },
    constellations: [],
    controls: [],
    frames: [],
    broadcasts: [],
    current: null,
    lastT: 0,
    trend: [],
    corr: [],
    evm: [],
    cfo: [],
    markers: [],
    histType: new Map(),
    histCh: new Map(),
  };
}

export function keyOf(c) {
  return `${c.t_sec.toFixed(3)}/${c.raw_hex}`;
}

const views = new Map();
function view(idx) {
  let v = views.get(idx);
  if (!v) { v = emptyView(); views.set(idx, v); }
  return v;
}

const trim = (arr, n) => { if (arr.length > n) arr.splice(0, arr.length - n); };
const bumpHist = (map, key) => { if (key) map.set(key, (map.get(key) || 0) + 1); };
function addMarker(v, t, kind, text) {
  v.markers.push({ t, kind, text });
  trim(v.markers, LIMIT.markers);
}
function trendPoint(t, q) {
  return {
    t,
    cch: q.cch_total > 0 ? q.cch_ok / q.cch_total : null,
    tch: q.tch_total > 0 ? q.tch_ok / q.tch_total : null,
  };
}

function load(v, d) {
  v.quality = d.quality;
  v.signal = d.signal;
  v.scramble = d.scramble || null;
  if (d.constellation) {
    v.constellation = d.constellation;
    v.constellations = [d.constellation];
  }
  v.controls = d.controls || [];
  v.frames = d.frames || [];
  v.broadcasts = d.broadcasts || [];
  v.current = d.current || null;
  v.histType.clear();
  v.histCh.clear();
  for (const c of v.controls) {
    bumpHist(v.histType, c.msg_type_name);
    bumpHist(v.histCh, c.channel);
  }
  v.corr = v.frames.map((f) => ({ t: f.t_sec, v: f.corr }));
  trim(v.corr, LIMIT.trend);
  for (const f of v.frames) v.lastT = Math.max(v.lastT, f.t_sec);
  for (const b of v.broadcasts) {
    addMarker(v, b.start_sec, 'broadcast', `通報開始 #${b.id}`);
    addMarker(v, b.end_sec, 'broadcast', `通報終了 #${b.id}`);
  }
  if (v.quality) v.trend.push(trendPoint(v.lastT, v.quality));
}

const ALL = Object.values(TOPICS);

function on(fn, ...topics) {
  return (ev, api) => {
    fn(view(ev.idx), ev.data, api);
    if (ev.idx === api.state.selected) for (const t of topics) api.touch(t);
  };
}

store.extend({
  topics: TOPICS,
  state: () => ({
    t115sel: null,
    t115filter: { text: '', channel: 'all', onlyNotify: false },
  }),
  snapshot(s, { state }) {
    views.clear();
    for (const [k, d] of Object.entries(s.t115 || {})) load(view(Number(k)), d);
    if (state.stations.length && !state.stations.some((x) => x.idx === state.selected)) {
      state.selected = state.stations[0].idx;
    }
  },
  snapshotTopics: ALL,
  scopeTopics: ALL,
  events: {
    frame: on((v, f) => {
      v.frames.push(f); trim(v.frames, LIMIT.frames);
      v.lastT = f.t_sec;
      v.corr.push({ t: f.t_sec, v: f.corr }); trim(v.corr, LIMIT.trend);
    }, TOPICS.frame),
    control: on((v, c) => {
      v.controls.push(c); trim(v.controls, LIMIT.controls);
      bumpHist(v.histType, c.msg_type_name);
      bumpHist(v.histCh, c.channel);
    }, TOPICS.control),
    broadcast_start: on((v, b) => {
      v.current = b;
      addMarker(v, b.start_sec, 'broadcast', `通報開始 #${b.id}`);
    }, TOPICS.windows),
    broadcast_end: on((v, b) => {
      v.current = null;
      v.broadcasts.push(b);
      addMarker(v, b.end_sec, 'broadcast', `通報終了 #${b.id}`);
    }, TOPICS.windows),
    quality: on((v, q) => {
      v.quality = q;
      v.trend.push(trendPoint(v.lastT, q));
      trim(v.trend, LIMIT.trend);
    }, TOPICS.quality),
    signal: on((v, s) => {
      v.signal = s;
      v.evm.push({ t: s.t_sec, v: s.evm }); trim(v.evm, LIMIT.trend);
      v.cfo.push({ t: s.t_sec, v: s.cfo_hz }); trim(v.cfo, LIMIT.trend);
    }, TOPICS.signal),
    constellation: on((v, c) => {
      v.constellation = c;
      v.constellations.push(c);
      trim(v.constellations, LIMIT.constellations);
    }, TOPICS.constellation),
    scramble: on((v, sc) => { v.scramble = sc; }, TOPICS.scramble),
  },
  selected(idx) {
    fetch(`api/select?idx=${encodeURIComponent(idx)}`, { method: 'POST' }).catch(() => {});
  },
  audio: {
    rate: 16000,
    jitterSec: 0.6,
    maxBufferSec: 3.2,
    rows: [{ id: 'au-t115-bc', label: '通報' }, { id: 'au-t115-sf', label: '音声SF' }],
    draw(st) {
      if (!st) return {};
      const v = view(st.idx);
      const q = v.quality || {};
      return {
        'au-t115-bc': v.current ? `#${v.current.id} ${v.current.target}` : '待機',
        'au-t115-sf': `${q.voice_superframes || 0} 個 / 組立不能 ${q.voice_dropped || 0} / 補間 ${q.voice_filled || 0} 枚`,
      };
    },
  },
  methods: {
    t115(idx) { return view(idx ?? store.state.selected); },
    filteredControls() {
      const f = store.state.t115filter;
      const text = f.text.trim().toLowerCase();
      return view(store.state.selected).controls.filter((c) => {
        if (f.channel !== 'all' && c.channel !== f.channel) return false;
        if (f.onlyNotify && !c.notify) return false;
        if (!text) return true;
        return (c.msg_type_name || '').toLowerCase().includes(text)
          || (c.raw_hex || '').toLowerCase().includes(text)
          || (c.summary || '').toLowerCase().includes(text);
      });
    },
    selectedMessage() {
      const items = store.filteredControls();
      const key = store.state.t115sel;
      if (key) {
        const hit = items.find((c) => keyOf(c) === key);
        if (hit) return hit;
      }
      return items[items.length - 1] || null;
    },
    selectMessage(key) { store.state.t115sel = key; store.touch(TOPICS.insel); },
    setInsFilter(patch) { Object.assign(store.state.t115filter, patch); store.touch(TOPICS.insfilter); },
    pinScramble(v) {
      return fetch(`ch/${encodeURIComponent(store.state.selected)}/api/scramble?value=${encodeURIComponent(v)}`,
        { method: 'POST' });
    },
  },
});
