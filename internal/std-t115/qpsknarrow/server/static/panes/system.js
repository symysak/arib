import { dur } from '../time.js';
import { TOPIC } from '../store.js';
import { UI_TICK_MS } from '../ui.js';

export function init(store) {
  const body = document.getElementById('system-body');
  const sub = document.getElementById('system-sub');
  const render = (st) => {
    const s = st || store.state;
    const m = s.meta;
    if (!m) { body.innerHTML = '<p class="muted">接続待ち…</p>'; return; }
    const q = s.quality || {};
    const a = s.audio;
    sub.textContent = s.finished ? '終端' : (m.network ? 'TCP 受信中' : 'ファイル再生');
    body.innerHTML = `
      <div class="row">
        <dl class="kv grow">
          <dt>方式</dt><dd>${m.system}</dd>
          <dt>入力</dt><dd>${m.sourceDesc || m.source}</dd>
          <dt>種別</dt><dd>${m.network
            ? 'ネットワーク（取りこぼし許容）'
            : 'ファイル（背圧で取りこぼさない）'}</dd>
          <dt>長さ</dt><dd>${m.durationSec ? dur(m.durationSec) : '不明（ストリーム）'}</dd>
          <dt>オフセット</dt><dd>${(m.offsetHz / 1000).toFixed(1)} kHz</dd>
        </dl>
        <dl class="kv grow">
          <dt>処理速度</dt><dd>${(q.throughput || 0).toFixed(1)} × 実時間</dd>
          <dt>取りこぼし</dt><dd>${q.overflows
            ? `<span class="pill bad">${q.overflows} 回</span> 復号が追いつかず入力を捨てた`
            : (m.network ? '0 回' : '0 回（ファイルは背圧で待たせる）')}</dd>
          <dt>経過</dt><dd>${dur(q.elapsed_sec || 0)}</dd>
          <dt>音声デコーダ</dt><dd>${m.audioAvailable
            ? `AMR-WB+ 利用可（${m.audioRate} Hz）`
            : '<span class="pill bad">未ビルド</span> scripts/std-t115/build_amrwbplus.sh'}</dd>
          <dt>音声SF</dt><dd>${q.voice_superframes || 0} 個 / 組立不能 ${q.voice_dropped || 0}</dd>
          <dt>復号音声</dt><dd>${(q.audio_sec || 0).toFixed(1)} 秒</dd>
          <dt>受信PCM</dt><dd>${(a.received / (a.rate || 16000)).toFixed(1)} 秒
            （バッファ ${a.bufferSec.toFixed(2)}s / 破棄 ${a.dropped.toFixed(2)}s）</dd>
          <dt>記録</dt><dd>${m.logDir
            ? `${m.logDir}/ ${m.iqRate
                ? `（I/Q 録音 ${(m.iqRate / 1000).toFixed(1)} kHz つき）`
                : '（I/Q 録音なし）'}`
            : '保存しない'}</dd>
        </dl>
      </div>`;
  };
  for (const t of [TOPIC.meta, TOPIC.quality, TOPIC.audio, TOPIC.conn]) store.on(t, render);
  render();
  setInterval(render, UI_TICK_MS);
}
