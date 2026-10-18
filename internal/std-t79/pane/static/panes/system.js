import { TOPIC } from '../store.js';
import { UI_TICK_MS } from '../ui.js';
import { dur } from '../time.js';
import { el, setText } from '../dom.js';

const KV = [['input', '入力'], ['center', '中心 / 標本化'], ['chans', '監視局数'], ['dir', '記録先'], ['method', '方式'],
  ['codec', '音声の符号器'], ['elapsed', '経過'], ['counts', '記録']];

export function init(store) {
  const body = document.getElementById('system-body');
  const sub = document.getElementById('system-sub');
  const dl = el('dl', { class: 'kv' });
  const v = {};
  for (const [k, n] of KV) {
    dl.append(el('dt', { text: n }));
    v[k] = el('dd', k === 'input' || k === 'dir' ? { class: 'mono' } : {});
    dl.append(v[k]);
  }
  body.appendChild(dl);

  function draw() {
    const s = store.state;
    if (!s.meta) { setText(v.input, '接続待ち'); return; }
    setText(sub, dur(s.lastT) + (s.meta.finished ? '（入力終端）' : ''));
    const ins = s.meta.inputs || [];
    if (ins.length > 1) {
      setText(v.input, ins.map((x) => `${x.label}: ${x.input}${x.finished ? '（終端）' : ''}`).join(' / '));
      setText(v.center, ins.map((x) => `${x.label} ${x.center_mhz.toFixed(4)} MHz / ${(x.sample_rate / 1e6).toFixed(3)} MS/s`).join('・'));
    } else {
      setText(v.input, s.meta.input);
      setText(v.center, `${s.meta.center_mhz.toFixed(4)} MHz / ${(s.meta.sample_rate / 1e6).toFixed(3)} MS/s`);
    }
    setText(v.chans, String(s.meta.channels.length));
    setText(v.dir, s.meta.log_dir);
    setText(v.method, s.meta.system);
    setText(v.codec, `${s.meta.codec_mode || 'off'}（登録: ${s.meta.codecs || '無し'}）`);
    setText(v.elapsed, dur(s.lastT));
    setText(v.counts, `L1 ${s.l1.length} / L2 ${s.l2.length} / L3 ${s.l3.length} / 呼 ${s.calls.length} / 制御キャリア ${s.nets.size} / 録音 ${s.recs.length}`);
  }
  store.on(TOPIC.meta, draw);
  setInterval(draw, UI_TICK_MS);
  draw();
}
