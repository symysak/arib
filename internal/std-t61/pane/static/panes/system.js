import { TOPIC } from '../store.js';
import { UI_TICK_MS } from '../ui.js';
import { dur } from '../time.js';
import { el, setText } from '../dom.js';

const KV = [['input', '入力'], ['center', '中心 / 標本化'], ['chans', '監視局数'],
  ['dir', '記録先'], ['method', '方式'], ['elapsed', '経過'],
  ['frames', '総フレーム'], ['counts', '記録']];

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
    const frames = s.stations.reduce((a, x) => a + x.frames, 0);
    const rich = s.stations.reduce((a, x) => a + x.rich_ok_num, 0);
    setText(sub, dur(s.lastT) + (s.meta.finished ? '（入力終端）' : ''));
    setText(v.input, s.meta.input);
    setText(v.center, `${s.meta.center_mhz.toFixed(4)} MHz / ${(s.meta.sample_rate / 1e6).toFixed(3)} MS/s`);
    setText(v.chans, String(s.meta.channels.length));
    setText(v.dir, s.meta.log_dir);
    setText(v.method, s.meta.method || 'STD-T61 のみ（上位方式の拡張なし）');
    setText(v.elapsed, dur(s.lastT));
    setText(v.frames, `${frames}（RICH 一致 ${rich}）`);
    const ext = Object.entries(s.ext).map(([k, a]) => `${k} ${a.length}`).join(' / ');
    setText(v.counts, `L1 ${s.l1.length} / L2 ${s.l2.length} / L3 ${s.l3.length}`
      + ` / 通話 ${s.calls.length} / 録音 ${s.recs.length}` + (ext ? ` / ${ext}` : ''));
  }

  store.on(TOPIC.meta, draw);
  setInterval(draw, UI_TICK_MS);
  draw();
}
