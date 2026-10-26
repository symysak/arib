import { TOPIC } from '../store.js';
import { makeLayerPane } from './layers.js';
import { hms } from '../time.js';

export function init(store) {
  makeLayerPane(store, {
    pane: 'calls',
    topic: TOPIC.callseq,
    empty: '呼はまだありません（制御チャネルの呼設定・無線チャネル指定、または通信チャネルの通話を見ると立ちます）。',
    items: (s) => s.callseq.filter((c) => store.inScope(c.idx) || (c.traffic_idx >= 0 && store.inScope(c.traffic_idx))),
    key: (c) => `call/${c.id}`,
    columns: [
      { name: '呼', class: 'mono tiny', get: (c) => `#${c.id}` },
      { name: '開始', class: 'muted tiny', get: (c) => hms(c.start) },
      { name: '局', class: 'mono', get: (c) => {
        const st = store.station(c.idx);
        const a = st ? st.mhz.toFixed(4) : '?';
        return c.traffic_mhz && c.traffic_mhz !== c.mhz ? `${a}→${c.traffic_mhz.toFixed(4)}` : a;
      } },
      { name: '通信種別 / 手順', class: 'tiny', get: (c) => c.type || '（未定）' },
      { name: '状態', class: 'badge info', get: (c) => (c.open ? c.phase : c.outcome || '終了') },
      { name: '直近', class: 'grow tiny muted', get: (c) => (c.events.length ? c.events[c.events.length - 1].text : '') },
    ],
    detail: (c) => {
      const tree = [
        { label: c.proc && c.proc !== '呼' ? '手順' : '通信種別', value: c.type || '（未定）', note: c.basis ? `根拠: ${c.basis}` : '' },
        { label: '規格の節', value: c.section ? `§${c.section}` : '' },
        { label: '下りで聞こえる声', value: c.downlink || '' },
        { label: 'アドレス', value: c.addr || '（通信チャネル）' },
        { label: '呼番号', value: c.ref >= 0 ? String(c.ref) : '' },
        { label: '通信チャネル', value: c.traffic_mhz ? `${c.traffic_mhz.toFixed(4)} MHz` : '' },
        { label: '段階', value: c.phase },
        { label: '結果', value: c.outcome || (c.open ? '（継続中）' : ''), note: c.detail || '' },
        { label: '規格と合わない点', value: c.anomalies ? `${c.anomalies} 件（出来事の anomaly）` : 'なし' },
        { label: '出来事', value: `${c.events.length} 件`,
          children: c.events.map((e) => ({ label: hms(e.t_sec), value: e.text, note: `${e.mhz.toFixed(4)} MHz ${e.kind}` })) },
      ];
      return { title: `呼#${c.id} ${c.type || ''} ${c.open ? c.phase : c.outcome || ''}`, tree, hex: '' };
    },
  });
}
