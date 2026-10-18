import { TOPIC } from '../store.js';
import { UI_TICK_MS, AUDIO_JITTER_SEC, AUDIO_MAX_BUFFER_SEC } from '../ui.js';

const DEFAULT_RATE = 8000;

const esc = (t) => String(t).replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c]);

export function init(store) {
  const body = document.getElementById('audio-body');
  const sub = document.getElementById('audio-sub');
  const spec = store.audioSpec();
  const RATE = spec.rate || DEFAULT_RATE;
  const JITTER = spec.jitterSec || AUDIO_JITTER_SEC;
  const MAX_BUFFER = spec.maxBufferSec || AUDIO_MAX_BUFFER_SEC;
  const extRows = (spec.rows || []).map((r) => `
      <dt>${esc(r.label)}</dt><dd id="${esc(r.id)}">-</dd>`).join('');
  body.innerHTML = `
    <div class="row">
      <button class="btn" id="au-start">再生開始</button>
      <button class="btn" id="au-mute">ミュート</button>
      <label class="tiny"><input type="checkbox" id="au-auto" checked> 自動再生</label>
      <label class="tiny"><input type="checkbox" id="au-follow"> 自動追従（ヘッダーと同じ）</label>
    </div>
    <div class="row">
      <span class="tiny muted">音量</span>
      <input type="range" id="au-vol" min="0" max="300" value="100" style="flex:1">
      <span class="tiny mono" id="au-volv">100%</span>
    </div>
    <div class="row"><span class="badge idle" id="au-now">再生を開始してください</span></div>
    <div class="row"><span class="tiny muted">聴取中</span><select id="au-ch" class="btn"></select></div>
    <div id="au-ext"></div>
    <canvas id="au-meter" style="width:100%;height:10px;display:block;margin:4px 0"></canvas>
    <dl class="kv">
      <dt>受信</dt><dd id="au-recv">0 秒</dd>
      <dt>バッファ</dt><dd id="au-buf">0.00 秒</dd>
      <dt>追いつき破棄</dt><dd id="au-drop">0</dd>${extRows}
    </dl>`;

  const $ = (id) => document.getElementById(id);
  const extUpdate = spec.mount ? spec.mount($('au-ext'), store) : null;
  let ctx = null, gain = null, listening = -1;
  let receivedSec = 0, dropped = 0, level = 0;
  const streams = new Map();
  function streamOf(idx) {
    let st = streams.get(idx);
    if (!st) { st = { playhead: 0, pending: [] }; streams.set(idx, st); }
    return st;
  }
  function playAll() { return !!store.state.showAll; }
  let wasAll = false;

  function ensureCtx() {
    if (ctx) return ctx;
    ctx = new (window.AudioContext || window.webkitAudioContext)();
    gain = ctx.createGain();
    gain.connect(ctx.destination);
    streams.clear();
    return ctx;
  }

  function stopPendingOf(st) {
    const now = ctx ? ctx.currentTime : 0;
    for (const s of st.pending.splice(0)) {
      if (s.startAt > now) { try { s.node.stop(); } catch {  } }
    }
    st.playhead = 0;
  }
  function stopOthers(keepIdx) {
    for (const [idx, st] of streams) {
      if (idx !== keepIdx) stopPendingOf(st);
    }
  }

  function push(idx, pcm, rate) {
    receivedSec += pcm.length / rate;
    if ((!playAll() && idx !== listening) || !ctx || ctx.state !== 'running') return;
    let peak = 0;
    const buf = ctx.createBuffer(1, pcm.length, rate);
    const ch = buf.getChannelData(0);
    for (let i = 0; i < pcm.length; i++) {
      const v = pcm[i] / 32768;
      ch[i] = v;
      const a = Math.abs(v);
      if (a > peak) peak = a;
    }
    level = Math.max(level, peak);
    const st = streamOf(idx);
    const now = ctx.currentTime;
    if (st.playhead < now + 0.02) st.playhead = now + JITTER;
    if (st.playhead - now > MAX_BUFFER) {
      stopPendingOf(st);
      dropped++;
      st.playhead = now + JITTER;
    }
    const node = ctx.createBufferSource();
    node.buffer = buf;
    node.connect(gain);
    node.start(st.playhead);
    st.pending.push({ node, startAt: st.playhead });
    st.playhead += buf.duration;
    while (st.pending.length && st.pending[0].startAt < now) st.pending.shift();
  }

  function connect() {
    const ws = new WebSocket(new URL('ws/audio', location.href).href.replace(/^http/, 'ws'));
    ws.binaryType = 'arraybuffer';
    ws.onmessage = (e) => {
      const dv = new DataView(e.data);
      if (dv.byteLength < 12) return;
      const idx = dv.getUint16(8, true);
      const rate = dv.getUint16(10, true) || RATE;
      const pcm = new Int16Array(e.data, 12);
      push(idx, pcm, rate);
    };
    ws.onclose = () => setTimeout(connect, 1000);
    ws.onerror = () => ws.close();
  }

  function setListening(idx) {
    if (idx === listening) return;
    listening = idx;
    if (!playAll()) stopOthers(idx);
    store.setAudio({ listening: idx });
    $('au-ch').value = String(idx);
  }

  function fillChannels(s) {
    const sel = $('au-ch');
    const want = s.stations.map((st) => `${st.idx}:${st.mhz.toFixed(4)}`).join(',');
    if (sel.dataset.sig === want) return;
    sel.dataset.sig = want;
    sel.replaceChildren(...s.stations.map((st) => {
      const o = document.createElement('option');
      o.value = String(st.idx);
      o.textContent = `${st.mhz.toFixed(4)} MHz`;
      return o;
    }));
    if (listening >= 0) sel.value = String(listening);
  }

  let armed = false;
  function blocked() { return !ctx || ctx.state !== 'running'; }

  async function start() {
    ensureCtx();
    try { await ctx.resume(); } catch {  }
    if (listening < 0 && store.state.stations.length) {
      const sel = store.station(store.state.selected);
      setListening(sel ? sel.idx : store.state.stations[0].idx);
    }
    if (gain) gain.gain.value = store.state.audio.muted ? 0 : store.state.audio.volume;
    store.setAudio({ playing: !blocked() });
    if (blocked()) arm();
    return !blocked();
  }

  const GESTURES = ['pointerdown', 'keydown', 'touchstart'];
  function arm() {
    if (armed) return;
    armed = true;
    const off = () => GESTURES.forEach((ev) => window.removeEventListener(ev, go, true));
    async function go() {
      off();
      armed = false;
      if (!ctx) return;
      try { await ctx.resume(); } catch {  }
      if (blocked()) { arm(); return; }
      store.setAudio({ playing: true });
    }
    GESTURES.forEach((ev) => window.addEventListener(ev, go, { capture: true, passive: true }));
  }

  const PREF = 'webui.audio.autoplay';
  function prefAuto() {
    try { return localStorage.getItem(PREF) !== '0'; } catch { return true; }
  }
  function setPrefAuto(v) {
    try { localStorage.setItem(PREF, v ? '1' : '0'); } catch {  }
  }

  $('au-auto').checked = prefAuto();
  $('au-auto').addEventListener('change', (e) => {
    setPrefAuto(e.target.checked);
    if (e.target.checked) start();
  });

  $('au-start').addEventListener('click', () => { start(); });
  $('au-mute').addEventListener('click', () => {
    const m = !store.state.audio.muted;
    store.setAudio({ muted: m });
    if (gain) gain.gain.value = m ? 0 : store.state.audio.volume;
    $('au-mute').classList.toggle('sel', m);
  });
  $('au-vol').addEventListener('input', (e) => {
    const v = Number(e.target.value) / 100;
    store.setAudio({ volume: v });
    $('au-volv').textContent = `${e.target.value}%`;
    $('au-volv').className = v > 1 ? 'tiny mono' : 'tiny mono muted';
    if (gain && !store.state.audio.muted) gain.gain.value = v;
  });
  function setFollow(v) {
    store.setAudio({ follow: !!v });
  }
  $('au-follow').addEventListener('change', (e) => setFollow(e.target.checked));
  $('au-ch').addEventListener('change', (e) => {
    setFollow(false);
    store.select(Number(e.target.value));
  });

  function tick() {
    const s = store.state;
    fillChannels(s);
    if (store.station(s.selected) && s.selected !== listening) setListening(s.selected);
    if (!playAll() && wasAll) stopOthers(listening);
    wasAll = playAll();
    $('au-follow').checked = !!s.audio.follow;
    $('au-recv').textContent = `${receivedSec.toFixed(1)} 秒`;
    let ph = 0;
    if (playAll()) { for (const st of streams.values()) ph = Math.max(ph, st.playhead); }
    else if (streams.has(listening)) ph = streams.get(listening).playhead;
    const buf = ctx ? Math.max(0, ph - ctx.currentTime) : 0;
    $('au-buf').textContent = `${buf.toFixed(2)} 秒`;
    $('au-drop').textContent = String(dropped);
    const st = store.station(listening);
    if (spec.draw) {
      const vals = spec.draw(st, s) || {};
      for (const r of spec.rows || []) { const d = $(r.id); if (d) d.textContent = vals[r.id] ?? '-'; }
    }
    if (extUpdate) extUpdate(st, s);
    const on = ctx && ctx.state === 'running';
    sub.textContent = !on ? '停止中' : playAll() ? '全局' : st ? `${st.mhz.toFixed(4)} MHz` : '停止中';
    $('au-start').textContent = on ? '再生中' : '再生開始';
    $('au-start').classList.toggle('sel', !!on);
    const now = $('au-now');
    const talking = store.talkingStation();
    now.textContent = on
      ? (playAll() ? `全局を重ねて再生中（通話中 ${s.stations.filter((x) => x.ext && x.ext.talking).length} 局）`
          : st ? `${st.mhz.toFixed(4)} MHz を再生中${talking && talking.idx === listening ? '（通話中）' : ''}`
          : '局が選ばれていません')
      : armed ? 'ブラウザが自動再生を止めています — 画面のどこかをクリックすると鳴ります'
      : '再生を開始してください';
    now.className = talking && st && talking.idx === listening ? 'badge crit' : 'badge idle';
    const cv = $('au-meter');
    const r = window.devicePixelRatio || 1;
    cv.width = Math.round(cv.clientWidth * r); cv.height = Math.round(10 * r);
    const g = cv.getContext('2d'); g.setTransform(r, 0, 0, r, 0, 0);
    const w = cv.clientWidth;
    g.fillStyle = getComputedStyle(document.documentElement).getPropertyValue('--surface-3') || '#eee';
    g.fillRect(0, 0, w, 10);
    g.fillStyle = level > 0.98
      ? getComputedStyle(document.documentElement).getPropertyValue('--crit')
      : getComputedStyle(document.documentElement).getPropertyValue('--ok');
    g.fillRect(0, 0, w * Math.min(1, level), 10);
    level *= 0.6;
  }

  store.on(TOPIC.stations, () => fillChannels(store.state));
  store.on(TOPIC.selection, () => {
    const s = store.state;
    if (store.station(s.selected) && s.selected !== listening) setListening(s.selected);
  });
  setInterval(tick, UI_TICK_MS);
  connect();
  if (prefAuto()) start();
}
