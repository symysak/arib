import { fit, fade, clear, label, COLOR } from '../draw.js';
import { UI_TICK_MS } from '../ui.js';

const RANGE = 4.5;

export function init(store) {
  const body = document.getElementById('eye-body');
  const cv = document.createElement('canvas');
  cv.style.cssText = 'width:100%;height:150px;display:block';
  const hv = document.createElement('canvas');
  hv.style.cssText = 'width:100%;height:70px;display:block';
  const info = document.createElement('div');
  info.className = 'tiny muted';
  body.append(cv, hv, info);
  let drawn = 0;

  function yOf(v, h) { return h / 2 - (v / RANGE) * (h / 2 - 4); }

  function drawEye() {
    const { g, w, h } = fit(cv);
    const eyes = store.state.t102eyes;
    if (drawn > eyes.length) drawn = 0;
    if (drawn === 0) clear(g, w, h);
    else fade(g, w, h, 0.25);
    g.strokeStyle = COLOR.grid();
    g.lineWidth = 1;
    for (const v of [-3, -1, 1, 3]) {
      g.beginPath(); g.moveTo(0, yOf(v, h)); g.lineTo(w, yOf(v, h)); g.stroke();
    }
    g.beginPath(); g.moveTo(w / 2, 0); g.lineTo(w / 2, h); g.stroke();
    const st = store.selectedStation();
    g.strokeStyle = st && st.synced ? COLOR.accent() : COLOR.grid();
    g.globalAlpha = 0.5;
    for (let i = drawn; i < eyes.length; i++) {
      for (const tr of eyes[i].traces) {
        g.beginPath();
        tr.forEach((v, j) => {
          const x = (j / (tr.length - 1)) * w;
          const y = yOf(Math.max(-RANGE, Math.min(RANGE, v)), h);
          if (j) g.lineTo(x, y); else g.moveTo(x, y);
        });
        g.stroke();
      }
    }
    g.globalAlpha = 1;
    drawn = eyes.length;
    label(g, '+3', 2, yOf(3, h) - 2);
    label(g, '-3', 2, yOf(-3, h) - 2);
  }

  function drawHist() {
    const { g, w, h } = fit(hv);
    clear(g, w, h);
    const st = store.selectedStation();
    const sig = st && store.state.t102signals[String(st.idx)];
    if (!sig || !sig.hist) { info.textContent = '記号の分布: まだ'; return; }
    const hist = sig.hist;
    const mx = Math.max(1, ...hist);
    const bw = w / hist.length;
    g.fillStyle = COLOR.accent();
    hist.forEach((n, i) => {
      const bh = (n / mx) * (h - 12);
      g.fillRect(i * bw + 1, h - 10 - bh, bw - 2, bh);
    });
    g.fillStyle = COLOR.ink3();
    for (const v of [-3, -1, 1, 3]) {
      const x = ((v + RANGE) / (2 * RANGE)) * w;
      label(g, v > 0 ? `+${v}` : String(v), x - 6, h - 1);
    }
    info.textContent = `偏位 ±1 = ${sig.dev_hz.toFixed(0)} Hz・直流 ${sig.dc_hz.toFixed(0)} Hz・EVM ${sig.evm.toFixed(3)}・同期ワードの誤り ${sig.fsw_err.toFixed(2)}`;
  }

  store.on('t102eye', drawEye);
  store.on('selection', () => { drawn = 0; drawEye(); drawHist(); });
  setInterval(drawHist, UI_TICK_MS * 2);
  drawEye();
  drawHist();
}
