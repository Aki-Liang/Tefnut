const el = document.getElementById('reader');
const id = el.dataset.id;
const total = parseInt(el.dataset.pages, 10);
let cur = Math.min(parseInt(el.dataset.start, 10) || 0, Math.max(total - 1, 0));
let dir = el.dataset.dir || 'ltr';
let mode = el.dataset.mode || 'single';
let spreadStep = localStorage.getItem('spreadStep') === '1' ? 1 : 2;

const stage = document.querySelector('.reader-stage');
const counter = document.getElementById('counter');
const thumbsEl = document.getElementById('thumbs');
const stripEl = document.getElementById('thumbstrip');

let contLazyObs = null, contPageObs = null;

function pageURL(n) { return `/api/comics/${id}/pages/${n}`; }
function clampPage(n) { return Math.max(0, Math.min(n, total - 1)); }
function preload(n) { if (n >= 0 && n < total) { const i = new Image(); i.src = pageURL(n); } }

// ---- progress ----
let saveTimer = null;
function saveProgress(n) {
  clearTimeout(saveTimer);
  saveTimer = setTimeout(() => {
    fetch(`/api/comics/${id}/progress`, {
      method: 'PUT', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ page: n })
    }).catch(() => {});
  }, 400);
}

// ---- thumbnail strip (mode-agnostic) ----
const thumbObserver = new IntersectionObserver((entries) => {
  entries.forEach((e) => {
    if (e.isIntersecting) { const im = e.target; if (!im.src && im.dataset.src) im.src = im.dataset.src; thumbObserver.unobserve(im); }
  });
}, { root: stripEl });

function buildStrip() {
  thumbsEl.innerHTML = '';
  thumbsEl.classList.toggle('rtl', dir === 'rtl');
  for (let i = 0; i < total; i++) {
    const fig = document.createElement('div'); fig.className = 'thumb'; fig.dataset.page = i;
    const im = document.createElement('img'); im.dataset.src = `/api/comics/${id}/pages/${i}/thumb`; im.alt = `第 ${i + 1} 页`;
    fig.appendChild(im); fig.onclick = () => goTo(i); thumbsEl.appendChild(fig); thumbObserver.observe(im);
  }
}
function updateStripActive() {
  const figs = thumbsEl.children;
  for (let i = 0; i < figs.length; i++) figs[i].classList.toggle('active', i === cur);
  const a = figs[cur]; if (a) a.scrollIntoView({ inline: 'center', block: 'nearest', behavior: 'smooth' });
}

// ---- navigation (mode-aware) ----
function spreadStart(n) { if (n <= 0) return 0; return n % 2 === 1 ? n : n - 1; } // [0],[1,2],[3,4]...
function advance() {
  if (mode === 'spread') {
    if (spreadStep === 1) { if (cur + 1 < total) goTo(cur + 1); }
    else { const ns = cur === 0 ? 1 : cur + 2; if (ns < total) goTo(ns); }
  } else { goTo(cur + 1); }
}
function back() {
  if (mode === 'spread') {
    if (spreadStep === 1) { if (cur > 0) goTo(cur - 1); }
    else { goTo(cur <= 1 ? 0 : cur - 2); }
  } else { goTo(cur - 1); }
}

function bindZones() {
  const prev = document.getElementById('prev'), next = document.getElementById('next');
  if (!prev || !next) return;
  if (dir === 'rtl') { prev.onclick = advance; next.onclick = back; }
  else { prev.onclick = back; next.onclick = advance; }
}

// ---- per-mode rendering ----
function renderSingle() {
  stage.innerHTML = '<button class="nav prev" id="prev" aria-label="上一页">‹</button><img id="page" alt="page"><button class="nav next" id="next" aria-label="下一页">›</button>';
  document.getElementById('page').src = pageURL(cur);
  bindZones(); preload(cur + 1);
}
function renderSpread() {
  let start = spreadStep === 2 ? spreadStart(cur) : cur;
  cur = start;
  let pages;
  if (spreadStep === 2 && start === 0) pages = [0];
  else pages = (start + 1 < total) ? [start, start + 1] : [start];
  const ordered = dir === 'rtl' ? pages.slice().reverse() : pages;
  const imgs = ordered.map((p) => `<img src="${pageURL(p)}" alt="page">`).join('');
  stage.innerHTML = '<button class="nav prev" id="prev" aria-label="上一页">‹</button><div class="spread">' + imgs + '</div><button class="nav next" id="next" aria-label="下一页">›</button>';
  bindZones(); preload(start + 2);
}
function buildContinuous() {
  stage.innerHTML = '<div class="continuous" id="cont"></div>';
  const cont = document.getElementById('cont');
  for (let i = 0; i < total; i++) {
    const im = document.createElement('img'); im.className = 'cpage'; im.dataset.page = i;
    im.dataset.src = pageURL(i); im.alt = `第 ${i + 1} 页`;
    cont.appendChild(im);
  }
  contLazyObs = new IntersectionObserver((es) => {
    es.forEach((e) => { if (e.isIntersecting) { const im = e.target; if (!im.src && im.dataset.src) { im.src = im.dataset.src; contLazyObs.unobserve(im); } } });
  }, { root: cont, rootMargin: '300px' });
  const visible = new Set();
  contPageObs = new IntersectionObserver((es) => {
    es.forEach((e) => {
      const n = parseInt(e.target.dataset.page, 10);
      if (e.isIntersecting) visible.add(n); else visible.delete(n);
    });
    if (!visible.size) return;
    const top = Math.min(...visible);
    if (top === cur) return;
    cur = top;
    counter.textContent = `${cur + 1} / ${total}`;
    saveProgress(cur);
    updateStripActive();
  }, { root: cont, threshold: 0.5 });
  cont.querySelectorAll('.cpage').forEach((im) => { contLazyObs.observe(im); contPageObs.observe(im); });
}
function scrollToPage(n) {
  const im = document.querySelector(`.cpage[data-page="${n}"]`);
  if (im) im.scrollIntoView({ behavior: 'auto', block: 'start' });
}

