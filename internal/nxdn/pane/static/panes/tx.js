import { hms, dur } from '../time.js';
import { el, setText, setClass, syncList } from '../dom.js';
import { KIND, recURL } from '../rec.js';
import { cfg, cell } from '../nxdn.js';

const KINDS = [['', '全部'], ['音声', '音声'], ['データ', 'データ']];
const MAX_ROWS = 300;

export function init(store) {
  const body = document.getElementById('tx-body');
  const sub = document.getElementById('tx-sub');
  if (!body) return;
  let kind = '';
  const tools = el('div', { class: 'toolbar' });
  const q = el('input', { type: 'search', placeholder: '絞り込み（周波数・番号・名前など）', class: 'btn', style: 'flex:1;min-width:160px;text-align:left' });
  q.setAttribute('aria-label', '送信の絞り込み');
  tools.append(q);
  const kindBtns = KINDS.map(([k, label]) => {
    const b = el('button', { class: 'btn', type: 'button', text: label });
    b.addEventListener('click', () => { kind = k; draw(); });
    tools.append(b);
    return [k, b];
  });
  const scope = el('span', { class: 'pill info' });
  tools.append(scope);
  const cols = cfg.txCols;
  const names = ['時刻', '周波数', ...cols.map((c) => c.name), '種別', '長さ', '音声', '終わり方', ''];
  const tbody = el('tbody');
  const empty = el('div', { class: 'muted tiny' });
  body.append(tools, el('div', { class: 'scroll tall hscroll' }, [el('table', { class: 'txtable' }, [
    el('thead', {}, [el('tr', {}, names.map((n) => el('th', { text: n })))]), tbody])]), empty);
  q.addEventListener('input', () => draw());

  function makeRow() {
    const tr = el('tr');
    tr._t = el('td', { class: 'mono' });
    tr._f = el('td', { class: 'mono' });
    tr._c = cols.map(() => el('span'));
    tr._k = el('span', { class: 'badge idle' });
    tr._len = el('td', { class: 'mono' });
    tr._v = el('td', { class: 'mono' });
    tr._end = el('td', { class: 'tiny' });
    tr._links = el('td', { class: 'row inline' });
    tr.append(tr._t, tr._f, ...tr._c.map((c) => el('td', {}, [c])), el('td', {}, [tr._k]), tr._len, tr._v, tr._end, tr._links);
    tr.addEventListener('click', (e) => {
      if (e.target.closest('a') || !tr._tx) return;
      const f = store.state.nxdnfocus;
      if (f && f.idx === tr._tx.idx && f.id === tr._tx.id) store.clearFocus();
      else store.focusTx(tr._tx);
    });
    return tr;
  }

  function draw() {
    const s = store.state;
    for (const [k, b] of kindBtns) setClass(b, k === kind ? 'btn sel' : 'btn');
    const all = store.scopeTxs();
    const text = q.value.trim().toLowerCase();
    let items = all;
    if (kind) items = items.filter((x) => (x.kind || '').startsWith(kind));
    if (text) {
      items = items.filter((x) => {
        const st = store.station(x.idx);
        const hay = `${st ? st.mhz.toFixed(4) : ''} ${x.kind} ${cfg.txSearch(x, st)} ${(x.msgs || []).join(' ')}`.toLowerCase();
        return hay.includes(text);
      });
    }
    const st0 = store.selectedStation();
    setText(scope, s.showAll ? '全局' : (st0 ? `${st0.mhz.toFixed(4)} MHz` : '-'));
    setText(sub, items.length === s.nxdntxs.length ? `${items.length} 本` : `${items.length} 本（全局では ${s.nxdntxs.length} 本）`);
    setText(empty, items.length ? '' : (all.length ? '絞り込みに合う送信がありません' : '送信はまだありません'));
    const f = s.nxdnfocus;
    syncList(tbody, items.slice(-MAX_ROWS).reverse(), (x) => `tx/${x.idx}/${x.id}`, makeRow, (tr, x) => {
      tr._tx = x;
      const st = store.station(x.idx);
      setClass(tr, f && f.idx === x.idx && f.id === x.id ? 'sel' : '');
      setText(tr._t, hms(x.t_sec));
      setText(tr._f, st ? st.mhz.toFixed(4) : '?');
      cols.forEach((c, i) => cell(tr._c[i], c.get(x, st), c.cls || ''));
      setClass(tr._k, `badge ${x.open ? 'live' : x.kind === '音声' ? 'warn' : 'info'}`);
      setText(tr._k, `${x.kind}${x.open ? '・送信中' : ''}`);
      setText(tr._len, x.open ? `${x.frames} 枚` : dur(Math.max(0, x.end_sec - x.t_sec)));
      setText(tr._v, x.voice ? `${x.voice}（${Math.round((100 * x.voice_ok) / x.voice)}%）` : '-');
      setText(tr._end, x.open ? '' : (x.end || ''));
      const rel = x.base && !x.open ? (s.recs.find((r) => r.base === x.base) || {}).name : '';
      const want = rel ? `${rel}|${x.audio}` : '';
      if (tr._links.dataset.for !== want) {
        tr._links.dataset.for = want;
        tr._links.replaceChildren();
        if (rel) {
          if (x.audio) tr._links.append(el('a', { class: 'pill info', href: recURL('audio', rel, KIND.audio), target: '_blank', text: '音声' }));
          tr._links.append(el('a', { class: 'pill', href: recURL('iq', rel, KIND.iq), target: '_blank', text: 'I/Q' }));
        }
      }
    });
  }

  store.on('nxdntxs', draw);
  store.on('nxdnfocus', draw);
  store.on('stations', draw);
  store.on('selection', draw);
  store.on('activity', draw);
  draw();
}
