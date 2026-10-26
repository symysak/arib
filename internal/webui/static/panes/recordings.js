import { TOPIC } from '../store.js';
import { hms, dur } from '../time.js';
import { el, setText, setClass, syncList } from '../dom.js';
import { KIND, recURL } from '../rec.js';

export function init(store) {
  const body = document.getElementById('activity-body');
  const sub = document.getElementById('activity-sub');
  const list = el('div', { class: 'scroll tall' });
  const empty = el('div', { class: 'muted' });
  body.append(list, empty);

  function makeRow() {
    const row = el('div', { class: 'row' });
    const t = el('span', { class: 'muted tiny' });
    const freq = el('span', { class: 'mono' });
    const badge = el('span', { class: 'badge idle' });
    const pills = el('span', { class: 'row inline' });
    const text = el('span', { class: 'grow tiny muted' });
    row.append(t, freq, badge, pills, text);
    row._parts = { t, freq, badge, pills, text };
    return row;
  }

  function draw(s) {
    const items = s.recs.filter((r) => store.inScope(r.idx)).slice().reverse();
    setText(sub, `${items.length} 本`);
    syncList(list, items, (r) => `r/${r.base}`, makeRow, (node, r) => {
      const p = node._parts;
      const st = store.station(r.idx);
      setText(p.t, hms(r.t_sec));
      setText(p.freq, st ? st.mhz.toFixed(4) : '?');
      setClass(p.badge, `badge ${r.open ? 'live' : 'warn'}`);
      setText(p.badge, r.open ? '録音中' : '録音');
      p.pills.replaceChildren();
      if (!r.open) {
        p.pills.append(
          r.iq_removed
            ? el('span', { class: 'pill', text: 'I/Q 削除済' })
            : el('a', { class: 'btn', href: recURL('iq', r.name, KIND.iq), text: 'I/Q' }),
          el('a', { class: 'btn', href: recURL('iq', r.name, KIND.text), text: '記録' }),
        );
        if (r.audio) p.pills.append(el('a', { class: 'btn', href: recURL('audio', r.name, KIND.audio), text: '音声' }));
      }
      setText(p.text, (r.iq_removed ? 'I/Q 削除済　' : '') + (r.reason || r.name)
        + (r.sec ? `　${dur(r.sec)}` : ''));
    });
    setText(empty, items.length ? '' : 'まだ録音はありません');
    empty.style.display = items.length ? 'none' : '';
  }

  store.on(TOPIC.activity, draw);
  store.on(TOPIC.selection, draw);
  draw(store.state);
}