// ---- universal "go to logical page" ----
function goTo(page) {
  cur = clampPage(page);
  if (mode === 'single') renderSingle();
  else if (mode === 'spread') renderSpread();
  else { scrollToPage(cur); }
  counter.textContent = `${cur + 1} / ${total}`;
  saveProgress(cur);
  updateStripActive();
}

function teardownContinuous() {
  if (contLazyObs) { contLazyObs.disconnect(); contLazyObs = null; }
  if (contPageObs) { contPageObs.disconnect(); contPageObs = null; }
}

function setMode(m) {
  teardownContinuous();
  mode = m; el.dataset.mode = m;
  document.getElementById('dirtoggle').style.display = (m === 'continuous') ? 'none' : '';
  document.getElementById('stepbtn').style.display = (m === 'spread') ? '' : 'none';
  if (m === 'continuous') { buildContinuous(); scrollToPage(cur); counter.textContent = `${cur + 1} / ${total}`; updateStripActive(); }
  else goTo(cur);
}

// ---- bottom-bar buttons (logical: 下一页 = advance, 上一页 = back) ----
document.getElementById('nextbtn').onclick = advance;
document.getElementById('prevbtn').onclick = back;

// ---- keyboard ----
document.addEventListener('keydown', (e) => {
  if (mode === 'continuous') return; // native scroll
  if (e.key === 'ArrowRight') { dir === 'rtl' ? back() : advance(); }
  if (e.key === 'ArrowLeft') { dir === 'rtl' ? advance() : back(); }
});

// ---- direction toggle ----
function applyDirLabel() { document.getElementById('dirlabel').textContent = dir.toUpperCase(); }
document.getElementById('dirtoggle').onclick = () => {
  const next = dir === 'ltr' ? 'rtl' : 'ltr';
  fetch(`/api/comics/${id}`, { method: 'PATCH', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ readingDirection: next }) })
    .then((r) => { if (!r.ok) { alert('切换方向失败'); return; } dir = next; applyDirLabel(); buildStrip(); goTo(cur); })
    .catch(() => alert('切换方向失败'));
};

// ---- display-mode toggle ----
const modeToggle = document.getElementById('modetoggle');
modeToggle.value = mode;
modeToggle.onchange = () => {
  const next = modeToggle.value;
  fetch(`/api/comics/${id}`, { method: 'PATCH', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ displayMode: next }) })
    .then((r) => { if (!r.ok) { alert('切换展示方式失败'); modeToggle.value = mode; return; } setMode(next); })
    .catch(() => { alert('切换展示方式失败'); modeToggle.value = mode; });
};

// ---- spread page-step toggle ----
function applyStepLabel() { document.getElementById('steplabel').textContent = spreadStep === 1 ? '一页' : '两页'; }
document.getElementById('stepbtn').onclick = () => {
  spreadStep = spreadStep === 2 ? 1 : 2;
  localStorage.setItem('spreadStep', String(spreadStep));
  applyStepLabel();
  if (mode === 'spread') goTo(cur);
};

// ---- metadata editing ----
const authorInput = document.getElementById('author');
const ratingSel = document.getElementById('rating');
const tagsBox = document.getElementById('tags');
function patchMeta(payload) {
  fetch(`/api/comics/${id}`, { method: 'PATCH', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload) })
    .then((r) => { if (!r.ok) alert('保存失败'); }).catch(() => alert('保存失败'));
}
authorInput.addEventListener('change', () => patchMeta({ author: authorInput.value }));
ratingSel.addEventListener('change', () => patchMeta({ rating: parseInt(ratingSel.value, 10) }));
function renderTags(tags) {
  tagsBox.innerHTML = '';
  (tags || []).forEach((t) => {
    const span = document.createElement('span'); span.className = 'tag'; span.textContent = t.name + ' ';
    const x = document.createElement('button'); x.textContent = '×';
    x.onclick = () => fetch(`/api/comics/${id}/tags/${t.id}`, { method: 'DELETE' }).then((r) => { if (!r.ok) { alert('删除标签失败'); return; } loadDetail(); }).catch(() => alert('删除标签失败'));
    span.appendChild(x); tagsBox.appendChild(span);
  });
}
function loadDetail() {
  fetch(`/api/comics/${id}`).then((r) => r.json()).then((j) => renderTags(j.data.tags)).catch(() => {});
}
document.getElementById('addtag').addEventListener('submit', (e) => {
  e.preventDefault();
  const input = document.getElementById('newtag'); const name = input.value.trim();
  if (!name) return;
  fetch(`/api/comics/${id}/tags`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name }) })
    .then((r) => { if (!r.ok) { alert('添加标签失败'); return; } input.value = ''; loadDetail(); }).catch(() => alert('添加标签失败'));
});

// ---- strip collapse ----
let stripCollapsed = localStorage.getItem('stripCollapsed') === '1';
function applyStripCollapsed() {
  stripEl.classList.toggle('collapsed', stripCollapsed);
  document.getElementById('stripToggle').title = stripCollapsed ? '展开预览' : '收起预览';
}
document.getElementById('stripToggle').onclick = () => {
  stripCollapsed = !stripCollapsed;
  localStorage.setItem('stripCollapsed', stripCollapsed ? '1' : '0');
  applyStripCollapsed();
};

// ---- init ----
applyDirLabel();
applyStepLabel();
applyStripCollapsed();
buildStrip();
loadDetail();
if (total > 0) setMode(mode); else counter.textContent = '无可显示页面';
