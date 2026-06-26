const el = document.getElementById('reader');
const id = el.dataset.id;
const total = parseInt(el.dataset.pages, 10);
let cur = Math.min(parseInt(el.dataset.start, 10) || 0, Math.max(total - 1, 0));
const img = document.getElementById('page');
const counter = document.getElementById('counter');

function pageURL(n) { return `/api/comics/${id}/pages/${n}`; }

function preload(n) {
  if (n >= 0 && n < total) { const i = new Image(); i.src = pageURL(n); }
}

let saveTimer = null;
function saveProgress(n) {
  clearTimeout(saveTimer);
  saveTimer = setTimeout(() => {
    fetch(`/api/comics/${id}/progress`, {
      method: 'PUT', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ page: n })
    });
  }, 400);
}

function show(n) {
  if (n < 0 || n >= total) return;
  cur = n;
  img.src = pageURL(cur);
  counter.textContent = `${cur + 1} / ${total}`;
  preload(cur + 1);
  saveProgress(cur);
}

document.getElementById('next').onclick = () => show(cur + 1);
document.getElementById('prev').onclick = () => show(cur - 1);
document.addEventListener('keydown', (e) => {
  if (e.key === 'ArrowRight') show(cur + 1);
  if (e.key === 'ArrowLeft') show(cur - 1);
});

if (total > 0) show(cur); else counter.textContent = '无可显示页面';
