import { store } from './store.js';

const LIMIT = { frames: 2000, msgs: 500, txs: 400, eyes: 40 };

export const cfg = {
  laneCols: [],
  txCols: [],
  txSearch: () => '',
  focusHead: (tx, st) => ({ big: st ? `${st.mhz.toFixed(4)} MHz` : '?', sub: tx.kind }),
  focusRows: () => [],
  hideQuiet: false,
};
export function configure(c) { Object.assign(cfg, c); }

export function cell(e, v, base) {
  const o = v && typeof v === 'object' ? v : { text: v };
  const text = o.text === undefined || o.text === null ? '' : String(o.text);
  if (e.textContent !== text) e.textContent = text;
  const cls = [base, o.cls].filter(Boolean).join(' ');
  if (e.className !== cls) e.className = cls;
  const title = o.title || '';
  if (e.title !== title) e.title = title;
}

function put(a, r, same) {
  for (let i = a.length - 1; i >= 0 && i >= a.length - 200; i--) {
    if (same(a[i])) { a[i] = r; return; }
  }
  a.push(r);
}

store.extend({
  topics: {
    nxdnframes: 'nxdnframes',
    nxdnmsgs: 'nxdnmsgs',
    nxdntxs: 'nxdntxs',
    nxdnsignal: 'nxdnsignal',
    nxdneye: 'nxdneye',
    nxdnfocus: 'nxdnfocus',
  },
  state: () => ({ nxdnframes: [], nxdnmsgs: [], nxdntxs: [], nxdnsignals: {}, nxdneyes: [], nxdnfocus: null }),
  snapshot(s, { state }) {
    state.nxdnframes = s.nxdnframes || [];
    state.nxdnmsgs = s.nxdnmsgs || [];
    state.nxdntxs = s.nxdntxs || [];
    state.nxdnsignals = s.nxdnsignals || {};
  },
  snapshotTopics: ['nxdnframes', 'nxdnmsgs', 'nxdntxs', 'nxdnsignal'],
  scopeTopics: ['nxdnframes', 'nxdnmsgs', 'nxdntxs', 'nxdnsignal', 'nxdneye', 'nxdnfocus'],
  events: {
    nxdnframe(ev, { state, touch, trim }) {
      put(state.nxdnframes, ev.frame, (x) => x.no === ev.frame.no);
      trim(state.nxdnframes, LIMIT.frames);
      touch('nxdnframes');
    },
    nxdnmsg(ev, { state, touch, trim }) {
      put(state.nxdnmsgs, ev.msg, (x) => x.no === ev.msg.no);
      trim(state.nxdnmsgs, LIMIT.msgs);
      touch('nxdnmsgs');
    },
    nxdntx(ev, { state, touch, trim }) {
      put(state.nxdntxs, ev.tx, (x) => x.idx === ev.tx.idx && x.id === ev.tx.id);
      trim(state.nxdntxs, LIMIT.txs);
      touch('nxdntxs');
      touch('activity');
    },
    nxdnsignal(ev, { state, touch }) {
      state.nxdnsignals[String(ev.idx)] = ev.signal;
      touch('nxdnsignal');
    },
    nxdneye(ev, { state, touch, trim }) {
      if (ev.idx !== state.selected) return;
      state.nxdneyes.push(ev.eye);
      trim(state.nxdneyes, LIMIT.eyes);
      touch('nxdneye');
    },
  },
  trend: (st) => ({
    frames: st.frames || 0, fswOK: st.fsw_ok || 0, richOK: st.rich_ok || 0, crcOK: st.crc_ok || 0, crcAll: st.crc_all || 0,
    dev: st.dev_hz || 0, dc: st.dc_hz || 0, evm: st.evm || 0, synced: !!st.synced,
    voice: st.voice || 0, voiceOK: st.voice_ok || 0,
  }),
  select(idx, { state }) {
    state.nxdneyes = [];
    if (state.nxdnfocus && state.nxdnfocus.idx !== idx) state.nxdnfocus = null;
  },
  methods: {
    pickStation(idx) {
      store.clearFocus();
      store.pick(idx);
    },
    focusTx(tx) {
      if (!tx) { store.clearFocus(); return; }
      store.state.nxdnfocus = { idx: tx.idx, id: tx.id };
      if (store.state.selected !== tx.idx) store.select(tx.idx);
      else store.touch('nxdnfocus');
      for (const t of ['nxdnframes', 'nxdnmsgs', 'nxdntxs']) store.touch(t);
    },
    clearFocus() {
      if (!store.state.nxdnfocus) return;
      store.state.nxdnfocus = null;
      for (const t of ['nxdnfocus', 'nxdnframes', 'nxdnmsgs', 'nxdntxs']) store.touch(t);
    },
    focusedTx() {
      const f = store.state.nxdnfocus;
      return f ? (store.state.nxdntxs.find((t) => t.idx === f.idx && t.id === f.id) || null) : null;
    },
    scopeTxs() { return store.state.nxdntxs.filter((t) => store.inScope(t.idx)); },
    inFocus(rec) {
      const f = store.focusedTx();
      if (!f) return true;
      return rec.idx === f.idx && rec.t_sec >= f.t_sec - 0.05 && (f.open || rec.t_sec <= f.end_sec + 0.05);
    },
  },
  selected(idx) { fetch(`api/select?idx=${idx}`, { method: 'POST' }).catch(() => {}); },
  audio: { rate: 8000 },
});

document.addEventListener('keydown', (e) => {
  if (e.key === 'Escape' && !/^(INPUT|TEXTAREA|SELECT)$/.test((e.target && e.target.tagName) || '')) store.clearFocus();
});
