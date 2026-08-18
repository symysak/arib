import { setOrigin } from './time.js';

const LIMIT = {
  frames: 1200,
  controls: 600,
  logs: 400,
  trend: 600,
  constellations: 150,
  markers: 200,
};

export const TOPIC = {
  conn: 'conn',
  meta: 'meta',
  frame: 'frame',
  control: 'control',
  quality: 'quality',
  signal: 'signal',
  constellation: 'constellation',
  scramble: 'scramble',
  windows: 'windows',
  log: 'log',
  filter: 'filter',
  selection: 'selection',
  audio: 'audio',
};

function initialState() {
  return {
    conn: 'connecting',
    finished: false,

    meta: null,
    quality: null,
    signal: null,
    scramble: null,
    constellation: { t_sec: 0, points: [] },
    constellations: [],
    controls: [],
    frames: [],
    broadcasts: [],
    logs: [],
    current: null,
    lastT: 0,

    audio: { received: 0, bufferSec: 0, dropped: 0, rate: 16000, playing: false },

    trend: [],
    corr: [],
    evm: [],
    cfo: [],
    markers: [],
    histType: new Map(),
    histCh: new Map(),

    selectedKey: null,

    filter: { text: '', channel: 'all', onlyNotify: false },
    logGroups: { control: true, broadcast: true, audio: true, system: true },
  };
}

export function keyOf(c) {
  return `${c.t_sec.toFixed(3)}/${c.raw_hex}`;
}

