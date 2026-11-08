import { hms } from '../time.js';
import { el, setText, syncList } from '../dom.js';

const GSI = 'https://cyberjapandata.gsi.go.jp/xyz';
const ATTR = '<a href="https://maps.gsi.go.jp/development/ichiran.html">地理院タイル</a>';
const TRAIL_MAX = 30;

function color(key) {
  let h = 0;
  for (let i = 0; i < (key || '').length; i++) h = (h * 31 + key.charCodeAt(i)) & 0xffff;
  return `hsl(${h % 360} 70% 42%)`;
}

export function init(store) {
  const pane = document.getElementById('pane-datamap');
  const body = document.getElementById('datamap-body');
  const sub = document.getElementById('datamap-sub');
  if (!pane || !body) return;
  pane.style.display = 'none';

  const mapDiv = el('div', { class: 'mapview' });
  const btnFit = el('button', { class: 'mapbtn', text: '全体表示' });
  const list = el('div', { class: 'scroll datalist' });
  body.append(el('div', { class: 'maptools' }, [btnFit]), mapDiv, list);

  let map = null, layer = null;
  function ensureMap() {
    const L = window.L;
    if (map || !L) return map;
    map = L.map(mapDiv, { center: [36, 138], zoom: 5 });
    const pale = L.tileLayer(`${GSI}/pale/{z}/{x}/{y}.png`, { attribution: ATTR, maxZoom: 18 });
    const std = L.tileLayer(`${GSI}/std/{z}/{x}/{y}.png`, { attribution: ATTR, maxZoom: 18 });
    const photo = L.tileLayer(`${GSI}/seamlessphoto/{z}/{x}/{y}.jpg`, { attribution: ATTR, maxZoom: 18 });
    pale.addTo(map);
    L.control.layers({ '淡色地図': pale, '標準地図': std, '航空写真': photo }, {}, { collapsed: true }).addTo(map);
    layer = L.layerGroup().addTo(map);
    return map;
  }

  function points() { return (store.state.ext.position || []).filter((p) => p && p.pos); }

  function fitAll() {
    const pts = points().map((p) => [p.pos.lat, p.pos.lon]);
    if (!map || !pts.length) return;
    if (pts.length === 1) { map.setView(pts[0], 14); return; }
    map.fitBounds(pts, { padding: [30, 30], maxZoom: 15 });
  }
  btnFit.addEventListener('click', fitAll);

  function popup(p) {
    const box = el('div', { class: 'map-pop' });
    for (const s of [p.from, hms(p.t_sec), `${p.mhz.toFixed(4)} MHz`, p.label]) if (s) box.append(el('div', { text: s }));
    const dl = el('dl', { class: 'kv' });
    for (const r of p.rows || []) dl.append(el('dt', { text: r.name }), el('dd', { text: [r.bits, r.text].filter(Boolean).join('  ') }));
    box.append(dl);
    return box;
  }

  function drawMap(byFrom) {
    const L = window.L;
    if (!ensureMap()) { setText(sub, 'Leaflet を読み込めませんでした'); return; }
    layer.clearLayers();
    for (const [key, all] of byFrom) {
      const pts = all.filter((p) => p.pos).slice(-TRAIL_MAX);
      if (!pts.length) continue;
      const c = color(key);
      if (pts.length > 1) {
        const ll = pts.map((p) => [p.pos.lat, p.pos.lon]);
        L.polyline(ll, { color: '#ffffff', weight: 6, opacity: 0.9 }).addTo(layer);
        L.polyline(ll, { color: c, weight: 3, opacity: 0.9 }).addTo(layer);
      }
      const last = pts[pts.length - 1];
      L.circleMarker([last.pos.lat, last.pos.lon], { radius: 6, color: c, weight: 2, fillColor: c, fillOpacity: 0.85 })
        .bindTooltip(`${key} ${hms(last.t_sec)}`, { direction: 'top' })
        .bindPopup(popup(last), { maxWidth: 420 })
        .addTo(layer);
    }
    fitAll();
  }

  function makeRow() {
    const row = el('div', { class: 'row' });
    const t = el('span', { class: 'muted tiny' });
    const f = el('span', { class: 'badge idle' });
    const x = el('span', { class: 'grow tiny' });
    row.append(t, f, x);
    row._parts = { t, f, x };
    row.addEventListener('click', () => {
      const p = row._p;
      if (map && p && p.pos) map.setView([p.pos.lat, p.pos.lon], Math.max(map.getZoom(), 15));
    });
    return row;
  }

  function draw() {
    const all = store.state.ext.position || [];
    if (!all.length) return;
    pane.style.display = '';
    const byFrom = new Map();
    for (const p of all) {
      const k = p.from || `${p.mhz}`;
      if (!byFrom.has(k)) byFrom.set(k, []);
      byFrom.get(k).push(p);
    }
    const latest = [...byFrom.values()].map((l) => l[l.length - 1]).sort((a, b) => b.t_sec - a.t_sec);
    setText(sub, `送り元 ${byFrom.size} / 位置 ${all.length} 件`);
    syncList(list, latest, (p) => p.from || `${p.mhz}`, makeRow, (node, p) => {
      node._p = p;
      setText(node._parts.t, hms(p.t_sec));
      setText(node._parts.f, p.format);
      setText(node._parts.x, p.label);
    });
    try {
      if (points().length) drawMap(byFrom);
    } catch (e) {
      console.error('datamap', e);
    }
  }

  store.on('position', draw);
  draw();
}
