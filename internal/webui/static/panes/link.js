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
  const input = mk('入力'), center = mk('中心'), fs = mk('標本化'), gain = mk('利得');
  const sync = mk('同期'), talk = mk('通話中'), rec = mk('録音');
  const modeAll = el('button', { class: 'btn', text: '全部' });
  const modeSel = el('button', { class: 'btn sel', text: '選択局' });
  const modeFollow = el('button', { class: 'btn', text: '自動追従' });
  const scopeWhat = el('span', { class: 'pill info' });
  const time = el('span', { text: '--:--:--' });
  const date = el('span', { class: 'date' });
  const clock = el('div', { class: 'clock' }, [time, date]);
  root.append(
    el('div', { class: 'group' }, [el('div', { class: 'title' }, [
      el('b', { text: root.dataset.title || document.title }), el('span', { text: root.dataset.sub || '' })])]),
    el('div', { class: 'group' }, [conn]),
    el('div', { class: 'group' }, [input.box]),
    el('div', { class: 'group' }, [center.box, fs.box, gain.box]),
    el('div', { class: 'group' }, [sync.box, talk.box, rec.box]),
    el('div', { class: 'group' }, [modeAll, modeSel, modeFollow, scopeWhat]),
    el('div', { class: 'spacer' }),
    el('div', { class: 'group' }, [clock]),
  );

  const PREF_MODE = 't61.link.mode';
  const MODES = ['all', 'sel', 'follow'];
  function modeOf(s) { return s.showAll ? 'all' : (s.audio.follow ? 'follow' : 'sel'); }
  function setMode(m) {
    store.setShowAll(m === 'all');
    store.setAudio({ follow: m === 'follow' });
  }
  modeAll.addEventListener('click', () => setMode('all'));
  modeSel.addEventListener('click', () => setMode('sel'));
  modeFollow.addEventListener('click', () => setMode('follow'));
  let savedMode = 'sel';
  try { savedMode = localStorage.getItem(PREF_MODE) || 'sel'; } catch {  }
  if (!MODES.includes(savedMode)) savedMode = 'sel';
  setMode(savedMode);

  function drawConn(s) {
    const map = { connecting: ['接続中', 'warn'], open: ['接続', 'ok'], reconnecting: ['再接続中', 'crit'] };
    const [text, cls] = map[s.conn] || ['?', 'idle'];
    setText(conn, text);
    setClass(conn, `badge ${cls}`);
  }

  let inputs = null;
  async function pollInputs() {
    try {
      const res = await fetch('api/inputs');
      inputs = res.ok ? await res.json() : null;
    } catch { inputs = null; }
  }
  const mhz = (hz) => `${(hz / 1e6).toFixed(4)} MHz`;
  function draw() {
    const s = store.state;
    if (inputs && inputs.length > 1) {
      const sts = inputs.map((x) => ({ label: x.label || x.id, st: x.status || {} }));
      setText(input.v, sts.map((x) => `${x.label}: ${x.st.input || '-'}`).join(' / '));
      setText(center.v, sts.map((x) => `${x.label} ${mhz(x.st.center_hz || 0)}`).join(' / '));
      setText(fs.v, sts.map((x) => `${((x.st.sample_rate || 0) / 1e6).toFixed(3)}`).join(' / ') + ' MS/s');
      setText(gain.v, sts.map((x) => (x.st.agc ? 'AGC' : `${(x.st.gain_db || 0).toFixed(1)}`)).join(' / ') + ' dB');
    } else if (s.meta) {
      setText(input.v, s.meta.input);
      setText(center.v, `${s.meta.center_mhz.toFixed(4)} MHz`);
      setText(fs.v, `${(s.meta.sample_rate / 1e6).toFixed(3)} MS/s`);
      setText(gain.v, `${s.meta.gain_db.toFixed(1)} dB`);
    }
    const nsync = s.stations.filter((x) => x.synced).length;
    setText(sync.v, `${nsync}/${s.stations.length}`);
    const t = s.stations.filter((x) => x.ext && x.ext.talking).length;
    setText(talk.v, t ? `${t} 局` : 'なし');
    setClass(talk.v, t ? 'v bad' : 'v');
    const r = s.stations.filter((x) => x.recording).length;
    setText(rec.v, r ? `${r} 局` : 'なし');
    setClass(rec.v, r ? 'v warnv' : 'v');
    if (s.audio.follow) {
      const tk = store.talkingStation();
      if (tk && tk.idx !== s.selected) store.select(tk.idx);
    }
    const m = modeOf(s);
    setClass(modeAll, m === 'all' ? 'btn sel' : 'btn');
    setClass(modeSel, m === 'sel' ? 'btn sel' : 'btn');
    setClass(modeFollow, m === 'follow' ? 'btn sel' : 'btn');
    if (m !== savedMode) {
      savedMode = m;
      try { localStorage.setItem(PREF_MODE, m); } catch {  }
    }
    const st = store.selectedStation();
    setText(scopeWhat, m === 'all' ? '全局を表示中' : (st ? `${st.mhz.toFixed(4)} MHz` : '-'));
    setText(time, hms(s.lastT));
    setText(date, ymd(s.lastT));
    setClass(clock, isEstimated() ? 'clock estimated' : 'clock');
  }

  store.on(TOPIC.conn, drawConn);
  store.on(TOPIC.meta, draw);
  store.on(TOPIC.selection, draw);
  setInterval(draw, UI_TICK_MS);
  setInterval(pollInputs, CLOCK_TICK_MS);
  pollInputs();
  setInterval(() => { setText(time, hms(store.state.lastT)); setText(date, ymd(store.state.lastT)); }, CLOCK_TICK_MS);
  drawConn(store.state);
}
