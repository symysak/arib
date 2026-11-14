import { TOPIC } from '../store.js';
import { el, setText } from '../dom.js';
import { UI_TICK_MS } from '../ui.js';
import { hms } from '../time.js';
import { groupOf, cipherText } from '../t98.js';

const ROWS = [
  ['freq', '周波数'],
  ['ch', 'チャネル'],
  ['chnote', 'チャネルの決まり'],
  ['sync', '同期'],
  ['init', 'ホワイトニングの初期値'],
  ['mode', '直近の RICH'],
  ['uc', 'ユーザコード UC'],
  ['cipher', '通話識別・秘話鍵'],
  ['csm', '呼出名称（直近）'],
  ['sb0', '同期バースト（PICH の CRC）'],
  ['maker', '製造者番号'],
  ['codec', '音声の符号器'],
  ['voice', '音声の符号器の検査'],
  ['frames', 'フレーム（同期ワード / RICH）'],
  ['crc', '機能チャネルの CRC'],
  ['dev', '±1 の偏位'],
  ['dc', '直流（搬送波のずれ）'],
  ['evm', 'EVM'],
  ['txs', '送信'],
  ['last', '最後のフレーム'],
];

export function init(store) {
  const body = document.getElementById('detail-body');
  const sub = document.getElementById('detail-sub');
  const dl = el('dl', { class: 'kv' });
  const cells = {};
  for (const [k, name] of ROWS) {
    const dd = el('dd', { class: 'mono tiny' });
    dl.append(el('dt', { text: name }), dd);
    cells[k] = dd;
  }
  body.append(dl);

  function pct(a, b) { return b ? `${((100 * a) / b).toFixed(1)}%` : '-'; }

  function draw() {
    const st = store.selectedStation();
    if (!st) { setText(sub, '-'); for (const k of Object.keys(cells)) setText(cells[k], '-'); return; }
    const ext = st.ext || {};
    const sig = store.state.nxdnsignals[String(st.idx)];
    setText(sub, `${st.mhz.toFixed(4)} MHz`);
    setText(cells.freq, `${st.mhz.toFixed(5)} MHz（オフセット ${(st.offset_hz / 1e3).toFixed(1)} kHz）`);
    const ch = ext.ch;
    setText(cells.ch, ch ? `${ch.class} ${ch.no} ch（${ch.mhz.toFixed(6)} MHz）` : '規格の割当周波数の外');
    setText(cells.chnote, ch && ch.note ? ch.note : '-');
    setText(cells.sync, st.synced ? '送信中（同期）' : (st.watched ? '待機（電波なし）' : '監視終了'));
    setText(cells.init, st.init >= 0 ? `0x${st.init.toString(16).toUpperCase().padStart(3, '0')}（${st.init_why || ''}）` : '-');
    setText(cells.mode, st.mode || '-');
    setText(cells.uc, st.u >= 0 ? String(st.u) : '-');
    const g = st.u >= 0 ? groupOf(st, st.u) : null;
    setText(cells.cipher, g ? cipherText(g.cipher, g.key, g.key_how) : (ext.call || '-'));
    setText(cells.csm, st.caller || '-');
    setText(cells.sb0, ext.sb0 ? `${ext.sb0} 枚（呼出名称が読めた ${ext.pich_ok || 0}）` : '-');
    setText(cells.maker, st.maker > 0 ? String(st.maker) : st.maker === 0 ? '0（共通製造者番号）' : '-');
    setText(cells.codec, st.codec || '-');
    setText(cells.voice, st.voice ? `${st.voice_ok}/${st.voice}（${pct(st.voice_ok, st.voice)}）` : '-');
    setText(cells.frames, `${st.frames}（${pct(st.fsw_ok, st.frames)} / ${pct(st.rich_ok, st.frames)}）`);
    setText(cells.crc, `${st.crc_ok}/${st.crc_all}（${pct(st.crc_ok, st.crc_all)}）`);
    setText(cells.dev, `${(sig ? sig.dev_hz : st.dev_hz).toFixed(0)} Hz（規格 315 Hz）`);
    setText(cells.dc, `${(sig ? sig.dc_hz : st.dc_hz).toFixed(0)} Hz`);
    setText(cells.evm, (sig ? sig.evm : st.evm).toFixed(3));
    setText(cells.txs, `${st.txs} 本${st.overflows ? `・取りこぼし ${st.overflows}` : ''}`);
    setText(cells.last, st.last_t_sec ? hms(st.last_t_sec) : '-');
  }

  store.on(TOPIC.selection, draw);
  store.on(TOPIC.stations, draw);
  setInterval(draw, UI_TICK_MS);
  draw();
}
