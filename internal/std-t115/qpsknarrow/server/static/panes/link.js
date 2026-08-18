import { hms, ymd, isEstimated } from '../time.js';
import { sparkline, COL } from '../draw.js';
import { TOPIC } from '../store.js';
import { UI_TICK_MS, CLOCK_TICK_MS } from '../ui.js';

export function init(store) {
  const el = document.getElementById('pane-link');
  el.innerHTML = `
    <div class="group">
      <div class="title"><b>ARIB STD-T115</b><span>QPSK ナロー方式</span></div>
    </div>
    <div class="group">
      <div class="clock" id="lb-clock">--:--:--<span class="date"></span></div>
    </div>
    <div class="group" id="lb-state"></div>
    <div class="group">
      <div class="metric"><span class="k">スクランブル</span><span class="v" id="lb-scr">-</span></div>
      <div class="metric"><span class="k">市区町村</span><span class="v" id="lb-muni">-</span></div>
    </div>
    <div class="group">
      <div class="metric"><span class="k">同期相関</span><span class="v" id="lb-corr">-</span>
        <canvas id="lb-corr-spark"></canvas></div>
      <div class="metric"><span class="k">EVM</span><span class="v" id="lb-evm">-</span>
        <canvas id="lb-evm-spark"></canvas></div>
      <div class="metric"><span class="k">残留CFO</span><span class="v" id="lb-cfo">-</span></div>
    </div>
    <div class="group">
      <div class="metric"><span class="k">CCH CRC16</span><span class="v" id="lb-cch">-</span></div>
      <div class="metric"><span class="k">TCH CRC16</span><span class="v" id="lb-tch">-</span></div>
      <div class="metric"><span class="k">フレーム</span><span class="v" id="lb-frames">-</span></div>
    </div>
    <div class="group spacer"></div>
    <div class="group">
      <div class="metric"><span class="k">処理速度</span><span class="v" id="lb-thru">-</span></div>
      <div class="metric"><span class="k">入力</span><span class="v" id="lb-src">-</span></div>
    </div>`;

  const $ = (id) => document.getElementById(id);

  const renderScramble = (st) => {
    const sc = (st || store.state).scramble;
    if (!sc || !sc.locked) {
      $('lb-scr').textContent = '判定中…';
      $('lb-muni').textContent = '-';
      return;
    }
    $('lb-scr').textContent = `0x${sc.init.toString(16).toUpperCase().padStart(4, '0')}`
      + (sc.pinned ? '（固定）' : '');
    $('lb-muni').textContent = sc.municipality_known ? sc.municipality : sc.municipality_label;
  };
  const pct = (a, b) => (b > 0 ? (100 * a / b) : null);

  function renderClock(st) {
    const clock = $('lb-clock');
    clock.className = 'clock' + (isEstimated() ? ' estimated' : '');
    clock.firstChild.nodeValue = st.lastT ? hms(st.lastT) : '—';
    const d = clock.querySelector('.date');
    if (d) d.textContent = st.lastT ? ymd(st.lastT) : '—';
  }

  function renderState(st) {
    const sig = st.signal || {};
    const badges = [];
    badges.push(st.conn === 'open'
      ? '<span class="badge ok">接続</span>'
      : st.conn === 'reconnecting'
        ? '<span class="badge warn">再接続中</span>'
        : '<span class="badge idle">接続中</span>');
    const synced = (sig.corr_peak || 0) > 4;
    badges.push(synced
      ? '<span class="badge info">同期</span>'
      : '<span class="badge idle">同期なし</span>');
    badges.push(st.current
      ? `<span class="badge crit live">通報中 #${st.current.id}</span>`
      : '<span class="badge idle">待機</span>');
    if (st.finished) badges.push('<span class="badge idle">終端</span>');
    $('lb-state').innerHTML = badges.join(' ');
  }

  function renderSignal(st) {
    const sig = st.signal || {};
    const synced = (sig.corr_peak || 0) > 4;
    $('lb-corr').textContent = (sig.corr_peak || 0).toFixed(2);
    $('lb-corr').className = 'v' + (synced ? ' okv' : ' warnv');
    $('lb-evm').textContent = (sig.evm || 0).toFixed(3);
    $('lb-evm').className = 'v' + ((sig.evm || 1) < 0.25 ? ' okv' : ' warnv');
    $('lb-cfo').textContent = `${(sig.cfo_hz || 0).toFixed(0)} Hz`;
    sparkline($('lb-corr-spark'), st.corr.slice(-76).map((p) => p.v),
      { lo: 0, hi: 8, color: COL.accent() });
    sparkline($('lb-evm-spark'), st.evm.slice(-76).map((p) => p.v),
      { lo: 0, hi: 0.6, color: COL.warn() });
  }

  function renderQuality(st) {
    const q = st.quality || {};
    const c = pct(q.cch_ok, q.cch_total), t = pct(q.tch_ok, q.tch_total);
    $('lb-cch').textContent = c === null ? '-' : `${c.toFixed(1)}%`;
    $('lb-cch').className = 'v' + (c === null ? '' : c > 99 ? ' okv' : c > 90 ? ' warnv' : ' bad');
    $('lb-tch').textContent = t === null ? '-' : `${t.toFixed(1)}%`;
    $('lb-tch').className = 'v' + (t === null ? '' : t > 99 ? ' okv' : t > 90 ? ' warnv' : ' bad');
    $('lb-frames').textContent = String(q.frames || 0);
    $('lb-thru').textContent = q.throughput ? `${q.throughput.toFixed(1)}×` : '-';
    $('lb-src').textContent = st.meta ? (st.meta.network ? 'TCP' : 'ファイル') : '-';
  }

  store.on(TOPIC.conn, renderState);
  store.on(TOPIC.scramble, renderScramble);
  store.on(TOPIC.meta, renderScramble);
  renderScramble();
  store.on(TOPIC.meta, (st) => { renderState(st); renderQuality(st); });
  store.on(TOPIC.windows, renderState);
  store.on(TOPIC.signal, (st) => { renderSignal(st); renderState(st); });
  store.on(TOPIC.quality, (st) => { renderQuality(st); renderClock(st); });
  store.on(TOPIC.frame, (st) => { renderSignal(st); renderClock(st); });

  setInterval(() => renderQuality(store.state), UI_TICK_MS);
  setInterval(() => renderClock(store.state), CLOCK_TICK_MS);

  renderClock(store.state); renderState(store.state);
  renderSignal(store.state); renderQuality(store.state);
}
