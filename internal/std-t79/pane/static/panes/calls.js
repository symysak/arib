import { hms } from '../time.js';
import { el, setText, setClass, syncList } from '../dom.js';

const COLS = ['時刻', '局', '団体', 'ST', '種別', '通信種別', '内容', '指定先', 'スクランブル', '呼番号', '発 → 着', '移動局（MSI）', '符号器'];
const KIND_CLASS = (k) => (k.includes('無線チャネル指定') ? 'warn' : k.startsWith('CC') ? 'ok' : k.includes('ショートメッセージ') ? 'info'
  : k.includes('同期バースト') ? 'info' : k.startsWith('MM') ? 'idle' : 'idle');

export function init(store) {
  const body = document.getElementById('calls-body');
  const sub = document.getElementById('calls-sub');
  const bar = document.getElementById('calls-toolbar');
  const onlySel = el('label', { class: 'btn' });
  const onlyBox = el('input', { type: 'checkbox' });
  onlySel.append(onlyBox, el('span', { text: ' 選択局だけ' }));
  const filter = el('input', { type: 'search', placeholder: '絞り込み（例 単信・1234・周波数）' });
  bar.append(onlySel, filter);
  const tbody = el('tbody');
  body.appendChild(el('div', { class: 'hscroll scroll tall' }, [el('table', { class: 'stations' }, [
    el('thead', {}, [el('tr', {}, COLS.map((c) => el('th', { text: c })))]), tbody])]));
  const empty = el('div', { class: 'muted' });
  body.appendChild(empty);

  function makeRow() {
    const tr = el('tr');
    const cells = COLS.map(() => { const d = el('td', { class: 'left tiny' }); tr.appendChild(d); return d; });
    const badge = el('span', { class: 'badge idle' });
    cells[4].appendChild(badge);
    tr._p = { cells, badge };
    return tr;
  }

  function draw() {
    const s = store.state;
    const q = filter.value.trim().toLowerCase();
    const items = s.calls.filter((r) => (!onlyBox.checked || r.idx === s.selected)
      && (!q || JSON.stringify(r).toLowerCase().includes(q))).slice(-300).reverse();
    setText(sub, `${items.length} 件`);
    syncList(tbody, items, (r) => `c/${r.no}`, makeRow, (tr, r) => {
      const c = tr._p.cells;
      const st = store.station(r.idx);
      tr.dataset.mhz = r.mhz || '';
      setText(c[0], hms(r.t_sec));
      setText(c[1], st ? st.mhz.toFixed(4) : '?');
      setText(c[2], r.org || '-');
      setText(c[3], `ST#${r.slot} ${r.channel}`);
      setText(tr._p.badge, r.kind);
      setClass(tr._p.badge, `badge ${KIND_CLASS(r.kind)}`);
      setText(c[5], r.mode || '-');
      setText(c[6], r.text + (r.display ? `　表示「${r.display}」` : '') + (r.cause ? `　理由 ${r.cause}` : ''));
      setText(c[7], r.mhz ? `${r.mhz.toFixed(4)} MHz${r.to_slot >= 0 ? ` ST#${r.to_slot}` : ''}` : '-');
      setText(c[8], r.scramble || '-');
      setText(c[9], r.ref >= 0 ? String(r.ref) : (r.id >= 0 ? `ID ${r.id}` : '-'));
      setText(c[10], r.calling || r.called ? `${r.calling || '?'} → ${r.called || '?'}` : '-');
      setText(c[11], r.ms || r.ms_org || '-');
      setText(c[12], r.codec || '-');
    });
    setText(empty, items.length ? '' : 'まだ呼の出来事はありません（無線チャネル指定・呼設定・ページング・同期バーストがここに並ぶ）');
    empty.style.display = items.length ? 'none' : '';
  }

  tbody.addEventListener('click', (e) => {
    const tr = e.target.closest('tr');
    const mhz = tr && Number(tr.dataset.mhz);
    if (!mhz) return;
    const st = store.state.stations.find((x) => Math.abs(x.mhz - mhz) < 0.0125);
    if (st) store.select(st.idx);
  });
  onlyBox.addEventListener('change', () => { onlySel.classList.toggle('sel', onlyBox.checked); draw(); });
  filter.addEventListener('input', draw);
  store.on('t79.calls', draw);
  store.on('selection', draw);
  draw();
}
