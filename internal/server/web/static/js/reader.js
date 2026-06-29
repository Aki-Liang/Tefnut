const el = document.getElementById('reader');
const id = el.dataset.id;
const total = parseInt(el.dataset.pages, 10);
let cur = Math.min(parseInt(el.dataset.start, 10) || 0, Math.max(total - 1, 0));
const img = document.getElementById('page');
const counter = document.getElementById('counter');

let dir = el.dataset.dir || 'ltr';

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
  updateStripActive();
}

function advance() { show(cur + 1); }
function back() { show(cur - 1); }

function bindControls() {
  // bottom bar buttons are LOGICAL (上一页 = back, 下一页 = advance) regardless of direction
  document.getElementById('nextbtn').onclick = advance;
  document.getElementById('prevbtn').onclick = back;
  // side click zones are PHYSICAL and flip with direction
  if (dir === 'rtl') {
    document.getElementById('prev').onclick = advance; // left zone advances
    document.getElementById('next').onclick = back;    // right zone goes back
  } else {
    document.getElementById('prev').onclick = back;
    document.getElementById('next').onclick = advance;
  }
}
bindControls();

document.addEventListener('keydown', (e) => {
  if (e.key === 'ArrowRight') { dir === 'rtl' ? back() : advance(); }
  if (e.key === 'ArrowLeft')  { dir === 'rtl' ? advance() : back(); }
});

// --- thumbnail strip ---
const thumbsEl = document.getElementById('thumbs');
const stripEl = document.getElementById('thumbstrip');

const thumbObserver = new IntersectionObserver((entries) => {
  entries.forEach((entry) => {
    if (entry.isIntersecting) {
      const img = entry.target;
      if (!img.src && img.dataset.src) img.src = img.dataset.src;
      thumbObserver.unobserve(img);
    }
  });
}, { root: stripEl });

function buildStrip() {
  thumbsEl.innerHTML = '';
  thumbsEl.classList.toggle('rtl', dir === 'rtl');
  for (let i = 0; i < total; i++) {
    const fig = document.createElement('div');
    fig.className = 'thumb';
    fig.dataset.page = i;
    const timg = document.createElement('img');
    timg.dataset.src = `/api/comics/${id}/pages/${i}/thumb`;
    timg.alt = `第 ${i + 1} 页`;
    fig.appendChild(timg);
    fig.onclick = () => show(i);
    thumbsEl.appendChild(fig);
    thumbObserver.observe(timg);
  }
}

function updateStripActive() {
  const figs = thumbsEl.children;
  for (let i = 0; i < figs.length; i++) figs[i].classList.toggle('active', i === cur);
  const active = figs[cur];
  if (active) active.scrollIntoView({ inline: 'center', block: 'nearest', behavior: 'smooth' });
}

// --- direction toggle ---
function applyDirLabel() { document.getElementById('dirlabel').textContent = dir.toUpperCase(); }

document.getElementById('dirtoggle').onclick = () => {
  const next = dir === 'ltr' ? 'rtl' : 'ltr';
  fetch(`/api/comics/${id}`, {
    method: 'PATCH', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ readingDirection: next })
  }).then(r => {
    if (!r.ok) { alert('切换方向失败'); return; }
    dir = next;
    el.dataset.dir = next;
    applyDirLabel();
    bindControls();
    buildStrip();
    updateStripActive();
  }).catch(() => alert('切换方向失败'));
};

// --- strip collapse ---
let stripCollapsed = localStorage.getItem('stripCollapsed') === '1';
function applyStripCollapsed() {
  stripEl.classList.toggle('collapsed', stripCollapsed);
  document.getElementById('stripToggle').textContent = stripCollapsed ? '▲' : '▼';
}
document.getElementById('stripToggle').onclick = () => {
  stripCollapsed = !stripCollapsed;
  localStorage.setItem('stripCollapsed', stripCollapsed ? '1' : '0');
  applyStripCollapsed();
};

applyDirLabel();
applyStripCollapsed();
buildStrip();

if (total > 0) show(cur); else counter.textContent = '无可显示页面';

// --- metadata editing ---
const authorInput = document.getElementById('author');
const ratingSel = document.getElementById('rating');
const tagsBox = document.getElementById('tags');

function patchMeta(payload) {
  fetch(`/api/comics/${id}`, {
    method: 'PATCH', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload)
  }).then(r => {
    if (!r.ok) alert('保存失败');
  }).catch(() => alert('保存失败'));
}
authorInput.addEventListener('change', () => patchMeta({ author: authorInput.value }));
ratingSel.addEventListener('change', () => patchMeta({ rating: parseInt(ratingSel.value, 10) }));

function renderTags(tags) {
  tagsBox.innerHTML = '';
  (tags || []).forEach(t => {
    const span = document.createElement('span');
    span.className = 'tag';
    span.textContent = t.name + ' ';
    const x = document.createElement('button');
    x.textContent = '×';
    x.onclick = () => fetch(`/api/comics/${id}/tags/${t.id}`, { method: 'DELETE' })
      .then(r => { if (!r.ok) { alert('删除标签失败'); return; } loadDetail(); })
      .catch(() => alert('删除标签失败'));
    span.appendChild(x);
    tagsBox.appendChild(span);
  });
}

function loadDetail() {
  fetch(`/api/comics/${id}`).then(r => r.json()).then(j => renderTags(j.data.tags)).catch(() => {});
}

document.getElementById('addtag').addEventListener('submit', (e) => {
  e.preventDefault();
  const input = document.getElementById('newtag');
  const name = input.value.trim();
  if (!name) return;
  fetch(`/api/comics/${id}/tags`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name })
  }).then(r => {
    if (!r.ok) { alert('添加标签失败'); return; }
    input.value = '';
    loadDetail();
  }).catch(() => alert('添加标签失败'));
});

loadDetail();
