import { TOPIC } from '../store.js';
import { UI_TICK_MS } from '../ui.js';
import { el, setText, setClass, syncList } from '../dom.js';

const COLS = ['ST', '同期ワード', '種別', 'SF', 'スクランブル', '通話', '通信種別', '発 → 着', '呼出符号', '位相誤差'];

export function init(store) {
  const body = document.getElementById('carrier-body');
  const sub = document.getElementById('carrier-sub');
  const kv = el('dl', { class: 'kv' });
  const fields = {};
  for (const [k, label] of [['kind', '種別'], ['cc', 'カラーコード'], ['cfo', '搬送波ずれ'], ['sw', '同期ワード'],
    ['cac', 'CAC'], ['sacch', 'SACCH'], ['facch', 'FACCH'], ['sb', '同期バースト'], ['access', 'ランダムアクセス'], ['calls', '通話'], ['codec', '音声の符号器'], ['vendor', '製造者・独自'], ['lock', '捕捉 / 外れ']]) {
    const dd = el('dd', { text: '-' });
    kv.append(el('dt', { text: label }), dd);
    fields[k] = dd;
  }
  const tbody = el('tbody');
  body.append(kv, el('div', { class: 'hscroll' }, [el('table', {}, [
    el('thead', {}, [el('tr', {}, COLS.map((c) => el('th', { text: c })))]), tbody])]));

  function makeRow() {
    const tr = el('tr');
    const cells = COLS.map(() => { const d = el('td'); tr.appendChild(d); return d; });
    const badge = el('span', { class: 'badge idle' });
    cells[2].appendChild(badge);
    tr._parts = { cells, badge };
    return tr;
  }

  const KIND_CLASS = { '音声': 'ok live', 'FACCH': 'warn live', '同期バースト': 'warn', '制御': 'info', '空線': 'idle', '慣性': 'idle', '未同期': 'crit',
    '制御（上り）': 'info', '送信なし': 'idle' };

  function draw() {
    const c = store.t79().carrier;
    if (!c) {
      setText(sub, '');
      syncList(tbody, [], (x) => x.num, makeRow, () => {});
      return;
    }
    setText(sub, c.locked ? '同期中' : '未同期');
    setText(fields.kind, c.kind || '-');
    setText(fields.cc, c.cc >= 0 ? String(c.cc) : '-');
    setText(fields.cfo, `${(c.coarse_hz + c.cfo_hz).toFixed(0)} Hz（粗 ${c.coarse_hz.toFixed(0)} + 残留 ${c.cfo_hz.toFixed(0)}）`);
    setText(fields.sw, `${c.sw_found}/${c.slots_n} スロット`);
    setText(fields.cac, `CRC ${c.cac_ok}/${c.cac_total}`);
    setText(fields.sacch, `CRC ${c.sacch_ok}/${c.sacch_total}`);
    setText(fields.facch, `${c.facch_ok} ユニット`);
    setText(fields.sb, `CRC ${c.sb_ok}/${c.sb_total}`);
    if (c.up_cac) setText(fields.access, `CAC↑ ${c.up_cac}（下りの PE で基地局の受信を確認 ${c.pe_matched || 0}）`);
    else if (c.e_units) setText(fields.access, `送信許可 ${(100 * (c.e_allowed || 0) / c.e_units).toFixed(1)}%・受信 ${c.e_received || 0} 回（上りと PE 一致 ${c.pe_matched || 0}）`);
    else setText(fields.access, '-');
    setText(fields.calls, `${c.calls} 本`);
    setText(fields.codec, c.codec || '未決定');
    setText(fields.vendor, c.vendor || '-');
    setText(fields.lock, `${c.locks} / ${c.losses}${c.overflows ? `（取りこぼし ${c.overflows}）` : ''}`);
    syncList(tbody, c.slots, (x) => x.num, makeRow, (tr, s) => {
      const p = tr._parts;
      setText(p.cells[0], `#${s.num}`);
      setText(p.cells[1], s.sw);
      setText(p.badge, s.kind);
      setClass(p.badge, `badge ${KIND_CLASS[s.kind] || 'idle'}`);
      setText(p.cells[3], s.sf >= 0 ? String(s.sf) : '-');
      setText(p.cells[4], s.scramble);
      setText(p.cells[5], s.call ? `通話中 ${s.voice} 枚` : '-');
      setText(p.cells[6], s.mode || '-');
      p.cells[6].title = s.mode_note || '';
      setText(p.cells[7], s.peer || '-');
      setText(p.cells[8], s.id >= 0 ? String(s.id) : '-');
      setText(p.cells[9], `${(s.phase_err_deg || 0).toFixed(1)}°`);
    });
  }

  store.on(TOPIC.t79, draw);
  store.on(TOPIC.selection, draw);
  setInterval(draw, UI_TICK_MS);
  draw();
}
