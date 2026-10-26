import { setOrigin } from './time.js';

const LIMIT = {
  ext: 1500,
  logs: 600,
  recs: 200,
  trend: 1800,
};

export const TOPIC = {
  conn: 'conn',
  meta: 'meta',
  stations: 'stations',
  selection: 'selection',
  activity: 'activity',
  logs: 'logs',
  audio: 'audio',
  filter: 'filter',
};

function initialState() {
  return {
    conn: 'connecting',
    meta: null,
    stations: [],
    selected: 0,
    showAll: false,
    ext: {},
    recs: [],
    logs: [],
    trend: new Map(),
    audio: {
      follow: false, listening: -1, volume: 1.0, muted: false,
      received: 0, bufferSec: 0, dropped: 0, playing: false,
    },
    filter: { text: '' },
    logGroups: { station: true, call: true, record: true, system: true },
    lastT: 0,
  };
}

function createStore() {
  const state = initialState();
  const subs = new Map();
  let pending = new Set();
  let flushQueued = false;
  const specs = [];
  const events = {};

  function on(topic, fn) {
    if (!subs.has(topic)) subs.set(topic, new Set());
    subs.get(topic).add(fn);
    return () => subs.get(topic).delete(fn);
  }

  function touch(topic) {
    pending.add(topic);
    if (flushQueued) return;
    flushQueued = true;
    requestAnimationFrame(() => {
      flushQueued = false;
      const topics = pending;
      pending = new Set();
      for (const t of topics) {
        const set = subs.get(t);
        if (!set) continue;
        for (const fn of set) {
          try { fn(state); } catch (e) { console.error(`pane[${t}]`, e); }
        }
      }
    });
  }

  const trim = (arr, n) => { if (arr.length > n) arr.splice(0, arr.length - n); };
  const api = { state, touch, trim };

  function pushTrend(st) {
    let a = state.trend.get(st.idx);
    if (!a) { a = []; state.trend.set(st.idx, a); }
    const p = {
      t: st.last_t_sec, level: st.level_db,
      talking: !!st.talking, recording: !!st.recording,
      ext: st.ext || {},
    };
    for (const s of specs) if (s.trend) Object.assign(p, s.trend(st));
    a.push(p);
    trim(a, LIMIT.trend);
  }

  function byFreq(a, b) { return a.mhz - b.mhz || a.idx - b.idx; }

  function applyStation(st) {
    const i = state.stations.findIndex((x) => x.idx === st.idx);
    if (i < 0) state.stations.push(st);
    else state.stations[i] = st;
    state.stations.sort(byFreq);
    pushTrend(st);
    state.lastT = Math.max(state.lastT, st.last_t_sec || 0);
  }

  function apply(ev) {
    switch (ev.type) {
      case 'snapshot': {
        const s = ev.snapshot;
        state.meta = s.meta;
        setOrigin(s.meta.t0_wall_ms, s.meta.t0_estimated);
        state.stations = (s.stations || []).slice().sort(byFreq);
        for (const sp of specs) if (sp.snapshot) sp.snapshot(s, api);
        for (const [k, v] of Object.entries(s.ext || {})) state.ext[k] = v || [];
        state.logs = s.logs || [];
        state.recs = s.recs || [];
        for (const st of state.stations) pushTrend(st);
        touch(TOPIC.meta); touch(TOPIC.stations); touch(TOPIC.activity);
        for (const sp of specs) for (const t of sp.snapshotTopics || []) touch(t);
        touch(TOPIC.logs);
        for (const k of Object.keys(state.ext)) touch(k);
        break;
      }
      case 'station':
        applyStation(ev.station);
        touch(TOPIC.stations);
        break;
      case 'station_gone': {
        const idx = ev.station && ev.station.idx;
        state.stations = state.stations.filter((x) => x.idx !== idx);
        state.trend.delete(idx);
        if (state.meta && Array.isArray(state.meta.channels)) {
          state.meta.channels = state.stations.map((x) => x.mhz).sort((a, b) => a - b);
        }
        if (state.selected === idx && state.stations.length) {
          state.selected = state.stations[0].idx;
          touch(TOPIC.selection);
        }
        touch(TOPIC.stations); touch(TOPIC.meta);
        break;
      }
      case 'rec': {
        const r = ev.rec;
        const hit = state.recs.find((x) => x.base === r.base);
        if (hit) Object.assign(hit, r);
        else state.recs.push(r);
        trim(state.recs, LIMIT.recs);
        touch(TOPIC.activity);
        break;
      }
      case 'meta':
        state.meta = ev.meta;
        touch(TOPIC.meta);
        break;
      case 'log':
        state.logs.push(ev.log);
        trim(state.logs, LIMIT.logs);
        touch(TOPIC.logs);
        break;
      default: {
        const h = events[ev.type];
        if (h) { h(ev, api); break; }
        if (!ev.type) break;
        const a = state.ext[ev.type] || (state.ext[ev.type] = []);
        a.push(ev.data);
        trim(a, LIMIT.ext);
        touch(ev.type);
        break;
      }
    }
  }

  function station(idx) { return state.stations.find((s) => s.idx === idx) || null; }
  function selectedStation() { return station(state.selected); }
  function talkingStation() { return state.stations.find((s) => s.ext && s.ext.talking) || null; }

  function inScope(idx) { return state.showAll || idx < 0 || idx === state.selected; }

  function collapsedLogs() {
    const g = state.logGroups;
    const out = [];
    for (const l of state.logs) {
      if (g[l.group] === false) continue;
      if (!inScope(l.idx ?? -1)) continue;
      const last = out[out.length - 1];
      if (last && last.l.text === l.text && last.l.group === l.group) {
        last.count++;
        last.lastT = l.t_sec;
        continue;
      }
      out.push({ l, count: 1, firstT: l.t_sec, lastT: l.t_sec, key: `${l.t_sec.toFixed(3)}/${l.group}/${l.text}` });
    }
    return out;
  }

  let backoff = 500;
  function connectEvents() {
    const ws = new WebSocket(new URL('ws', location.href).href.replace(/^http/, 'ws'));
    ws.onopen = () => { backoff = 500; state.conn = 'open'; touch(TOPIC.conn); };
    ws.onmessage = (e) => {
      let ev;
      try { ev = JSON.parse(e.data); } catch { return; }
      apply(ev);
    };
    ws.onclose = () => {
      state.conn = 'reconnecting';
      touch(TOPIC.conn);
      setTimeout(connectEvents, backoff = Math.min(backoff * 2, 10000));
    };
    ws.onerror = () => ws.close();
  }

  function touchAll() {
    touch(TOPIC.selection); touch(TOPIC.stations); touch(TOPIC.activity);
    for (const sp of specs) for (const t of sp.scopeTopics || []) touch(t);
    touch(TOPIC.logs);
    for (const k of Object.keys(state.ext)) touch(k);
  }

  const store = {
    state, on, apply, touch,
    station, selectedStation, talkingStation, inScope,
    collapsedLogs,
    connect: connectEvents,
    extend(spec) {
      specs.push(spec);
      Object.assign(TOPIC, spec.topics || {});
      if (spec.state) Object.assign(state, spec.state());
      Object.assign(events, spec.events || {});
      Object.assign(store, spec.methods || {});
    },
    text(key, fallback) {
      let t = fallback;
      for (const s of specs) if (s.texts && s.texts[key] !== undefined) t = s.texts[key];
      return t;
    },
    audioSpec() {
      let a = {};
      for (const s of specs) if (s.audio) a = { ...a, ...s.audio };
      return a;
    },
    select(idx) {
      if (state.selected === idx) return;
      state.selected = idx;
      for (const sp of specs) if (sp.select) sp.select(idx, api);
      touchAll();
      for (const sp of specs) if (sp.selected) sp.selected(idx, api);
    },
    setShowAll(v) { state.showAll = !!v; touchAll(); },
    setAudio(patch) { Object.assign(state.audio, patch); touch(TOPIC.audio); },
    setFilter(patch) {
      Object.assign(state.filter, patch);
      for (const sp of specs) for (const t of sp.filterTopics || []) touch(t);
    },
    setLogGroup(k, v) { state.logGroups[k] = v; touch(TOPIC.logs); },
  };
  return store;
}

export const store = createStore();
