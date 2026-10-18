import { TOPIC } from '../store.js';
import { UI_TICK_MS } from '../ui.js';
import { dur } from '../time.js';

export function init(store) {
  const body = document.getElementById('system-body');
  const sub = document.getElementById('system-sub');
  const render = () => {
    const m = store.state.meta;
    if (!m) { body.innerHTML = '<p class="muted">接続待ち…</p>'; return; }
    const v = store.t115();
    const q = v.quality || {};
    const st = store.selectedStation();
    sub.textContent = m.finished ? '終端' : (m.network ? 'ネットワーク受信中' : 'ファイル再生');
    body.innerHTML = `
      <div class="row">
        <dl class="kv grow">
          <dt>方式</dt><dd>${m.system}</dd>
          <dt>入力</dt><dd>${m.input}</dd>
          <dt>種別</dt><dd>${m.network
            ? 'ネットワーク（取りこぼし許容）'
            : 'ファイル（背圧で取りこぼさない）'}</dd>
          <dt>標本化</dt><dd>${(m.sample_rate / 1000).toFixed(1)} kHz · 中心 ${m.center_mhz.toFixed(5)} MHz</dd>
          <dt>局</dt><dd>${st ? `${st.mhz.toFixed(5)} MHz（オフセット ${(st.offset_hz / 1000).toFixed(1)} kHz）` : '-'}</dd>
          <dt>走査</dt><dd>${m.scan ? '有効（-scan）' : '無効'}</dd>
        </dl>
        <dl class="kv grow">
          <dt>処理速度</dt><dd>${(q.throughput || 0).toFixed(1)} × 実時間</dd>
          <dt>取りこぼし</dt><dd>${q.overflows
            ? `<span class="pill bad">${q.overflows} 回</span> 復号が追いつかず入力を捨てた`
            : (m.network ? '0 回' : '0 回（ファイルは背圧で待たせる）')}</dd>
          <dt>経過</dt><dd>${dur(q.elapsed_sec || 0)}</dd>
          <dt>音声デコーダ</dt><dd>${m.audio_available
            ? `AMR-WB+ 利用可（${m.audio_rate} Hz）`
            : '<span class="pill bad">未ビルド</span> scripts/std-t115/build_amrwbplus.sh'}</dd>
          <dt>音声SF</dt><dd>${q.voice_superframes || 0} 個 / 組立不能 ${q.voice_dropped || 0}</dd>
          <dt>復号音声</dt><dd>${(q.audio_sec || 0).toFixed(1)} 秒</dd>
          <dt>記録</dt><dd>${m.log_dir}/ ${m.iq_rate
            ? `（I/Q 録音 ${(m.iq_rate / 1000).toFixed(1)} kHz つき）`
            : '（I/Q 録音なし）'}</dd>
        </dl>
      </div>`;
  };
  for (const t of [TOPIC.meta, TOPIC.quality, TOPIC.selection, TOPIC.conn]) store.on(t, render);
  render();
  setInterval(render, UI_TICK_MS);
}
