import { CLOCK_TICK_MS } from '../ui.js';
import { el, setText, setClass, syncList } from '../dom.js';

export function init(_store) {
  const body = document.getElementById('settings-body');
  const sub = document.getElementById('settings-sub');
  const boxes = el('div');
  const msg = el('div', { class: 'tiny muted' });
  body.append(boxes, msg);
  const blocks = new Map();

  function say(text, bad) {
    setText(msg, text);
    setClass(msg, bad ? 'tiny' : 'tiny muted');
    msg.style.color = bad ? 'var(--crit)' : '';
  }

  async function call(method, path, payload) {
    const opt = { method };
    if (payload !== undefined) {
      opt.headers = { 'Content-Type': 'application/json' };
      opt.body = JSON.stringify(payload);
    }
    const res = await fetch(path, opt);
    if (!res.ok) throw new Error((await res.text()).trim() || `HTTP ${res.status}`);
  }
  async function act(what, method, path, payload) {
    try {
      await call(method, path, payload);
      say(`${what}: 済`, false);
    } catch (e) {
      say(`${what}: ${e.message}`, true);
    }
    refresh();
  }

  function makeBlock(id) {
    const base = `api/inputs/${encodeURIComponent(id)}`;
    const title = el('b');
    const head = el('div', { class: 'row' }, [title]);
    const kv = el('dl', { class: 'kv' });
    const v = {};
    for (const [k, n] of [['input', '入力'], ['rate', '標本化'], ['center', '中心'], ['gain', '利得'], ['ctl', '操作']]) {
      kv.append(el('dt', { text: n }));
      v[k] = el('dd', k === 'input' ? { class: 'mono' } : {});
      kv.append(v[k]);
    }
    const num = (ph, step) => el('input', { type: 'number', step, placeholder: ph, class: 'btn mono', style: 'width:9em' });
    const tuneIn = num('中心 [MHz]', '0.0001');
    const tuneBtn = el('button', { class: 'btn', text: '同調' });
    const gainIn = num('利得 [dB]', '0.1');
    const agcIn = el('input', { type: 'checkbox' });
    const gainBtn = el('button', { class: 'btn', text: '利得を設定' });
    const addIn = num('局 [MHz]', '0.00625');
    const kindSel = el('select', { class: 'btn' });
    kindSel.style.display = 'none';
    const addBtn = el('button', { class: 'btn', text: '局を足す' });
    const list = el('div', { class: 'scroll' });
    const empty = el('div', { class: 'muted tiny', text: '監視している局はありません（上で足せます）' });
    const box = el('div', {}, [
      head, kv,
      el('div', { class: 'row' }, [tuneIn, tuneBtn, gainIn, el('label', { class: 'tiny' }, [agcIn, ' AGC']), gainBtn]),
      el('div', { class: 'row' }, [addIn, kindSel, addBtn]),
      list, empty,
    ]);
    const b = { id, box, head, title, v, tuneIn, tuneBtn, gainIn, agcIn, gainBtn, addIn, kindSel, addBtn, list, empty, first: true, label: '' };
    tuneBtn.addEventListener('click', () => {
      const mhz = Number(tuneIn.value);
      if (!(mhz > 0)) { say('中心の周波数 [MHz] を入れてください', true); return; }
      act(`${b.label}同調 ${mhz} MHz`, 'POST', `${base}/tune`, { center_mhz: mhz });
    });
    gainBtn.addEventListener('click', () => {
      act(`${b.label}利得`, 'POST', `${base}/gain`, { gain_db: Number(gainIn.value) || 0, agc: agcIn.checked });
    });
    addBtn.addEventListener('click', () => {
      const mhz = Number(addIn.value);
      if (!(mhz > 0)) { say('局の周波数 [MHz] を入れてください', true); return; }
      const payload = { mhz };
      if (kindSel.style.display !== 'none' && kindSel.value) payload.kind = kindSel.value;
      act(`${b.label}局 ${mhz} MHz を足す`, 'POST', `${base}/channels`, payload);
    });
    b.makeRow = () => {
      const row = el('div', { class: 'row' });
      const freq = el('span', { class: 'mono' });
      const idt = el('span', { class: 'tiny muted' });
      const rm = el('button', { class: 'btn', text: '外す' });
      rm.addEventListener('click', () => {
        const mhz = row._mhz;
        act(`${b.label}局 ${mhz} MHz を外す`, 'DELETE', `${base}/channels/${encodeURIComponent(mhz)}`);
      });
      row.append(freq, idt, rm);
      row._parts = { freq, id: idt, rm };
      return row;
    };
    return b;
  }

  function drawBlock(b, it, many) {
    b.label = many && it.label ? `${it.label}: ` : '';
    b.head.style.display = many ? '' : 'none';
    setText(b.title, it.label || it.id);
    const st = it.status;
    if (it.error || !st) {
      setText(b.v.ctl, it.error || '使えません');
      b.tuneBtn.disabled = b.gainBtn.disabled = b.addBtn.disabled = true;
      return;
    }
    const state = st.done ? '（受信終了）' : st.running ? '' : '（起動前）';
    setText(b.v.input, `${st.input || '-'}${state}`);
    setText(b.v.rate, st.sample_rate ? `${(st.sample_rate / 1e6).toFixed(3)} MS/s` : '-');
    setText(b.v.center, `${(st.center_hz / 1e6).toFixed(4)} MHz`);
    setText(b.v.gain, st.agc ? 'AGC' : `${st.gain_db.toFixed(1)} dB`);
    setText(b.v.ctl, st.controllable ? '同調・利得を変えられる' : 'この入力は同調・利得を変えられない（局の追加と削除はできる）');
    b.tuneBtn.disabled = b.gainBtn.disabled = !st.controllable || st.done;
    b.addBtn.disabled = st.done;
    const kinds = it.kinds || [];
    b.kindSel.style.display = kinds.length ? '' : 'none';
    if (kinds.length && b.kindSel.options.length !== kinds.length) {
      b.kindSel.replaceChildren(...kinds.map((k) => el('option', { value: k.key, text: k.label })));
    }
    if (b.first) {
      b.tuneIn.value = (st.center_hz / 1e6).toFixed(4);
      b.gainIn.value = st.gain_db.toFixed(1);
      b.agcIn.checked = !!st.agc;
      b.first = false;
    }
    const chans = st.channels || [];
    syncList(b.list, chans, (c) => String(c.mhz), b.makeRow, (node, c) => {
      node._mhz = c.mhz;
      setText(node._parts.freq, `${c.mhz.toFixed(4)} MHz`);
      setText(node._parts.id, c.id >= 0 ? `局 ${c.id}` : '');
      node._parts.rm.disabled = st.done;
    });
    b.empty.style.display = chans.length ? 'none' : '';
  }

  function draw(items) {
    if (!items || !items.length) { setText(sub, '使えません'); return; }
    const many = items.length > 1;
    const sts = items.map((x) => x.status).filter(Boolean);
    let state = '起動前';
    if (sts.length && sts.every((x) => x.done)) state = '受信終了';
    else if (sts.some((x) => x.running)) state = '受信中';
    setText(sub, many ? `${state}・入力 ${items.length} 本` : state);
    for (const it of items) {
      let b = blocks.get(it.id);
      if (!b) {
        b = makeBlock(it.id);
        blocks.set(it.id, b);
        boxes.appendChild(b.box);
      }
      drawBlock(b, it, many);
    }
  }

  async function refresh() {
    try {
      const res = await fetch('api/inputs');
      draw(res.ok ? await res.json() : null);
    } catch {
      draw(null);
    }
  }

  setInterval(refresh, CLOCK_TICK_MS);
  refresh();
}