function createStore() {
  const state = initialState();
  const subs = new Map();
  let pending = new Set();
  let flushQueued = false;

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
  const bumpHist = (map, key) => { if (key) map.set(key, (map.get(key) || 0) + 1); };

  function addMarker(t, kind, text) {
    state.markers.push({ t, kind, text });
    trim(state.markers, LIMIT.markers);
  }

  function trendPoint(t, q) {
    return {
      t,
      cch: q.cch_total > 0 ? q.cch_ok / q.cch_total : null,
      tch: q.tch_total > 0 ? q.tch_ok / q.tch_total : null,
    };
  }

  function apply(e) {
    switch (e.type) {
      case 'snapshot': {
        const d = e.data;
        setOrigin(d.t0_wall_ms, d.t0_estimated);
        state.meta = {
          source: d.source, sourceDesc: d.source_desc, network: d.network,
          sampleRate: d.sample_rate, offsetHz: d.offset_hz,
          durationSec: d.duration_sec, realtime: d.realtime, system: d.system,
          symbolRate: d.symbol_rate, frameBits: d.frame_bits, rollOff: d.roll_off,
          sw1: d.sw1_hex, sw3: d.sw3_hex, scrambleInit: d.scramble_init,
          audioAvailable: d.audio_available, audioRate: d.audio_rate,
          logDir: d.log_dir, logPath: d.log_path, iqRate: d.iq_rate,
          t0Estimated: d.t0_estimated,
        };
        state.audio.rate = d.audio_rate || 16000;
        state.quality = d.quality;
        state.signal = d.signal;
        state.scramble = d.scramble || null;
        if (d.constellation) {
          state.constellation = d.constellation;
          state.constellations = [d.constellation];
        }
        state.controls = d.controls || [];
        state.frames = d.frames || [];
        state.broadcasts = d.broadcasts || [];
        state.logs = d.logs || [];
        state.current = d.current || null;
        state.histType.clear();
        state.histCh.clear();
        for (const c of state.controls) {
          bumpHist(state.histType, c.msg_type_name);
          bumpHist(state.histCh, c.channel);
        }
        state.corr = state.frames.map((f) => ({ t: f.t_sec, v: f.corr }));
        trim(state.corr, LIMIT.trend);
        for (const f of state.frames) state.lastT = Math.max(state.lastT, f.t_sec);
        for (const b of state.broadcasts) {
          addMarker(b.start_sec, 'broadcast', `通報開始 #${b.id}`);
          addMarker(b.end_sec, 'broadcast', `通報終了 #${b.id}`);
        }
        if (state.quality) state.trend.push(trendPoint(state.lastT, state.quality));
        touch(TOPIC.meta); touch(TOPIC.quality); touch(TOPIC.signal);
        touch(TOPIC.constellation); touch(TOPIC.scramble);
        touch(TOPIC.control); touch(TOPIC.windows); touch(TOPIC.frame);
        touch(TOPIC.log); touch(TOPIC.selection);
        break;
      }
      case 'frame': {
        const f = e.data;
        state.frames.push(f); trim(state.frames, LIMIT.frames);
        state.lastT = f.t_sec;
        state.corr.push({ t: f.t_sec, v: f.corr }); trim(state.corr, LIMIT.trend);
        touch(TOPIC.frame);
        break;
      }
      case 'control': {
        const c = e.data;
        state.controls.push(c); trim(state.controls, LIMIT.controls);
        bumpHist(state.histType, c.msg_type_name);
        bumpHist(state.histCh, c.channel);
        touch(TOPIC.control);
        break;
      }
      case 'broadcast_start':
        state.current = e.data;
        addMarker(e.data.start_sec, 'broadcast', `通報開始 #${e.data.id}`);
        touch(TOPIC.windows);
        break;
      case 'broadcast_end':
        state.current = null;
        state.broadcasts.push(e.data);
        addMarker(e.data.end_sec, 'broadcast', `通報終了 #${e.data.id}`);
        touch(TOPIC.windows);
        break;
      case 'quality':
        state.quality = e.data;
        state.trend.push(trendPoint(state.lastT, e.data));
        trim(state.trend, LIMIT.trend);
        touch(TOPIC.quality);
        break;
      case 'signal': {
        const s = e.data;
        state.signal = s;
        state.evm.push({ t: s.t_sec, v: s.evm }); trim(state.evm, LIMIT.trend);
        state.cfo.push({ t: s.t_sec, v: s.cfo_hz }); trim(state.cfo, LIMIT.trend);
        touch(TOPIC.signal);
        break;
      }
      case 'constellation':
        state.constellation = e.data;
        state.constellations.push(e.data);
        trim(state.constellations, LIMIT.constellations);
        touch(TOPIC.constellation);
        break;
      case 'scramble':
        state.scramble = e.data;
        touch(TOPIC.scramble); touch(TOPIC.meta);
        break;
      case 'log': {
        const l = e.data;
        state.logs.push(l); trim(state.logs, LIMIT.logs);
        if (l.level !== 'info') addMarker(l.t_sec, l.level, l.text);
        touch(TOPIC.log);
        break;
      }
      case 'finished':
        state.finished = true;
        touch(TOPIC.conn); touch(TOPIC.log);
        break;
      default: break;
    }
  }

  function filteredControls() {
    const f = state.filter;
    const text = f.text.trim().toLowerCase();
    return state.controls.filter((c) => {
      if (f.channel !== 'all' && c.channel !== f.channel) return false;
      if (f.onlyNotify && !c.notify) return false;
      if (!text) return true;
      return (c.msg_type_name || '').toLowerCase().includes(text)
        || (c.raw_hex || '').toLowerCase().includes(text)
        || (c.summary || '').toLowerCase().includes(text);
    });
  }

  function filteredLogs() {
    const g = state.logGroups;
    return state.logs.filter((l) => {
      const t = l.text;
      if (t.includes('通報')) return g.broadcast;
      if (t.includes('音声')) return g.audio;
      if (t.includes('入力') || t.includes('受信開始') || t.includes('終端')) return g.system;
      return g.control;
    });
  }

  function selectedMessage() {
    const items = filteredControls();
    if (state.selectedKey) {
      const hit = items.find((c) => keyOf(c) === state.selectedKey);
      if (hit) return hit;
    }
    return items[items.length - 1] || null;
  }

  let backoff = 500;
  function connectEvents() {
    const proto = location.protocol === 'https:' ? 'wss' : 'ws';
    const ws = new WebSocket(`${proto}://${location.host}/ws`);
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

  return {
    state, on, apply, filteredControls, filteredLogs, selectedMessage, addMarker,
    connect: connectEvents,
    select(key) { state.selectedKey = key; touch(TOPIC.selection); },
    setAudioStats(s) { Object.assign(state.audio, s); touch(TOPIC.audio); },
    setFilter(patch) { Object.assign(state.filter, patch); touch(TOPIC.filter); },
    pinScramble(v) { return fetch(`/api/scramble?value=${encodeURIComponent(v)}`, { method: 'POST' }); },
    setLogGroup(k, v) { state.logGroups[k] = v; touch(TOPIC.filter); },
  };
}

export const store = createStore();
