import { hms } from '../time.js';
import { el, setText, syncList } from '../dom.js';
import { resolveAll, halfWindow } from '../wrap.js';

const GSI = 'https://cyberjapandata.gsi.go.jp/xyz';
const ATTR = '<a href="https://maps.gsi.go.jp/development/ichiran.html">地理院タイル</a>';
const TRAIL_MAX = 30;
const REF_KEY = 'webui.datamap.ref';
const USER_MOVE_MS = 2000;

function color(key) {
  let h = 0;
  for (let i = 0; i < (key || '').length; i++) h = (h * 31 + key.charCodeAt(i)) & 0xffff;
  return `hsl(${h % 360} 70% 42%)`;
}

function loadRef() {
  try {
    const v = JSON.parse(localStorage.getItem(REF_KEY) || 'null');
    if (v && Number.isFinite(v.lat) && Number.isFinite(v.lon)) return { lat: v.lat, lon: v.lon, src: '前回の基準点' };
  } catch {  }
  return null;
}

function saveRef(r) {
  try { localStorage.setItem(REF_KEY, JSON.stringify({ lat: r.lat, lon: r.lon })); } catch {  }
}

export function init(store) {
  const pane = document.getElementById('pane-datamap');
  const body = document.getElementById('datamap-body');
  const sub = document.getElementById('datamap-sub');
  if (!pane || !body) return;
  pane.style.display = 'none';

  const mapDiv = el('div', { class: 'mapview' });
  const btnFit = el('button', { class: 'mapbtn', text: '全体表示' });
  const btnGPS = el('button', { class: 'mapbtn', text: '現在地', title: '基準点をブラウザの現在地にする' });
  const refText = el('span', { class: 'muted tiny maprefs' });
  btnGPS.style.display = 'none';
  const list = el('div', { class: 'scroll datalist' });
  body.append(el('div', { class: 'maptools' }, [btnFit, btnGPS, refText]), mapDiv, list);

  let map = null, layer = null, refLayer = null;
  let ref = loadRef();
  let autoFit = true;
  let userAt = 0;
  let askedGPS = false;
  let resolved = new Map();

  function setRef(lat, lon, src) {
    ref = { lat, lon, src };
    saveRef(ref);
    draw();
  }

  function askGPS(manual) {
    askedGPS = true;
    const g = navigator.geolocation;
    if (!g || !window.isSecureContext) {
      if (manual) setText(refText, '位置情報はこの開き方では使えません（localhost か https で開く）。地図を受信地へ動かしてください');
      return;
    }
    try {
      g.getCurrentPosition((p) => {
        autoFit = true;
        setRef(p.coords.latitude, p.coords.longitude, `現在地（±${Math.round(p.coords.accuracy)} m）`);
      }, () => {
        if (manual) setText(refText, '位置情報を使えませんでした。地図を受信地へ動かすと、その中心を基準点にします');
      }, { timeout: 10000, maximumAge: 600000 });
    } catch {  }
  }
  btnGPS.addEventListener('click', () => askGPS(true));

  function ensureMap() {
    const L = window.L;
    if (map || !L) return map;
    map = L.map(mapDiv, { center: ref ? [ref.lat, ref.lon] : [36, 138], zoom: ref ? 10 : 5 });
    const pale = L.tileLayer(`${GSI}/pale/{z}/{x}/{y}.png`, { attribution: ATTR, maxZoom: 18 });
    const std = L.tileLayer(`${GSI}/std/{z}/{x}/{y}.png`, { attribution: ATTR, maxZoom: 18 });
    const photo = L.tileLayer(`${GSI}/seamlessphoto/{z}/{x}/{y}.jpg`, { attribution: ATTR, maxZoom: 18 });
    pale.addTo(map);
    L.control.layers({ '淡色地図': pale, '標準地図': std, '航空写真': photo }, {}, { collapsed: true }).addTo(map);
    refLayer = L.layerGroup().addTo(map);
    layer = L.layerGroup().addTo(map);
    const touch = () => { userAt = Date.now(); };
    for (const ev of ['pointerdown', 'wheel', 'keydown', 'touchstart']) mapDiv.addEventListener(ev, touch, { passive: true });
    map.on('drag', touch);
    map.on('moveend', () => {
      if (Date.now() - userAt > USER_MOVE_MS) return;
      autoFit = false;
      if (hasWrap()) {
        const c = map.getCenter();
        setRef(c.lat, c.lng, '地図の中心');
      }
    });
    return map;
  }

  const all = () => store.state.ext.position || [];
  const hasWrap = () => all().some((p) => p && p.wrap);
  const posOf = (p) => (p.pos ? [p.pos.lat, p.pos.lon] : resolved.get(p) || null);
  const points = () => all().map(posOf).filter(Boolean);

  function resolve() {
    const ws = all().filter((p) => p && p.wrap && !p.pos);
    const ll = resolveAll(ws.map((p) => p.wrap), ref ? [ref.lat, ref.lon] : null);
    resolved = new Map();
    ws.forEach((p, i) => { if (ll[i]) resolved.set(p, ll[i]); });
    return ws;
  }

  function fitAll() {
    const pts = points();
    if (!map || !pts.length) return;
    if (pts.length === 1) { map.setView(pts[0], 14); return; }
    map.fitBounds(pts, { padding: [30, 30], maxZoom: 15 });
  }
  btnFit.addEventListener('click', () => { autoFit = true; fitAll(); });

  function popup(p) {
    const box = el('div', { class: 'map-pop' });
    for (const s of [p.from, hms(p.t_sec), `${p.mhz.toFixed(4)} MHz`, p.label]) if (s) box.append(el('div', { text: s }));
    const q = posOf(p);
    if (p.wrap && q && ref) box.append(el('div', { text: `${q[0].toFixed(5)}, ${q[1].toFixed(5)}（基準点 ${ref.src} から解いた写し）` }));
    const dl = el('dl', { class: 'kv' });
    for (const r of p.rows || []) dl.append(el('dt', { text: r.name }), el('dd', { text: [r.bits, r.text].filter(Boolean).join('  ') }));
    box.append(dl);
    return box;
  }

  function drawRef(wraps) {
    const L = window.L;
    refLayer.clearLayers();
    if (!wraps.length || !ref) return;
    const h = halfWindow(wraps[0].wrap);
    L.rectangle([[ref.lat - h, ref.lon - h], [ref.lat + h, ref.lon + h]],
      { color: '#4a5560', weight: 1.5, dashArray: '6 6', fill: false, interactive: false }).addTo(refLayer);
    L.circleMarker([ref.lat, ref.lon], { radius: 5, color: '#4a5560', weight: 2, fill: false })
      .bindTooltip(`基準点（${ref.src}）。点線の枠の中にいる車なら正しく読める`, { direction: 'top' }).addTo(refLayer);
  }

  function drawMap(byFrom, wraps) {
    const L = window.L;
    if (!ensureMap()) { setText(sub, 'Leaflet を読み込めませんでした'); return; }
    layer.clearLayers();
    drawRef(wraps);
    for (const [key, evs] of byFrom) {
      const pts = evs.filter(posOf).slice(-TRAIL_MAX);
      if (!pts.length) continue;
      const c = color(key);
      if (pts.length > 1) {
        const ll = pts.map(posOf);
        L.polyline(ll, { color: '#ffffff', weight: 6, opacity: 0.9 }).addTo(layer);
        L.polyline(ll, { color: c, weight: 3, opacity: 0.9 }).addTo(layer);
      }
      const last = pts[pts.length - 1];
      L.circleMarker(posOf(last), { radius: 6, color: c, weight: 2, fillColor: c, fillOpacity: 0.85 })
        .bindTooltip(`${key} ${hms(last.t_sec)}`, { direction: 'top' })
        .bindPopup(popup(last), { maxWidth: 420 })
        .addTo(layer);
    }
    if (autoFit) fitAll();
  }

  function makeRow() {
    const row = el('div', { class: 'row' });
    const t = el('span', { class: 'muted tiny' });
    const f = el('span', { class: 'badge idle' });
    const x = el('span', { class: 'grow tiny' });
    row.append(t, f, x);
    row._parts = { t, f, x };
    row.addEventListener('click', () => {
      const q = row._p && posOf(row._p);
      if (map && q) { autoFit = false; map.setView(q, Math.max(map.getZoom(), 15)); }
    });
    return row;
  }

  function draw() {
    const evs = all();
    if (!evs.length) return;
    pane.style.display = '';
    const wraps = resolve();
    if (wraps.length) {
      btnGPS.style.display = '';
      if (!askedGPS) askGPS(false);
      setText(refText, ref ? `基準点: ${ref.src} ${ref.lat.toFixed(3)}, ${ref.lon.toFixed(3)}（地図を動かすとその中心に付いて行く）`
        : '基準点がありません。「現在地」か、地図を受信地へ動かしてください（上位の桁を送らない位置は基準点から解く）');
    }
    const byFrom = new Map();
    for (const p of evs) {
      const k = p.from || `${p.mhz}`;
      if (!byFrom.has(k)) byFrom.set(k, []);
      byFrom.get(k).push(p);
    }
    const latest = [...byFrom.values()].map((l) => l[l.length - 1]).sort((a, b) => b.t_sec - a.t_sec);
    setText(sub, `送り元 ${byFrom.size} / 位置 ${evs.length} 件`);
    syncList(list, latest, (p) => p.from || `${p.mhz}`, makeRow, (node, p) => {
      node._p = p;
      setText(node._parts.t, hms(p.t_sec));
      setText(node._parts.f, p.format);
      setText(node._parts.x, p.label);
    });
    try {
      if (points().length || wraps.length) drawMap(byFrom, wraps);
    } catch (e) {
      console.error('datamap', e);
    }
  }

  store.on('position', draw);
  draw();
}
