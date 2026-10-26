import { store } from './store.js';

const LIMIT = {
  calls: 400,
  l1: 2000,
  l2: 500,
  l3: 200,
  callseq: 300,
  constellations: 120,
};

store.extend({
  topics: {
    l1: 'l1',
    l2: 'l2',
    l3: 'l3',
    constellation: 'constellation',
    callseq: 'callseq',
  },
  state: () => ({ calls: [], l1: [], l2: [], l3: [], constellations: [], callseq: [] }),
  snapshot(s, { state }) {
    state.calls = s.calls || [];
    state.l1 = s.l1 || [];
    state.l2 = s.l2 || [];
    state.l3 = s.l3 || [];
    state.callseq = s.callseq || [];
  },
  snapshotTopics: ['l1', 'l2', 'l3', 'callseq'],
  scopeTopics: ['l1', 'l2', 'l3', 'callseq'],
  filterTopics: ['l1', 'l2'],
  events: {
    call(ev, { state, touch, trim }) {
      state.calls.push(ev.call);
      trim(state.calls, LIMIT.calls);
      touch('activity');
    },
    l1(ev, { state, touch, trim }) {
      state.l1.push(ev.l1);
      trim(state.l1, LIMIT.l1);
      touch('l1');
    },
    l2(ev, { state, touch, trim }) {
      const i = state.l2.findIndex((x) => x.no === ev.l2.no);
      if (i >= 0) state.l2[i] = ev.l2; else state.l2.push(ev.l2);
      trim(state.l2, LIMIT.l2);
      touch('l2');
    },
    callseq(ev, { state, touch, trim }) {
      const i = state.callseq.findIndex((x) => x.id === ev.seq.id);
      if (i >= 0) state.callseq[i] = ev.seq; else state.callseq.push(ev.seq);
      trim(state.callseq, LIMIT.callseq);
      touch('callseq');
    },
    l3(ev, { state, touch, trim }) {
      state.l3.push(ev.l3);
      trim(state.l3, LIMIT.l3);
      touch('l3');
    },
    const(ev, { state, touch, trim }) {
      state.constellations.push(ev.const);
      trim(state.constellations, LIMIT.constellations);
      touch('constellation');
    },
  },
  trend: (st) => ({
    richPct: st.frames ? (100 * st.rich_ok_num) / st.frames : 0,
    phase: st.phase_err_deg, cfo: st.cfo_hz, mode: st.mode || '',
    synced: !!st.synced, richOK: !!st.rich_ok, ledger: !!st.ledger,
  }),
  select(_idx, { state }) { state.constellations.length = 0; },
  selected(idx) {
    fetch(`api/select?idx=${encodeURIComponent(idx)}`, { method: 'POST' }).catch(() => {});
  },
  methods: {
    activity() {
      const state = store.state;
      const out = [];
      for (const c of state.calls) {
        if (!store.inScope(c.idx)) continue;
        const last = out[out.length - 1];
        if (last && last.kind === 'call' && last.idx === c.idx
          && last.call.kind === c.kind && last.call.text === c.text && last.call.id === c.id) {
          last.count++;
          last.t = c.t_sec;
          continue;
        }
        out.push({ kind: 'call', key: `c/${c.idx}/${c.t_sec.toFixed(3)}/${c.kind}`, t: c.t_sec, call: c, idx: c.idx, count: 1 });
      }
      for (const r of state.recs) {
        if (!store.inScope(r.idx)) continue;
        out.push({ kind: 'rec', key: `r/${r.base}`, t: r.t_sec, rec: r, idx: r.idx, count: 1 });
      }
      out.sort((a, b) => b.t - a.t);
      return out;
    },
  },
});
