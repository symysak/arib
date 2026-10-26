import { el, setText } from '../dom.js';

const SLOTS = [0, 1, 2, 3];
const DB_MIN = -60, DB_MAX = -6, DB_TALK = -45;

export function mount(root, store) {
  const chips = SLOTS.map((n) => {
    const name = el('span', { class: 'mono', text: `ST#${n}` });
    const state = el('span', { class: 'tiny muted', text: '-' });
    const bar = el('i', { style: 'display:block;height:100%;width:0;background:var(--ok)' });
    const meter = el('span', { style: 'display:block;height:4px;margin-top:4px;background:var(--surface-3);overflow:hidden' }, [bar]);
    const b = el('button', { class: 'btn', style: 'flex:1;min-width:0;text-align:left;padding:4px 8px', title: '押すと鳴らす / 止める' },
      [el('span', { style: 'display:flex;justify-content:space-between;gap:6px' }, [name, state]), meter]);
    b.addEventListener('click', () => toggle(n));
    return { b, state, bar };
  });
  const auto = el('input', { type: 'checkbox' });
  const all = el('button', { class: 'btn', text: '全選択' });
  const none = el('button', { class: 'btn', text: '全解除' });
  const summary = el('span', { class: 'tiny muted' });
  root.append(
    el('div', { class: 'row', style: 'gap:6px' }, chips.map((c) => c.b)),
    el('div', { class: 'row', style: 'gap:6px;align-items:center' }, [all, none,
      el('label', { class: 'tiny' }, [auto, ' 自動（先に話し始めた 1 本）']), summary]));

  let cur = null;
  let pending = null;
  let pendingAt = 0;

  function audioOf(st) {
    const a = st && st.audio;
    if (pending && a && a.mode === pending.mode && (a.mode !== 'fixed' || SLOTS.every((n) => !!a.slots[n] === pending.slots[n]))) pending = null;
    if (pending && Date.now() - pendingAt < 2000) return { ...pending, levels: (a && a.levels) || pending.levels };
    return (st && st.audio) || { mode: 'all', slots: [false, false, false, false], now: -1, levels: [-99, -99, -99, -99] };
  }
  function playing(a, n) {
    if (a.mode === 'first') return a.now === n;
    if (a.mode === 'fixed') return !!a.slots[n];
    return true;
  }
  const isControl = (st, n) => (st.slots[n] || '').includes('制御');

  function send(mode, slots) {
    if (!cur) return;
    pending = { ...audioOf(cur), mode, slots: SLOTS.map((n) => slots.includes(n)), now: -1 };
    pendingAt = Date.now();
    fetch(`api/audio-slots?idx=${encodeURIComponent(cur.idx)}&mode=${mode}&slots=${slots.join(',')}`, { method: 'POST' }).catch(() => {});
    update(cur);
  }
  function toggle(n) {
    if (!cur || isControl(cur, n)) return;
    const a = audioOf(cur);
    const on = SLOTS.filter((k) => !isControl(cur, k) && playing(a, k) !== (k === n));
    send('fixed', on);
  }
  all.addEventListener('click', () => send('all', []));
  none.addEventListener('click', () => send('fixed', []));
  auto.addEventListener('change', () => (auto.checked ? send('first', []) : send('all', [])));

  function update(st) {
    cur = st;
    if (!st || !st.slots) {
      for (const c of chips) { c.b.disabled = true; c.b.classList.remove('sel'); setText(c.state, '-'); c.bar.style.width = '0'; }
      setText(summary, '');
      return;
    }
    const a = audioOf(st);
    auto.checked = a.mode === 'first';
    const on = [];
    for (const n of SLOTS) {
      const c = chips[n];
      const ctl = isControl(st, n);
      const inCall = (st.slots[n] || '').startsWith('通話');
      const db = (a.levels || [])[n] ?? -99;
      const play = !ctl && playing(a, n);
      c.b.disabled = ctl;
      c.b.classList.toggle('sel', play);
      c.b.style.opacity = a.mode === 'first' && !play && !ctl ? '.6' : '';
      setText(c.state, ctl ? '制御' : !inCall ? (st.slots[n] || '-') : db >= DB_TALK ? '話し中' : '無音');
      c.bar.style.width = `${Math.round(100 * Math.min(1, Math.max(0, (db - DB_MIN) / (DB_MAX - DB_MIN))))}%`;
      if (play && inCall) on.push(`ST#${n}`);
    }
    setText(summary, a.mode === 'first'
      ? `鳴らす: ${a.now >= 0 ? `ST#${a.now}` : 'なし'}（自動）`
      : `鳴らす: ${on.join('・') || 'なし'}${on.length > 1 ? '（重ねて鳴らす）' : ''}`);
  }
  return update;
}
