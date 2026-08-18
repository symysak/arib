import { hmsMilli } from '../time.js';
import { TOPIC } from '../store.js';

export function init(store) {
  const body = document.getElementById('control-body');
  const sub = document.getElementById('control-sub');

  const scrambleCell = (sc) => {
    if (!sc || !sc.locked) return '<span class="pill">判定中…</span>';
    const hex = `0x${sc.init.toString(16).toUpperCase().padStart(4, '0')}`;
    if (sc.pinned) return `${hex} <span class="pill">固定</span>`;
    return `${hex} <span class="pill ok">自動</span>`
      + ` <span class="muted tiny">信頼度 ${sc.confidence.toFixed(3)} / SB0 ${sc.frames} 枚</span>`;
  };

  const municipalityCell = (sc) => {
    if (!sc || !sc.locked) return '<span class="muted">-</span>';
    if (!sc.municipality_known) {
      return `<span class="muted">${sc.municipality_label}</span>`
        + ' <span class="muted tiny">（変換ルールが規格書 Annex 4 非開示のため未実装）</span>';
    }
    return `<b>${sc.municipality}</b>`
      + ` <span class="muted tiny">コード ${sc.municipal_code}</span>`;
  };

  const render = (st) => {
    const s = st || store.state;
    const m = s.meta;
    if (!m) { body.innerHTML = '<p class="muted">接続待ち…</p>'; return; }

    const last = s.controls[s.controls.length - 1];
    const mfr = last ? `${last.mfr_name}（${last.mfr_code}）` : '-';
    sub.textContent = last ? `最新 ${hmsMilli(last.t_sec)}` : '';

    let notify = '';
    if (last && last.notify) {
      const n = last.notify;
      notify = `
        <dt>報知対象</dt><dd>${n.target}${n.simultaneous ? ' <span class="pill info">一斉</span>' : ''}</dd>
        <dt>メディア</dt><dd>${n.media}（伝送プロトコル ${n.trans_prot}）</dd>
        <dt>呼番号</dt><dd>${n.call_no}</dd>
        <dt>フラグ</dt><dd>${[
          n.emergency ? '緊急' : '', n.forced_volume ? '強制音量' : '',
          n.record_release ? '録音解除' : '', n.number_notify ? '番号通知' : '',
          n.time_split_ok ? `時差(分割${n.split_no})` : '',
        ].filter(Boolean).join(' / ') || 'なし'}</dd>`;
    }

    body.innerHTML = `
      <dl class="kv">
        <dt>方式</dt><dd>QPSK ナロー（Vol.2 / 7.5kHz / SCPC）</dd>
        <dt>製造者</dt><dd>${mfr}</dd>
        <dt>直近種別</dt><dd>${last ? `0x${last.msg_type.toString(16).toUpperCase().padStart(2, '0')} ${last.msg_type_name}` : '-'}</dd>
        ${notify}
      </dl>
      <hr style="border:0;border-top:1px solid var(--line-soft);margin:8px 0">
      <dl class="kv tiny">
        <dt>SW1 (SB0↓)</dt><dd>${m.sw1}</dd>
        <dt>SW3 (SC↓)</dt><dd>${m.sw3}</dd>
        <dt>スクランブル</dt><dd>${scrambleCell(s.scramble)}</dd>
        <dt>市区町村</dt><dd>${municipalityCell(s.scramble)}</dd>
        <dt>シンボル</dt><dd>${m.symbolRate} sym/s · ${m.frameBits}bit/80ms · RRC α=${m.rollOff}</dd>
      </dl>
      <p class="muted tiny" style="margin:6px 0 0">
        同期ワードは規格書 Annex 4 が非開示のため実波から同定した値。
        スクランブル値は<b>自治体ごとに違う</b>ので受信のたびに自動判定する
        （CCH の繰り返し等式から解く）。
      </p>`;
  };
  store.on(TOPIC.meta, render);
  store.on(TOPIC.control, render);
  store.on(TOPIC.conn, render);
  store.on(TOPIC.scramble, render);
  render();
}
