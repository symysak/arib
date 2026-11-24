import { store } from './store.js';

const LIMIT = { l1: 2000, l2: 500, l3: 500, calls: 400, constellations: 120 };

const TOPICS = {
  t79: 't79.carrier', l1: 'l1', l2: 'l2', l3: 'l3',
  calls: 't79.calls', net: 't79.net', constellation: 'constellation',
};

const views = new Map();

function putByNo(arr, r, trim, limit) {
  for (let i = arr.length - 1; i >= 0 && i >= arr.length - 200; i--) {
    if (arr[i].no === r.no) { arr[i] = r; return; }
  }
  arr.push(r);
  trim(arr, limit);
}

store.extend({
  topics: TOPICS,
  state: () => ({ l1: [], l2: [], l3: [], calls: [], nets: new Map(), constellations: [] }),
  events: {
    t79(ev, api) {
      const d = ev.data || {};
      views.set(ev.idx, { carrier: d.carrier || null });
      if (ev.idx === api.state.selected) api.touch(TOPICS.t79);
    },
    l1(ev, { state, touch, trim }) { putByNo(state.l1, ev.l1, trim, LIMIT.l1); touch(TOPICS.l1); },
    l2(ev, { state, touch, trim }) { putByNo(state.l2, ev.l2, trim, LIMIT.l2); touch(TOPICS.l2); },
    l3(ev, { state, touch, trim }) { putByNo(state.l3, ev.l3, trim, LIMIT.l3); touch(TOPICS.l3); },
    t79call(ev, { state, touch, trim }) {
      state.calls.push(ev.call);
      trim(state.calls, LIMIT.calls);
      touch(TOPICS.calls);
    },
    t79net(ev, { state, touch }) { state.nets.set(ev.net.idx, ev.net); touch(TOPICS.net); },
    t79const(ev, { state, touch, trim }) {
      state.constellations.push(ev.const);
      trim(state.constellations, LIMIT.constellations);
      touch(TOPICS.constellation);
    },
  },
  snapshot(s, { state }) {
    views.clear();
    for (const [k, c] of Object.entries(s.t79 || {})) views.set(Number(k), { carrier: c.carrier || null });
    state.l1 = s.l1 || [];
    state.l2 = s.l2 || [];
    state.l3 = s.l3 || [];
    state.calls = s.calls || [];
    state.nets = new Map((s.nets || []).map((n) => [n.idx, n]));
  },
  snapshotTopics: Object.values(TOPICS),
  scopeTopics: [TOPICS.t79, TOPICS.l1, TOPICS.l2, TOPICS.l3, TOPICS.calls, TOPICS.net],
  trend: (st) => ({
    swPct: st.sw_pct || 0, phase: st.phase_err_deg || 0, cfo: st.cfo_hz || 0, cacPct: st.cac_pct || 0,
    slots: (st.slots || []).slice(), synced: !!st.synced,
  }),
  select(_idx, { state }) { state.constellations.length = 0; },
  selected(idx) {
    fetch(`api/select?idx=${encodeURIComponent(idx)}`, { method: 'POST' }).catch(() => {});
  },
  audio: {
    rate: 8000,
    rows: [{ id: 'au-t79-codec', label: '符号器' }, { id: 'au-t79-call', label: '通話' }],
    draw(st) {
      if (!st) return {};
      const calls = (st.slots || []).map((k, n) => (k.startsWith('通話') ? `ST#${n} ${st.peers[n] || ''}`.trim() : ''))
        .filter(Boolean);
      return { 'au-t79-codec': st.codec || '未決定', 'au-t79-call': calls.join('・') || '-' };
    },
  },
  methods: {
    t79() { return views.get(store.state.selected) || { carrier: null }; },
  },
});
