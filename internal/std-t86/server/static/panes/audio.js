
"use strict";

const esc = s => String(s).replace(/[&<>"]/g,
  c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));

const SR = 16000;
const JITTER = 0.6;
const FRAME_MS = 20;

const MAX_BUFFER = 8.0;

const fin = v => (typeof v === "number" && isFinite(v)) ? v : null;
const iv = v => fin(v) === null ? "—" : String(Math.round(v));

function crcBadge(rate) {
  if (rate === null) return "";
  const pct = rate * 100;
  if (pct >= 99) return ` <span class="badge ok">良好</span>`;
  if (pct < 90) return ` <span class="badge crit">不良</span>`;
  if (pct < 95) return ` <span class="badge warn">劣化</span>`;
  return ` <span class="badge idle">やや劣化</span>`;
}

export function init(store) {
  const body = document.getElementById("audio-body");
  const sub = document.getElementById("audio-sub");
  if (!body) return;

  body.innerHTML = `
    <div class="audiobar">
      <button class="btn" data-a="play">▶ 再生開始</button>
      <button class="btn plain" data-a="mute">ミュート</button>
      <input type="range" min="0" max="100" value="80" data-a="vol"
             aria-label="音量" title="音量">
      <span class="mono" data-a="volv">80%</span>
    </div>
    <div class="meter" title="受信 PCM の RMS レベル"><span></span></div>
    <table class="kv" data-a="kv"></table>
    <div class="note" data-a="hint"></div>
    <div data-a="dechead" class="subhead"></div>
    <table class="kv" data-a="deckv"></table>
    <div class="note" data-a="decnote"></div>`;

  const el = k => body.querySelector(`[data-a="${k}"]`);
  const playBtn = el("play"), muteBtn = el("mute");
  const volEl = el("vol"), volVal = el("volv");
  const meter = body.querySelector(".meter span");
  const kv = el("kv"), hint = el("hint");
  const decHead = el("dechead"), decKv = el("deckv"), decNote = el("decnote");

  let ctx = null, gain = null;
  let playhead = 0;
  let volume = 0.8, muted = false;
  let userPaused = false;
  let curWindow = null;
  let lastSeq = null, gaps = 0, chunks = 0, recvSeconds = 0;
  let queued = [];
  let dropSeconds = 0, dropCount = 0;
  let level = 0;
  let wsState = "connecting";

  function ensureCtx() {
    if (!ctx) {
      const AC = window.AudioContext || window.webkitAudioContext;
      ctx = new AC({ sampleRate: SR });
      gain = ctx.createGain();
      gain.gain.value = muted ? 0 : volume;
      gain.connect(ctx.destination);
      playhead = 0;
    }
    if (ctx.state === "suspended" && !userPaused) ctx.resume();
    return ctx;
  }

  for (const name of ["pointerdown", "keydown"]) {
    document.addEventListener(name, () => { if (!userPaused) ensureCtx(); });
  }

  playBtn.addEventListener("click", () => {
    ensureCtx();
    if (ctx.state === "running") {
      userPaused = true;
      ctx.suspend();
    } else {
      userPaused = false;
      for (const q of queued) { try { q.node.stop(); } catch (_) {  } }
      queued = [];
      ctx.resume();
      playhead = 0;
    }
    update();
  });

  muteBtn.addEventListener("click", () => {
    muted = !muted;
    if (gain) gain.gain.value = muted ? 0 : volume;
    update();
  });

  volEl.addEventListener("input", () => {
    volume = Number(volEl.value) / 100;
    if (!muted && gain) gain.gain.value = volume;
    update();
  });

  function connect() {
    const proto = location.protocol === "https:" ? "wss" : "ws";
    const ws = new WebSocket(`${proto}://${location.host}/ws/audio`);
    ws.binaryType = "arraybuffer";
    ws.onopen = () => { wsState = "open"; update(); };
    ws.onmessage = e => onChunk(e.data);
    ws.onclose = () => {
      wsState = "closed";
      update();
      setTimeout(connect, 1000);
    };
    ws.onerror = () => ws.close();
  }

  function onChunk(data) {
    if (!(data instanceof ArrayBuffer) || data.byteLength < 8) return;
    const head = new DataView(data, 0, 8);
    curWindow = head.getUint32(0, true);
    const seq = head.getUint32(4, true);
    if (lastSeq !== null && seq !== lastSeq + 1) gaps += Math.max(0, seq - lastSeq - 1);
    lastSeq = seq;

    const c = ensureCtx();
    const pcm = new Int16Array(data, 8);
    if (!pcm.length) return;
    chunks++;
    recvSeconds += pcm.length / SR;

    let acc = 0;
    for (let i = 0; i < pcm.length; i++) { const v = pcm[i] / 32768; acc += v * v; }
    const rms = Math.sqrt(acc / pcm.length);
    level = Math.max(level, rms);

    if (c.state !== "running") return;
    const buf = c.createBuffer(1, pcm.length, SR);
    const ch = buf.getChannelData(0);
    for (let i = 0; i < pcm.length; i++) ch[i] = pcm[i] / 32768;
    const node = c.createBufferSource();
    node.buffer = buf;
    node.connect(gain);
    const now = c.currentTime;
    queued = queued.filter(q => q.endAt > now);
    if (playhead < now + 0.05) playhead = now + JITTER;
    if (playhead - now > MAX_BUFFER) catchUp(now);
    node.start(playhead);
    queued.push({ node, startAt: playhead, endAt: playhead + buf.duration });
    playhead += buf.duration;
  }

  function catchUp(now) {
    let resume = now + JITTER;
    const keep = [];
    for (const q of queued) {
      if (q.startAt <= now) {
        keep.push(q);
        if (q.endAt > resume) resume = q.endAt;
      } else {
        try { q.node.stop(); } catch (_) {  }
      }
    }
    queued = keep;
    dropSeconds += Math.max(0, playhead - resume);
    dropCount++;
    playhead = resume;
  }

  function liveAudio() {
    const wins = store.state.windows || {};
    if (curWindow && wins[curWindow] && wins[curWindow].audio) {
      return { id: curWindow, w: wins[curWindow], a: wins[curWindow].audio };
    }
    const ids = Object.keys(wins).map(Number).filter(n => !isNaN(n)).sort((a, b) => b - a);
    for (const id of ids) {
      if (wins[id] && wins[id].t_end == null && wins[id].audio) {
        return { id, w: wins[id], a: wins[id].audio };
      }
    }
    for (const id of ids) {
      if (wins[id] && wins[id].audio) return { id, w: wins[id], a: wins[id].audio };
    }
    return null;
  }

  function renderDecode() {
    const la = liveAudio();
    if (!la) {
      decHead.textContent = "デコード統計 — 音声フレーム待ち…";
      decKv.innerHTML = "";
      decNote.textContent = "";
      return;
    }
    const { id, w, a } = la;
    const live = w && w.t_end == null;
    decHead.innerHTML = `デコード統計 — 通報 #${id}`
      + (live ? ` <span class="badge crit live">受信中</span>`
        : ` <span class="badge idle">終了</span>`);

    const frames = fin(a.frames);
    const rate = fin(a.crc7_rate) !== null ? a.crc7_rate
      : (frames ? a.crc7_ok / frames : null);
    const fail = fin(a.crc7_fail) !== null ? a.crc7_fail
      : (frames !== null && fin(a.crc7_ok) !== null ? frames - a.crc7_ok : null);

    const rateCell = rate === null ? "—"
      : `${(rate * 100).toFixed(1)} %${crcBadge(rate)}`
        + ` <span class="votes">${iv(a.crc7_ok)}/${iv(frames)} フレーム</span>`;

    const filled = fin(a.filled) || 0;
    const stale = fin(a.stale) || 0;
    const rep = fin(a.plc_repeat) || 0;
    const mute = fin(a.plc_mute) || 0;

    const rows = [
      ["CRC7 一致率", rateCell],
      ["受信フレーム", `${iv(frames)} <span class="votes">`
        + `不一致 ${iv(fail)} / 20ms 刻み</span>`],
      ["欠落補間", `${filled}`
        + (filled ? ` <span class="pill bad">取りこぼし</span>` : "")
        + ` <span class="votes">取りこぼしスロットを消失フレームとして補間</span>`],
      ["遅延破棄 (stale)", `${stale}`
        + (stale ? ` <span class="pill bad">順序逆行</span>` : "")
        + ` <span class="votes">位置が逆行して捨てたバースト</span>`],
      ["PLC 反復", `${rep} <span class="votes">`
        + `直前の良フレームを繰り返した (${(rep * FRAME_MS / 1000).toFixed(1)}s)</span>`],
      ["PLC ミュート", mute
        ? `${mute} <span class="badge warn">長区間の欠落</span>`
          + ` <span class="votes">${(mute * FRAME_MS / 1000).toFixed(1)}s ぶん無音化</span>`
        : `0 <span class="votes">長区間の欠落なし</span>`],
      ["復号長", fin(a.decoded_seconds) === null
        ? "—" : `${a.decoded_seconds.toFixed(1)} s`],
      ["C 種別ハミング距離", fin(a.c_dist_max) === null ? "—"
        : `最大 ${iv(a.c_dist_max)} / 平均 `
          + `${fin(a.c_dist_mean) === null ? "—" : a.c_dist_mean.toFixed(2)}`
          + (fin(a.c_dist_bad) === null ? ""
            : (a.c_dist_bad ? ` <span class="pill bad">不一致 ${a.c_dist_bad}</span>`
              : ` <span class="badge ok">全バースト完全一致</span>`))],
    ];
    if (a.wav_path) rows.push(["保存先", esc(a.wav_path)]);
    decKv.innerHTML = rows.map(([k, v]) =>
      `<tr><th>${k}</th><td>${v}</td></tr>`).join("");

    const notes = [
      "CRC7 一致率の分母は受信できたフレームのみ（欠落補間したフレームは分母に入らない）。",
      "C 種別ハミング距離: 0 = 判定コードと完全一致。大きいほど TCH 側が劣化している。",
    ];
    if (mute) {
      notes.unshift("PLC ミュートが出ている = 長区間の欠落があり、その区間は無音に置換されている。");
    }
    if (a.decode_attempted === false) {
      notes.unshift("この通報はまだ音声デコードを試行していない。");
    }
    if (a.note) notes.unshift(esc(a.note));
    decNote.innerHTML = notes.map(s => `<div>${s}</div>`).join("");
  }

  function stateLabel() {
    if (!ctx) return `<span class="badge idle">未開始</span>`;
    if (ctx.state === "running") return `<span class="badge ok">再生中</span>`;
    if (userPaused) return `<span class="badge idle">一時停止</span>`;
    return `<span class="badge warn">停止中（要クリック）</span>`;
  }

  function update() {
    playBtn.textContent = (ctx && ctx.state === "running") ? "⏸ 一時停止" : "▶ 再生開始";
    muteBtn.className = "btn plain" + (muted ? " on" : "");
    muteBtn.textContent = muted ? "ミュート中" : "ミュート";
    volVal.textContent = `${Math.round(volume * 100)}%`;

    const db = level > 0 ? 20 * Math.log10(level) : -100;
    const pct = Math.max(0, Math.min(1, (db + 60) / 60)) * 100;
    if (meter) meter.style.width = `${pct.toFixed(0)}%`;

    const buffered = (ctx && ctx.state === "running")
      ? Math.max(0, playhead - ctx.currentTime) : 0;

    const rows = [
      ["状態", stateLabel()],
      ["受信中の通報", curWindow ? `#${curWindow}` : "—"],
      ["ジッタバッファ", `${(buffered * 1000).toFixed(0)} ms`
        + (buffered > MAX_BUFFER * 0.75 ? ` <span class="badge warn">遅延大</span>` : "")
        + ` <span class="votes">目標 ${(JITTER * 1000).toFixed(0)} ms`
        + ` / 上限 ${(MAX_BUFFER * 1000).toFixed(0)} ms</span>`],
      ["追いつき破棄", dropCount
        ? `${dropSeconds.toFixed(1)} s <span class="pill bad">${dropCount} 回</span>`
          + ` <span class="votes">上限超過で未再生ぶんを捨てた（全長は WAV に残る）</span>`
        : `0 <span class="votes">上限超過なし</span>`],
      ["レベル", db <= -99 ? "—" : `${db.toFixed(1)} dBFS`],
      ["受信 PCM", `${recvSeconds.toFixed(1)} s / ${chunks} チャンク`
        + (gaps ? ` <span class="pill bad">欠落 ${gaps}</span>` : "")],
      ["ストリーム", wsState === "open"
        ? `<span class="badge ok">接続</span>`
        : `<span class="badge warn">${esc(wsState === "closed" ? "再接続中…" : "接続中…")}</span>`],
    ];
    kv.innerHTML = rows.map(([k, v]) =>
      `<tr><th>${k}</th><td>${v}</td></tr>`).join("");
    renderDecode();

    if (!ctx || ctx.state !== "running") {
      hint.textContent = userPaused
        ? "一時停止中です。［▶ 再生開始］で再開します。"
        : "クリックすると再生を開始します（ブラウザの自動再生制限で無音のままになります）。";
    } else {
      hint.textContent = "16kHz モノラル。ジッタバッファ 0.6 秒ぶん遅れて鳴ります"
        + `（${MAX_BUFFER.toFixed(0)} 秒を超えて溜まったら捨てて追いつきます）。`;
    }
    if (sub) {
      sub.textContent = (ctx && ctx.state === "running")
        ? (curWindow ? `通報 #${curWindow} 再生中` : "再生中")
        : "停止中";
    }
  }

  setInterval(() => {
    level *= 0.6;
    if (level < 1e-4) level = 0;
    update();
  }, 200);

  store.on("windows", update);

  ensureCtx();
  connect();
  update();
}
