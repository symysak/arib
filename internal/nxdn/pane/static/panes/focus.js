import { hms, dur } from '../time.js';
import { el, setText, setClass } from '../dom.js';
import { KIND, recURL } from '../rec.js';
import { UI_TICK_MS } from '../ui.js';
import { cfg } from '../nxdn.js';

export function init(store) {
  const body = document.getElementById('focus-body');
  const sub = document.getElementById('focus-sub');
  if (!body) return;
  const state = el('span', { class: 'badge info' });
  const back = el('button', { class: 'btn', type: 'button', text: '最新へ' });
  back.addEventListener('click', () => store.clearFocus());
  const big = el('div', { class: 'focusbig mono' });
  const line = el('div', { class: 'muted' });
  const dl = el('dl', { class: 'kv' });
  const player = el('div', { class: 'focusplay' });
  const empty = el('div', { class: 'muted tiny', text: '送信はまだありません' });
  const box = el('div', {}, [el('div', { class: 'row', style: 'align-items:center;gap:6px' }, [state, back]), big, line, dl, player]);
  body.append(box, empty);
  let playFor = '';

  function draw() {
    const s = store.state;
    const picked = store.focusedTx();
    const scoped = store.scopeTxs();
    const tx = picked || scoped[scoped.length - 1];
    box.style.display = tx ? '' : 'none';
    empty.style.display = tx ? 'none' : '';
    if (!tx) { setText(sub, s.showAll ? '全局' : '選択局'); return; }
    const st = store.station(tx.idx);
    setClass(state, picked ? 'badge warn' : 'badge info');
    setText(state, picked ? '選択中' : `最新（${s.showAll ? '全局' : '選択局'}の中）`);
    back.style.display = picked ? '' : 'none';
    setText(sub, `${hms(tx.t_sec)}${st ? `・${st.mhz.toFixed(4)} MHz` : ''}`);
    const head = cfg.focusHead(tx, st);
    setText(big, head.big);
    setText(line, head.sub);
    const rows = [...cfg.focusRows(tx, st),
      ['種別', `${tx.kind}${tx.open ? '（送信中）' : ''}`],
      ['長さ', tx.open ? `${tx.frames} フレーム` : `${dur(Math.max(0, tx.end_sec - tx.t_sec))}・${tx.frames} フレーム`],
      ['音声の検査', tx.voice ? `${tx.voice_ok}/${tx.voice}（${Math.round((100 * tx.voice_ok) / tx.voice)}%）` : '-'],
      ['終わり方', tx.open ? '-' : (tx.end || '-')],
      ['メッセージ', (tx.msgs || []).slice(0, 4).join(' / ') || '-'],
    ];
    const want = rows.map((r) => `${r[0]}\u0001${r[1]}`).join('\u0002');
    if (dl.dataset.for !== want) {
      dl.dataset.for = want;
      dl.replaceChildren();
      for (const [k, v] of rows) dl.append(el('dt', { text: k }), el('dd', { class: 'tiny', text: String(v) }));
    }
    const rel = tx.base && !tx.open ? (s.recs.find((r) => r.base === tx.base) || {}).name : '';
    const key = rel ? `${rel}|${tx.audio}` : (tx.open ? 'open' : '');
    if (playFor !== key) {
      playFor = key;
      player.replaceChildren();
      if (rel && tx.audio) {
        const a = el('audio', { controls: '', preload: 'none', src: recURL('audio', rel, KIND.audio) });
        a.style.cssText = 'width:100%;height:34px';
        player.append(a);
      } else if (rel) {
        player.append(el('span', { class: 'muted tiny', text: 'この送信の音声の録音はありません（音声でないか、符号器・秘話の鍵が決まらなかった）' }));
      } else if (tx.open) {
        player.append(el('span', { class: 'muted tiny', text: '送信中（終わると録音を再生できます。今の音はライブ音声で）' }));
      }
      if (rel) player.append(el('div', { class: 'row inline', style: 'margin-top:4px' }, [el('a', { class: 'pill', href: recURL('iq', rel, KIND.iq), target: '_blank', text: 'I/Q の録音' })]));
    }
  }

  store.on('nxdntxs', draw);
  store.on('nxdnfocus', draw);
  store.on('selection', draw);
  store.on('activity', draw);
  setInterval(draw, UI_TICK_MS * 5);
  draw();
}
