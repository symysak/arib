import { store } from './store.js';

const LIMIT = { frames: 2000, msgs: 500, txs: 400, eyes: 40 };

function put(a, r, same) {
  for (let i = a.length - 1; i >= 0 && i >= a.length - 200; i--) {
    if (same(a[i])) { a[i] = r; return; }
  }
  a.push(r);
}

store.extend({
  topics: {
    t102frames: 't102frames',
    t102msgs: 't102msgs',
    t102txs: 't102txs',
    t102signal: 't102signal',
    t102eye: 't102eye',
  },
  state: () => ({ t102frames: [], t102msgs: [], t102txs: [], t102signals: {}, t102eyes: [] }),
  snapshot(s, { state }) {
    state.t102frames = s.t102frames || [];
    state.t102msgs = s.t102msgs || [];
    state.t102txs = s.t102txs || [];
    state.t102signals = s.t102signals || {};
  },
  snapshotTopics: ['t102frames', 't102msgs', 't102txs', 't102signal'],
  scopeTopics: ['t102frames', 't102msgs', 't102txs', 't102signal', 't102eye'],
  events: {
    t102frame(ev, { state, touch, trim }) {
      put(state.t102frames, ev.frame, (x) => x.no === ev.frame.no);
      trim(state.t102frames, LIMIT.frames);
      touch('t102frames');
    },
    t102msg(ev, { state, touch, trim }) {
      put(state.t102msgs, ev.msg, (x) => x.no === ev.msg.no);
      trim(state.t102msgs, LIMIT.msgs);
      touch('t102msgs');
    },
    t102tx(ev, { state, touch, trim }) {
      put(state.t102txs, ev.tx, (x) => x.idx === ev.tx.idx && x.id === ev.tx.id);
      trim(state.t102txs, LIMIT.txs);
      touch('t102txs');
      touch('activity');
    },
    t102signal(ev, { state, touch }) {
      state.t102signals[String(ev.idx)] = ev.signal;
      touch('t102signal');
    },
    t102eye(ev, { state, touch, trim }) {
      if (ev.idx !== state.selected) return;
      state.t102eyes.push(ev.eye);
      trim(state.t102eyes, LIMIT.eyes);
      touch('t102eye');
    },
  },
  trend: (st) => ({
    frames: st.frames || 0, fswOK: st.fsw_ok || 0, richOK: st.rich_ok || 0, crcOK: st.crc_ok || 0, crcAll: st.crc_all || 0,
    dev: st.dev_hz || 0, dc: st.dc_hz || 0, evm: st.evm || 0, synced: !!st.synced,
  }),
  select(idx, { state }) { state.t102eyes = []; },
  selected(idx) { fetch(`api/select?idx=${idx}`, { method: 'POST' }).catch(() => {}); },
  audio: { rate: 8000 },
});
