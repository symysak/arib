import { TOPIC } from '../store.js';
import { hms, ymd, isEstimated } from '../time.js';
import { UI_TICK_MS, CLOCK_TICK_MS } from '../ui.js';
import { el, setText, setClass } from '../dom.js';

export function init(store) {
  const root = document.getElementById('pane-link');
  const mk = (k) => {
    const v = el('span', { class: 'v' });
    return { box: el('div', { class: 'metric' }, [el('span', { class: 'k', text: k }), v]), v };
  };
  const conn = el('span', { class: 'badge', text: '接続中' });
  const sync = mk('同期'), talk = mk('通話中'), rec = mk('録音');
  const modeAll = el('button', { class: 'btn', text: '全部' });
  const modeSel = el('button', { class: 'btn sel', text: '選択局' });
  const scopeWhat = el('span', { class: 'pill info' });
  const time = el('span', { text: '--:--:--' });
  const date = el('span', { class: 'date' });
  const clock = el('div', { class: 'clock' }, [time, date]);
  root.append(
    el('div', { class: 'group' }, [el('div', { class: 'title' }, [
      el('b', { text: root.dataset.title || document.title }), el('span', { text: root.dataset.sub || '' })])]),
    el('div', { class: 'group' }, [conn]),
    el('div', { class: 'group' }, [sync.box, talk.box, rec.box]),
    el('div', { class: 'group' }, [modeAll, modeSel, scopeWhat]),
    el('div', { class: 'spacer' }),
    el('div', { class: 'group' }, [clock]),
  );

  const PREF_MODE = 't61.link.mode';
  const MODES = ['all', 'sel'];
  function modeOf(s) { return s.showAll ? 'all' : 'sel'; }
  function setMode(m) { store.setShowAll(m === 'all'); }
  modeAll.addEventListener('click', () => setMode('all'));
  modeSel.addEventListener('click', () => setMode('sel'));
  const defMode = MODES.includes(root.dataset.modeDefault) ? root.dataset.modeDefault : 'sel';
  let savedMode = defMode;
  try { savedMode = localStorage.getItem(PREF_MODE) || defMode; } catch {  }
  if (!MODES.includes(savedMode)) savedMode = defMode;
  setMode(savedMode);

  function drawConn(s) {
    const map = { connecting: ['接続中', 'warn'], open: ['接続', 'ok'], reconnecting: ['再接続中', 'crit'] };
    const [text, cls] = map[s.conn] || ['?', 'idle'];
    setText(conn, text);
    setClass(conn, `badge ${cls}`);
  }

  function draw() {
    const s = store.state;
    const nsync = s.stations.filter((x) => x.synced).length;
    setText(sync.v, `${nsync}/${s.stations.length}`);
    const t = s.stations.filter((x) => x.ext && x.ext.talking).length;
    setText(talk.v, t ? `${t} 局` : 'なし');
    setClass(talk.v, t ? 'v bad' : 'v');
    const r = s.stations.filter((x) => x.recording).length;
    setText(rec.v, r ? `${r} 局` : 'なし');
    setClass(rec.v, r ? 'v warnv' : 'v');
    const m = modeOf(s);
    setClass(modeAll, m === 'all' ? 'btn sel' : 'btn');
    setClass(modeSel, m === 'sel' ? 'btn sel' : 'btn');
    if (m !== savedMode) {
      savedMode = m;
      try { localStorage.setItem(PREF_MODE, m); } catch {  }
    }
    const st = store.selectedStation();
    scopeWhat.hidden = m === 'all';
    setText(scopeWhat, m === 'all' ? '' : (st ? `${st.mhz.toFixed(4)} MHz` : '-'));
    setText(time, hms(s.lastT));
    setText(date, ymd(s.lastT));
    setClass(clock, isEstimated() ? 'clock estimated' : 'clock');
  }

  store.on(TOPIC.conn, drawConn);
  store.on(TOPIC.meta, draw);
  store.on(TOPIC.selection, draw);
  setInterval(draw, UI_TICK_MS);
  setInterval(() => { setText(time, hms(store.state.lastT)); setText(date, ymd(store.state.lastT)); }, CLOCK_TICK_MS);
  drawConn(store.state);
}
