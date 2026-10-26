import { hms } from '../time.js';
import { el, setText, setClass, syncList } from '../dom.js';

const COLS = ['周波数', '団体', 'システムコード', '製造者', 'ゾーン / 基地局', 'CC', '制御チャネル', '周辺ゾーン（● 監視中）', '受信', '最終'];

export function init(store) {
  const body = document.getElementById('network-body');
  const sub = document.getElementById('network-sub');
  const tbody = el('tbody');
  const detail = el('dl', { class: 'kv' });
  const msg = el('div', { class: 'tiny muted' });
  body.append(el('div', { class: 'hscroll' }, [el('table', { class: 'stations' }, [
    el('thead', {}, [el('tr', {}, COLS.map((c) => el('th', { text: c })))]), tbody])]), msg, detail);
  let sel = -1;

  const watched = (mhz) => store.state.stations.some((x) => x.watched !== false && Math.abs(x.mhz - mhz) < 0.0125);

  function freqChips(list) {
    return (list || []).map((f) => {
      const on = watched(f);
      const b = el('span', { class: `pill ${on ? 'ok' : ''}`, text: `${on ? '●' : '○'} ${f.toFixed(4)}` });
      if (!on && f > 0) {
        b.style.cursor = 'pointer';
        b.title = 'クリックで監視に加える';
        b.addEventListener('click', async (e) => {
          e.stopPropagation();
          try {
            const res = await fetch('api/engine/channels', { method: 'POST', headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify({ mhz: f }) });
            setText(msg, res.ok ? `${f.toFixed(4)} MHz を監視に加えた` : `加えられない: ${(await res.text()).trim()}`);
          } catch (err) { setText(msg, `加えられない: ${err.message}`); }
        });
      }
      return b;
    });
  }

  function makeRow() {
    const tr = el('tr');
    tr._c = COLS.map(() => { const d = el('td', { class: 'left tiny' }); tr.appendChild(d); return d; });
    return tr;
  }

  function draw() {
    const nets = [...store.state.nets.values()].sort((a, b) => a.mhz - b.mhz);
    setText(sub, `制御キャリア ${nets.length}・団体 ${new Set(nets.map((n) => n.org).filter(Boolean)).size}`);
    syncList(tbody, nets, (n) => `n/${n.idx}`, makeRow, (tr, n) => {
      const c = tr._c;
      tr.dataset.idx = n.idx;
      setClass(tr, n.idx === sel ? 'sel' : '');
      setText(c[0], n.mhz.toFixed(4));
      setText(c[1], n.org || '-');
      setText(c[2], n.system || '-');
      setText(c[3], String(n.maker));
      setText(c[4], `${n.zone} / ${n.base}`);
      setText(c[5], String(n.cc));
      c[6].replaceChildren(...freqChips(n.control));
      c[7].replaceChildren(...freqChips(n.neighbors));
      setText(c[8], `${n.count} 回`);
      setText(c[9], hms(n.t_sec));
    });
    const n = store.state.nets.get(sel) || nets[0];
    detail.replaceChildren();
    if (n) {
      detail.append(el('dt', { text: '報知情報' }), el('dd', { text: `${n.mhz.toFixed(4)} MHz　${n.org || ''}` }));
      for (const r of n.rows || []) detail.append(el('dt', { text: r.n }), el('dd', { text: r.v }));
    }
  }

  tbody.addEventListener('click', (e) => {
    const tr = e.target.closest('tr');
    if (!tr) return;
    sel = Number(tr.dataset.idx);
    store.select(sel);
    draw();
  });
  store.on('t79.net', draw);
  store.on('stations', draw);
  setInterval(draw, 2000);
  draw();
}
