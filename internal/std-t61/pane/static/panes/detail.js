import { TOPIC } from '../store.js';
import { UI_TICK_MS } from '../ui.js';
import { hms } from '../time.js';
import { el, setText, syncList } from '../dom.js';

const KV = [
  ['kind', 'チャネル種別'], ['sf', 'スーパーフレーム内位置'], ['rich', 'RICH'],
  ['richText', '読み下し'], ['richNote', '上位方式の読み'], ['sbNote', '同期バーストの読み'],
  ['frames', 'フレーム'], ['richOK', 'RICH 一致'],
  ['sacch', 'SACCH'], ['facch', 'FACCH'], ['nv2', '非音声2'], ['ctl', '制御ユニット'], ['sb', '同期バースト'],
  ['cfo', '粗捕捉 / 残留 CFO'], ['phase', '差動位相誤差'], ['level', '信号レベル'],
  ['sacchHex', 'SACCH 生'], ['rchHex', 'RCH 生'], ['facchHex', 'FACCH 生'], ['nv2Hex', '非音声2 生'],
  ['ctlHex', '制御 生'], ['ctlE', '衝突制御 E'],
];

function rowsTable() {
  const t = el('table', { class: 'rows' });
  const tb = el('tbody');
  t.appendChild(tb);
  return { t, tb };
}

function fillRows(tb, rows) {
  syncList(tb, rows || [], (r) => r.name,
    () => el('tr', {}, [el('td', { class: 'n' }), el('td', { class: 'b' }), el('td')]),
    (node, r) => {
      setText(node.children[0], r.name);
      setText(node.children[1], r.bits);
      setText(node.children[2], r.text || r.value);
    });
}

export function init(store) {
  const body = document.getElementById('detail-body');
  const sub = document.getElementById('detail-sub');
  const dl = el('dl', { class: 'kv' });
  const v = {};
  for (const [k, name] of KV) {
    dl.appendChild(el('dt', { text: name }));
    v[k] = el('dd');
    dl.appendChild(v[k]);
  }
  const rich = rowsTable();
  const sb = rowsTable();
  const sbHead = el('h3', { class: 'tiny muted', text: '同期バースト' });
  body.append(
    dl,
    el('h3', { class: 'tiny muted', text: 'RICH 17 ビットの展開' }), rich.t,
    sbHead, sb.t,
  );
  v.sacchHex.className = v.rchHex.className = v.facchHex.className = v.nv2Hex.className = v.ctlHex.className = 'mono';
  v.rich.className = 'mono';

  function draw() {
    const st = store.selectedStation();
    if (!st) { setText(sub, '局がありません'); return; }
    const pct = (a, b) => (b ? ((100 * a) / b).toFixed(1) : '0.0');
    setText(sub, `${st.mhz.toFixed(4)} MHz`);
    setText(v.kind, `${st.kind || '-'}${st.uplink ? ' (上り)' : ''} / 同期ワード ${st.sw_name || '-'}`);
    setText(v.sf, st.sf_pos >= 0 ? String(st.sf_pos) : '不明');
    setText(v.rich, st.rich_bits || '-');
    setText(v.richText, st.rich_text || '-');
    setText(v.richNote, st.rich_note || '-');
    setText(v.sbNote, st.sb_note || '-');
    setText(v.frames, `${st.frames}（同期ワード検出 ${st.sw_detected} / 慣性 ${st.flywheel} / 同期外れ ${st.sync_loss}）`);
    setText(v.richOK, `${st.rich_ok_num} / ${st.frames}（${pct(st.rich_ok_num, st.frames)}%）`);
    setText(v.sacch, `${st.sacch_ok} / ${st.sacch_num}（${pct(st.sacch_ok, st.sacch_num)}%）空き枠 ${st.sacch_filler ?? 0}`);
    setText(v.facch, `${st.facch_ok} / ${st.facch_num}（${pct(st.facch_ok, st.facch_num)}%）`);
    setText(v.sb, `${st.sb_ok} / ${st.sb_num}（${pct(st.sb_ok, st.sb_num)}%）`);
    setText(v.nv2, `ユニット ${st.nv2_ok ?? 0}（FACCH と同じ符号・CRC 一致のみ）`);
    setText(v.ctl, st.up_ctl_num
      ? `下り ${st.ctl_ok ?? 0} / ${st.ctl_num ?? 0}　上り ${st.up_ctl_ok} / ${st.up_ctl_num}（上りの空きは全ゼロの充填）`
      : `${st.ctl_ok ?? 0} / ${st.ctl_num ?? 0}`);
    setText(v.cfo, `${st.coarse_hz.toFixed(0)} Hz / ${st.cfo_hz.toFixed(0)} Hz`);
    setText(v.phase, `${st.phase_err_deg.toFixed(1)}°（判定境界 45°）`);
    setText(v.level, `${st.level_db.toFixed(1)} dB（観測ピーク基準）`);
    setText(v.sacchHex, st.sacch_hex || '-');
    setText(v.rchHex, st.rch_raw ? st.rch_raw.toString(2).padStart(8, '0') : '-');
    setText(v.facchHex, st.facch_hex || '-');
    setText(v.nv2Hex, st.nv2_hex || '-');
    setText(v.ctlHex, st.ctl_hex || '-');
    setText(v.ctlE, st.ctl_e || '-');
    fillRows(rich.tb, st.rich_rows);
    setText(sbHead, st.sb_t_sec ? `同期バースト（${hms(st.sb_t_sec)}）` : '同期バースト');
    fillRows(sb.tb, st.sb_rows);
  }

  store.on(TOPIC.selection, draw);
  store.on(TOPIC.stations, draw);
  setInterval(draw, UI_TICK_MS);
}
