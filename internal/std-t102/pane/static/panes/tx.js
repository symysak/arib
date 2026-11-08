import { hms, dur } from '../time.js';
import { el, setText, setClass, syncList } from '../dom.js';
import { KIND, recURL } from '../rec.js';

export function init(store) {
  const body = document.getElementById('tx-body');
  const sub = document.getElementById('tx-sub');
  const list = el('div', { class: 'scroll tall' });
  const empty = el('div', { class: 'muted tiny', text: '送信はまだありません' });
  body.append(list, empty);

  function makeRow() {
    const row = el('div', { class: 'row pick' });
    const t = el('span', { class: 'muted tiny' });
    const freq = el('span', { class: 'mono' });
    const badge = el('span', { class: 'badge idle' });
    const who = el('span', { class: 'mono tiny' });
    const stat = el('span', { class: 'tiny' });
    const msgs = el('span', { class: 'grow tiny muted' });
    const links = el('span', { class: 'row inline' });
    row.append(t, freq, badge, who, stat, msgs, links);
    row._p = { t, freq, badge, who, stat, msgs, links };
    row.addEventListener('click', () => { if (row._idx !== undefined) store.select(row._idx); });
    return row;
  }

  function draw() {
    const items = store.state.t102txs.slice().reverse();
    setText(sub, `${items.length} 本`);
    empty.style.display = items.length ? 'none' : '';
    syncList(list, items, (x) => `tx/${x.idx}/${x.id}`, makeRow, (node, x) => {
      const p = node._p;
      node._idx = x.idx;
      const st = store.station(x.idx);
      setText(p.t, hms(x.t_sec));
      setText(p.freq, st ? st.mhz.toFixed(4) : '?');
      setClass(p.badge, `badge ${x.open ? 'live' : x.kind === '音声' ? 'warn' : 'info'}`);
      setText(p.badge, `${x.kind}${x.open ? '（送信中）' : ''}`);
      const parts = [];
      if (x.u >= 0) parts.push(`U=${x.u}`);
      if (x.maker >= 0) parts.push(`製造者 ${x.maker}`);
      if (x.d >= 0) parts.push(x.d ? '下り' : '上り/直接');
      if (x.call) parts.push(x.call);
      if (x.name) parts.push(`「${x.name}」`);
      setText(p.who, parts.join(' '));
      const s = [`${x.frames} 枚`];
      if (x.voice) s.push(`音声 ${x.voice}（${x.voice_ok} OK）`);
      if (x.data) s.push(`データ ${x.data}`);
      if (!x.open) s.push(dur(Math.max(0, x.end_sec - x.t_sec)));
      if (x.end) s.push(x.end);
      setText(p.stat, s.join('・'));
      setText(p.msgs, (x.msgs || []).slice(0, 4).join(' / '));
      p.links.replaceChildren();
      if (x.base && !x.open) {
        const rel = (store.state.recs.find((r) => r.base === x.base) || {}).name;
        if (rel) {
          if (x.audio) p.links.append(el('a', { class: 'pill info', href: recURL('audio', rel, KIND.audio), target: '_blank', text: '音声' }));
          p.links.append(el('a', { class: 'pill', href: recURL('iq', rel, KIND.iq), target: '_blank', text: 'I/Q' }));
        }
      }
    });
  }

  store.on('t102txs', draw);
  store.on('stations', draw);
  draw();
}
