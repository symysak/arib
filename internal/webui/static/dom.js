
export function el(tag, attrs = {}, children = []) {
  const e = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (v === undefined || v === null) continue;
    if (k === 'class') e.className = v;
    else if (k === 'text') e.textContent = v;
    else if (k === 'html') e.innerHTML = v;
    else if (k.startsWith('on')) e.addEventListener(k.slice(2), v);
    else e.setAttribute(k, v);
  }
  for (const c of [].concat(children)) {
    if (c) e.appendChild(typeof c === 'string' ? document.createTextNode(c) : c);
  }
  return e;
}

export function setText(e, v) {
  const s = String(v ?? '');
  if (e.textContent !== s) e.textContent = s;
}

export function setClass(e, v) {
  if (e.className !== v) e.className = v;
}

export function syncList(container, items, keyOf, create, update) {
  const have = container._byKey || (container._byKey = new Map());
  const seen = new Set();
  let prev = null;
  for (const item of items) {
    const key = keyOf(item);
    seen.add(key);
    let node = have.get(key);
    if (!node) {
      node = create(item);
      node.dataset.key = key;
      have.set(key, node);
    }
    if (update) update(node, item);
    const want = prev ? prev.nextSibling : container.firstChild;
    if (node !== want) container.insertBefore(node, want);
    prev = node;
  }
  for (const [key, node] of have) {
    if (!seen.has(key)) {
      node.remove();
      have.delete(key);
    }
  }
}
