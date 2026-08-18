import { TOPIC } from '../store.js';
import { UI_TICK_MS } from '../ui.js';

const JITTER = 0.6;
const MAX_BUFFER = 4.0;
const RECONNECT_MS = 1000;
const VOL_MAX = 300;
const VOL_DEFAULT = 100;
const LEVEL_DECAY = 0.6;

export function init(store) {
  const el = document.getElementById('audio-body');
  const sub = document.getElementById('audio-sub');
  el.innerHTML = `
    <p style="margin:0 0 6px">
      <button class="btn" id="au-btn">再生開始</button>
      <button class="btn" id="au-mute" title="ミュート">🔇</button>
      <span id="au-state" class="badge idle">停止</span></p>
    <div class="vol">
      <input type="range" id="au-vol" min="0" max="${VOL_MAX}" step="5"
             value="${VOL_DEFAULT}" aria-label="音量" title="音量（100% 超は歪むことがあります）">
      <span class="volv" id="au-volv">${VOL_DEFAULT}%</span>
    </div>
    <div class="meter" title="出力レベル"><i id="au-level"></i></div>
    <dl class="kv" id="au-kv"></dl>
    <p class="muted tiny" style="margin:6px 0 0">
      16kHz モノラル。ジッタバッファ ${JITTER} 秒ぶん遅れて鳴ります。<br>
      ブラウザの制限で、最初の再生はクリックが必要です。</p>`;
  const btn = document.getElementById('au-btn');
  const muteBtn = document.getElementById('au-mute');
  const stateEl = document.getElementById('au-state');
  const kv = document.getElementById('au-kv');
  const volEl = document.getElementById('au-vol');
  const volVal = document.getElementById('au-volv');
  const levelEl = document.getElementById('au-level');

  let ctx = null, gain = null;
  let volume = VOL_DEFAULT / 100, muted = false, level = 0;
  let playhead = 0;
  let scheduled = [];
  let received = 0;
  let dropped = 0;
  let running = false;

  const rate = () => (store.state.meta && store.state.meta.audioRate) || 16000;

  function applyGain() {
    if (gain) gain.gain.value = muted ? 0 : volume;
    volVal.textContent = `${Math.round(volume * 100)}%`;
    volVal.className = 'volv' + (volume > 1 ? ' hot' : '');
    muteBtn.classList.toggle('sel', muted);
  }

  volEl.addEventListener('input', () => {
    volume = Number(volEl.value) / 100;
    if (muted && volume > 0) muted = false;
    applyGain();
  });
  muteBtn.addEventListener('click', () => { muted = !muted; applyGain(); });

  function update() {
    const buf = ctx ? Math.max(0, playhead - ctx.currentTime) : 0;
    level *= LEVEL_DECAY;
    if (level < 1e-4) level = 0;
    levelEl.style.width = `${Math.min(100, Math.round(level * 100))}%`;
    levelEl.className = level > 0.98 ? 'clip' : '';
    kv.innerHTML = `
      <dt>受信</dt><dd>${(received / rate()).toFixed(1)} 秒</dd>
      <dt>バッファ</dt><dd>${buf.toFixed(2)} 秒（ジッタ ${JITTER} / 上限 ${MAX_BUFFER}）</dd>
      <dt>追いつき破棄</dt><dd>${dropped.toFixed(2)} 秒</dd>`;
    if (sub) {
      const cur = store.state.current;
      sub.textContent = running
        ? (cur ? `通報 #${cur.id} 再生中` : '再生中')
        : '停止中';
    }
    store.setAudioStats({ received, bufferSec: buf, dropped, playing: running });
  }

  btn.onclick = async () => {
    if (!ctx) {
      ctx = new (window.AudioContext || window.webkitAudioContext)({ sampleRate: rate() });
      gain = ctx.createGain();
      gain.connect(ctx.destination);
      applyGain();
    }
    await ctx.resume();
    running = true;
    playhead = ctx.currentTime + JITTER;
    stateEl.textContent = '再生中';
    stateEl.className = 'badge ok';
    btn.disabled = true;
    update();
  };

  function push(arrayBuf) {
    const view = new DataView(arrayBuf);
    if (view.byteLength <= 8) return;
    const n = (view.byteLength - 8) / 2;
    received += n;
    let peak = 0;
    for (let i = 0; i < n; i += 8) {
      const a = Math.abs(view.getInt16(8 + 2 * i, true)) / 32768;
      if (a > peak) peak = a;
    }
    const shown = peak * (muted ? 0 : volume);
    if (shown > level) level = shown;
    if (!running || !ctx) return;

    const now = ctx.currentTime;
    if (playhead < now + 0.05) playhead = now + JITTER;

    if (playhead - now > MAX_BUFFER) {
      let cut = 0;
      scheduled = scheduled.filter((s) => {
        if (s.at > now + 0.05) { try { s.src.stop(); } catch (_) {} cut += s.dur; return false; }
        return true;
      });
      dropped += cut;
      playhead = now + 0.05;
    }

    const buf = ctx.createBuffer(1, n, rate());
    const ch = buf.getChannelData(0);
    for (let i = 0; i < n; i++) ch[i] = view.getInt16(8 + 2 * i, true) / 32768;
    const src = ctx.createBufferSource();
    src.buffer = buf;
    src.connect(gain || ctx.destination);
    const at = playhead;
    src.start(at);
    const dur = n / rate();
    playhead += dur;
    scheduled.push({ src, at, dur });
    scheduled = scheduled.filter((s) => s.at + s.dur > now - 1);
  }

  function connect() {
    const proto = location.protocol === 'https:' ? 'wss' : 'ws';
    const ws = new WebSocket(`${proto}://${location.host}/ws/audio`);
    ws.binaryType = 'arraybuffer';
    ws.onmessage = (m) => push(m.data);
    ws.onclose = () => setTimeout(connect, RECONNECT_MS);
    ws.onerror = () => ws.close();
  }

  store.on(TOPIC.meta, update);
  store.on(TOPIC.windows, update);
  setInterval(update, UI_TICK_MS);
  applyGain();
  connect();
  update();
}
