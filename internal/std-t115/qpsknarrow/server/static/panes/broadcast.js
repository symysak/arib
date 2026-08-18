import { fitCanvas, clear, gridLines, bands, label, COL } from '../draw.js';
import { hms, dur } from '../time.js';
import { TOPIC } from '../store.js';
import { UI_TICK_MS } from '../ui.js';

export function init(store) {
  const body = document.getElementById('broadcast-body');
  const sub = document.getElementById('broadcast-sub');
  body.innerHTML = `
    <canvas id="bc-timeline"></canvas>
    <div class="axis tiny muted" style="display:flex;justify-content:space-between">
      <span id="bc-t0">-</span><span id="bc-t1">-</span></div>
    <div id="bc-now" style="margin-top:8px"></div>
    <div class="scroll" id="bc-list" style="margin-top:8px"></div>`;

  const drawTimeline = () => {
    const s = store.state;
    const cv = document.getElementById('bc-timeline');
    const { ctx, w, h } = fitCanvas(cv, 44);
    clear(ctx, w, h);

    const frames = s.frames;
    let t0 = frames.length ? frames[0].t_sec : 0;
    let t1 = s.lastT || t0 + 1;
    if (s.current) t0 = Math.min(t0, s.current.start_sec);
    for (const b of s.broadcasts) t0 = Math.min(t0, b.start_sec);
    if (t1 - t0 < 1) t1 = t0 + 1;
    const X = (t) => (t - t0) / (t1 - t0);
    document.getElementById('bc-t0').textContent = hms(t0);
    document.getElementById('bc-t1').textContent = hms(t1);

    const segs = s.broadcasts.map((b) => ({
      a: X(b.start_sec), b: X(b.endSec || b.end_sec), color: COL.accent(), alpha: 0.55,
    }));
    if (s.current) {
      segs.push({ a: X(s.current.start_sec), b: X(s.lastT), color: COL.crit(), alpha: 0.55 });
    }
    ctx.save();
    ctx.translate(0, 2);
    bands(ctx, w, 14, segs);
    ctx.restore();
    label(ctx, '通報', 3, 12, { color: COL.ink3() });

    ctx.save();
    ctx.translate(0, 22);
    const bh = 18;
    clear(ctx, w, bh, COL.bg());
    for (const f of frames) {
      const x = X(f.t_sec) * w;
      ctx.fillStyle = f.crc_ok ? COL.ok() : COL.crit();
      ctx.globalAlpha = f.crc_ok ? 0.75 : 1;
      ctx.fillRect(x, f.kind === 'SB0' ? 0 : bh / 2, Math.max(1, w / Math.max(frames.length, 1)), bh / 2);
    }
    ctx.globalAlpha = 1;
    ctx.restore();
    gridLines(ctx, w, h, { rows: 0 });
    label(ctx, 'SB0', 3, 32, { color: COL.ink3() });
    label(ctx, 'SC', 3, 42, { color: COL.ink3() });
  };

  const render = () => {
    const s = store.state;
    sub.textContent = `${s.broadcasts.length} 本` + (s.current ? '（通報中）' : '');
    const now = document.getElementById('bc-now');
    if (s.current) {
      const c = s.current;
      now.innerHTML = `<p style="margin:0">
        <span class="badge crit live">通報中 #${c.id}</span>
        <b>${c.target}</b>
        ${c.emergency ? '<span class="pill bad">緊急</span>' : ''}
        ${c.mid_join ? '<span class="pill">途中参加</span>' : ''}</p>
        <dl class="kv tiny"><dt>開始</dt><dd>${hms(c.start_sec)}</dd>
        <dt>経過</dt><dd>${dur(s.lastT - c.start_sec)}</dd>
        <dt>呼番号</dt><dd>${c.call_no}</dd>
        <dt>音声</dt><dd>${c.voice_frames || 0} フレーム</dd></dl>`;
    } else {
      now.innerHTML = '<p style="margin:0"><span class="badge idle">待機中</span></p>';
    }

    const links = (b) => [
      b.audio_url ? `<a href="${b.audio_url}">音声</a>` : '',
      b.iq_url ? `<a href="${b.iq_url}">I/Q</a>` : '',
    ].filter(Boolean).join(' / ');

    const list = document.getElementById('bc-list');
    if (!s.broadcasts.length) { list.innerHTML = '<p class="muted tiny">まだ通報はありません</p>'; return; }
    let html = '<table><thead><tr><th>#</th><th>開始</th><th>終了</th><th>長さ</th>'
      + '<th>対象</th><th>音声</th><th>終了理由</th><th>記録</th></tr></thead><tbody>';
    for (const b of [...s.broadcasts].reverse()) {
      html += `<tr><td class="num">${b.id}</td><td>${hms(b.start_sec)}</td>`
        + `<td>${hms(b.end_sec)}</td><td class="num">${dur(b.end_sec - b.start_sec)}</td>`
        + `<td>${b.target}${b.mid_join ? ' <span class="pill">途中</span>' : ''}</td>`
        + `<td class="num">${(b.voice_seconds || 0).toFixed(1)}s</td>`
        + `<td>${b.end_reason || ''}</td>`
        + `<td>${links(b)}</td></tr>`;
    }
    list.innerHTML = html + '</tbody></table>';
  };

  store.on(TOPIC.windows, () => { render(); drawTimeline(); });
  store.on(TOPIC.meta, () => { render(); drawTimeline(); });
  store.on(TOPIC.frame, () => drawTimeline());
  setInterval(() => { if (store.state.current) render(); }, UI_TICK_MS);
  window.addEventListener('resize', drawTimeline);
  render(); drawTimeline();
}
