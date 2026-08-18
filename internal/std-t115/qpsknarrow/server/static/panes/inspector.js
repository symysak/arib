import { hmsMilli, toDate } from '../time.js';
import { TOPIC, keyOf } from '../store.js';

const ROW_H = 20;

export function init(store) {
  const toolbar = document.getElementById('inspector-toolbar');
  const head = document.getElementById('inspector-head');
  const scroll = document.getElementById('inspector-scroll');
  const spacer = document.getElementById('inspector-spacer');
  const rowsEl = document.getElementById('inspector-rows');
  const detail = document.getElementById('inspector-detail');
  const sub = document.getElementById('inspector-sub');

  let follow = true;

  toolbar.innerHTML = `
    <input type="search" id="ins-q" placeholder="種別 / 生値 / 要約で絞り込み">
    <span class="sep"></span>
    <button class="btn sel" data-ch="all">全 ch</button>
    <button class="btn" data-ch="CCH">CCH</button>
    <button class="btn" data-ch="FACCH">FACCH</button>
    <button class="btn" data-ch="TCH">TCH</button>
    <span class="sep"></span>
    <label class="tiny"><input type="checkbox" id="ins-notify"> 通報開始のみ</label>
    <span class="sep"></span>
    <label class="tiny"><input type="checkbox" id="ins-follow" checked> 追従</label>
    <span class="sep"></span>
    <button class="btn" data-export="csv">CSV</button>
    <button class="btn" data-export="jsonl">JSONL</button>`;
  head.innerHTML = `<div class="ihead"><span>時刻</span><span>ch</span><span>種別 / 要約</span><span>型</span></div>`;

  toolbar.querySelector('#ins-q').addEventListener('input', (e) => {
    store.setFilter({ text: e.target.value });
  });
  toolbar.querySelectorAll('button[data-ch]').forEach((b) => {
    b.addEventListener('click', () => {
      toolbar.querySelectorAll('button[data-ch]').forEach((x) => x.classList.remove('sel'));
      b.classList.add('sel');
      store.setFilter({ channel: b.dataset.ch });
    });
  });
  toolbar.querySelector('#ins-notify').addEventListener('change', (e) => {
    store.setFilter({ onlyNotify: e.target.checked });
  });
  toolbar.querySelector('#ins-follow').addEventListener('change', (e) => {
    follow = e.target.checked;
    if (follow) renderRows();
  });
  toolbar.querySelectorAll('button[data-export]').forEach((b) => {
    b.addEventListener('click', () => exportRows(b.dataset.export));
  });
  scroll.addEventListener('scroll', () => renderRows());

  function exportRows(kind) {
    const rows = list();
    let text;
    if (kind === 'csv') {
      const head = ['実時刻', 't_sec', 'バースト', 'ch', '種別コード', '種別名', '要約',
        'BI', '製造者', 'CH切替', 'CH切替タイミング', 'AMR-WB+ SN', '生値hex'];
      const q = (v) => `"${String(v ?? '').replace(/"/g, '""')}"`;
      text = head.map(q).join(',') + '\n' + rows.map((c) => [
        toDate(c.t_sec).toISOString(), c.t_sec, c.kind, c.channel,
        '0x' + Number(c.msg_type).toString(16).padStart(2, '0').toUpperCase(),
        c.msg_type_name, c.summary, c.busy ? 1 : 0, c.mfr_name,
        c.ch_switch_to_sc ? 1 : 0, c.ch_switch_timing,
        c.amr_sn >= 0 ? c.amr_sn : '', c.raw_hex,
      ].map(q).join(',')).join('\n');
    } else {
      // JSONL はサーバが持っている全項目 + ビット分解を 1 行 1 件で出す。
      text = rows.map((c) => JSON.stringify({
        wall_ms: toDate(c.t_sec).getTime(), ...c,
      })).join('\n');
    }
    const st = toDate(store.state.lastT)
      .toISOString().replace(/[-:]/g, '').replace(/\..+$/, '');
    download(`t115_qpsknarrow_messages_${st}.${kind === 'csv' ? 'csv' : 'jsonl'}`, text);
  }

  function download(name, text) {
    const blob = new Blob([text], { type: 'text/plain;charset=utf-8' });
    const a = document.createElement('a');
    a.href = URL.createObjectURL(blob);
    a.download = name;
    a.click();
    setTimeout(() => URL.revokeObjectURL(a.href), 1000);
  }

  function list() { return store.filteredControls(); }

  function renderRows() {
    const items = list();
    sub.textContent = `${items.length} / ${store.state.controls.length} 件`;
    spacer.style.height = `${items.length * ROW_H}px`;

    if (follow) scroll.scrollTop = spacer.scrollHeight; // 最新へ
    const view = scroll.clientHeight || 330;
    const first = Math.max(0, Math.floor(scroll.scrollTop / ROW_H) - 4);
    const count = Math.ceil(view / ROW_H) + 8;
    const slice = items.slice(first, first + count);
    rowsEl.style.transform = `translateY(${first * ROW_H}px)`;
    rowsEl.innerHTML = slice.map((c) => {
      const k = keyOf(c);
      const t = `0x${c.msg_type.toString(16).toUpperCase().padStart(2, '0')}`;
      return `<div class="irow${k === store.state.selectedKey ? ' sel' : ''}" data-k="${k}"
        style="height:${ROW_H}px">
        <span>${hmsMilli(c.t_sec)}</span>
        <span>${c.channel}</span>
        <span class="ell">${c.msg_type_name}${c.summary ? ' · ' + escapeHTML(c.summary) : ''}</span>
        <span>${t}</span></div>`;
    }).join('');
    rowsEl.querySelectorAll('.irow').forEach((r) => {
      r.addEventListener('click', () => {
        follow = false;
        const cb = toolbar.querySelector('#ins-follow');
        if (cb) cb.checked = false;
        store.select(r.dataset.k);   // 通知は store 経由（他ペインも追従できる）
      });
    });
  }

  function renderDetail() {
    const c = store.selectedMessage();
    if (!c) {
      detail.innerHTML = '<p class="muted">左の一覧から行を選ぶと、ここにビット割当を展開します。</p>';
      return;
    }
    const b = c.bits || {};
    const pay = new Set(b.payload_oct || []);

    let html = `<p style="margin:0 0 4px">
      <b>${hmsMilli(c.t_sec)}</b>
      <span class="pill info">${b.channel || c.channel}</span>
      <span class="pill">0x${c.msg_type.toString(16).toUpperCase().padStart(2, '0')}</span>
      ${escapeHTML(c.msg_type_name)}
      ${c.busy ? '<span class="pill bad">BUSY</span>' : ''}
    </p>
    <p class="tiny muted" style="margin:0 0 4px">
      情報部 ${b.octets || 0} オクテット（${(b.octets || 0) * 8}bit）· 受信 ${b.received || 0} オクテット</p>`;

    html += '<div class="octets">' + (b.raw || []).map((v, i) => (
      `<span class="oct${pay.has(i + 1) ? ' payload' : ''}"><i>${i + 1}</i>${
        v.toString(16).toUpperCase().padStart(2, '0')}</span>`
    )).join('') + '</div>';

    html += rows('共通ヘッダ（§4.2.3.1）', b.header || []);
    if (b.has_body) {
      html += rows('本体（オクテット 4 以降）', b.body || []);
    } else {
      html += '<p class="muted tiny">この種別の本体ビットマップは未登録です（上の生値を参照）。</p>';
    }
    if (b.id) {
      html += `<p class="tiny" style="margin:6px 0 0">
        ${escapeHTML(b.id.name)} = <b>${b.id.value}</b>
        ${b.id.simultaneous
          ? '<span class="pill info">一斉（16bit 全 0）</span>'
          : '<span class="pill">群/個別</span>'}</p>`;
    }
    detail.innerHTML = html;
  }

  // rows は Go が展開した行をそのまま表にする（値の解釈はしない）。
  function rows(title, fields) {
    let h = `<h3 class="tiny" style="margin:8px 0 3px;color:var(--ink-3)">${title}</h3>`;
    h += '<div class="hscroll"><table class="fields"><thead><tr><th>oct</th><th>bit</th>'
      + '<th>フィールド</th><th>値</th><th>2進</th><th>意味 / 備考</th></tr></thead><tbody>';
    for (const f of fields) {
      if (f.collapsed) {
        h += `<tr class="spare"><td class="num">${f.oct}-${f.oct_to}</td><td>${f.bits}</td>`
          + `<td class="nm">${escapeHTML(f.name)}</td><td class="num">-</td><td>-</td>`
          + `<td class="mean">${f.collapsed_bits} bit</td></tr>`;
        continue;
      }
      const mean = f.meaning
        ? escapeHTML(f.meaning)
        : (f.note ? `<span class="muted">${escapeHTML(f.note)}</span>` : '');
      h += `<tr${f.spare ? ' class="spare"' : ''}><td class="num">${f.oct}</td><td>${f.bits}</td>`
        + `<td class="nm">${escapeHTML(f.name)}</td><td class="num">${f.value}</td>`
        + `<td>${f.bin}</td><td class="mean">${mean}</td></tr>`;
    }
    return h + '</tbody></table></div>';
  }

  const escapeHTML = (s) => String(s).replace(/[&<>]/g,
    (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;' }[c]));

  const redraw = () => { renderRows(); renderDetail(); };
  store.on(TOPIC.control, redraw);
  store.on(TOPIC.meta, redraw);
  store.on(TOPIC.filter, redraw);
  store.on(TOPIC.selection, redraw);
  window.addEventListener('resize', renderRows);
  redraw();
}
