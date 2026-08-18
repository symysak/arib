import { hms } from '../time.js';
import { TOPIC } from '../store.js';

export function init(store) {
  const pre = document.getElementById('log-body');
  const sub = document.getElementById('log-sub');
  const filters = document.getElementById('log-filters');

  filters.querySelectorAll('input[data-group]').forEach((cb) => {
    cb.addEventListener('change', () => store.setLogGroup(cb.dataset.group, cb.checked));
  });

  const render = () => {
    const logs = store.filteredLogs();
    sub.textContent = `${logs.length} / ${store.state.logs.length} 行`;
    pre.innerHTML = logs.map((l) =>
      `<span class="t">${hms(l.t_sec)}</span> <span class="${l.level}">${escape(l.text)}</span>`
    ).join('\n');
    pre.scrollTop = pre.scrollHeight;
  };
  const escape = (s) => s.replace(/[&<>]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;' }[c]));

  store.on(TOPIC.log, render);
  store.on(TOPIC.filter, render);
  store.on(TOPIC.meta, render);
  render();
}
