import { TOPIC } from '../store.js';
import { makeLayerPane, timeCol, freqCol } from './layers.js';
import { el } from '../dom.js';

export function init(store) {
  const bar = document.getElementById('l2-toolbar');
  const follow = el('button', { class: 'btn sel', text: '最新へ追従' });
  bar.append(follow,
    el('span', { class: 'tiny muted', text: '1 行 = LAPDM フレーム。SACCH なら 8 枚で 1 ユニット、W が示す数だけユニットを集めて完結' }));
  bar.append(el('div', { class: 'tiny muted notice' }, [
    el('b', { text: '状態の見方: ' }),
    el('span', { text: store.text('t61.l2.notice', '「メッセージ無し」は CRC が通る正規のアイドル・ユニット（W=00。基地局が'
      + '「送る L2 メッセージは無い」と符号化して送っている）。'
      + '「空き枠（充填）」は CRC が落ちる充填で、送るものが無いスロットを全ゼロ（零符号語）や'
      + '計数語（非符号語）で埋めただけ＝ユニットですらない（本物の欠落ではない）。'
      + 'まれな「符号語だが CRC 不一致（別規約の疑い）」は、畳込み符号としては正しいのに CRC16 だけ合わない'
      + 'もので、T61 とは別規約の信号がこの枠を使っている疑い。中身は「未検証」として一覧に出している。') }),
  ]));

  const pane = makeLayerPane(store, {
    pane: 'l2',
    topic: TOPIC.l2,
    empty: 'この局の L2 フレームはまだありません',
    items: (s) => s.l2.filter((r) => store.inScope(r.idx)),
    key: (r) => `l2/${r.no}`,
    autoPick: (items) => {
      for (let i = items.length - 1; i >= 0; i--) {
        if (items[i].state !== '組立中') return items[i];
      }
      return items[items.length - 1];
    },
    columns: [
      { name: '番号', class: 'mono tiny', get: (r) => `#${r.no}` },
      timeCol,
      freqCol(store),
      { name: 'チャネル', class: 'badge info', get: (r) => r.channel },
      { name: '状態', class: 'pill', get: (r) => r.state },
      { name: '進捗', class: 'tiny muted', get: (r) => r.stage || '' },
      { name: '生', class: 'mono tiny', get: (r) => r.raw_hex || '' },
      { name: '概要', class: 'grow tiny', get: (r) => r.summary || '' },
    ],
    detail: (r) => ({
      title: `#${r.no} ${r.channel} ${r.state}　${r.stage || ''}　ユニット ${r.units}${r.err ? '　形式異常: ' + r.err : ''}`,
      tree: r.tree, hex: r.raw_hex,
      links: [
        el('span', { class: 'pill info', text: `L1 フレーム ${r.frame_nos.map((n) => '#' + n).join(' ')}` }),
        ...(r.l3_nos || []).map((n) => el('span', { class: 'pill ok', text: `→ L3 #${n}` })),
      ],
    }),
  });
  follow.addEventListener('click', () => pane.follow());
}
