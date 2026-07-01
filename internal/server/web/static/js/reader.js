import { clampZoom, stepZoom, parsePercent, normFit } from './zoom.js';

const el = document.getElementById('reader');
const id = el.dataset.id;
const total = parseInt(el.dataset.pages, 10);
let cur = Math.min(parseInt(el.dataset.start, 10) || 0, Math.max(total - 1, 0));
const hashP = location.hash.match(/^#p=(\d+)$/);
if (hashP) cur = Math.max(0, Math.min(parseInt(hashP[1], 10), Math.max(total - 1, 0)));
let dir = el.dataset.dir || 'ltr';
let mode = el.dataset.mode || 'single';
let spreadStep = localStorage.getItem('spreadStep') === '1' ? 1 : 2;

const stage = document.querySelector('.reader-stage');
const counter = document.getElementById('counter');
const thumbsEl = document.getElementById('thumbs');
const stripEl = document.getElementById('thumbstrip');

// ---- view state: fit mode + zoom (global, localStorage) ----
let fit = normFit(localStorage.getItem('readerFit'));
let zoom = clampZoom(parseFloat(localStorage.getItem('readerZoom')));

function measureStage() {
  el.style.setProperty('--stage-h', stage.clientHeight + 'px');
}
function updateZoomLabel() {
  const input = document.getElementById('zoominput');
  if (input && document.activeElement !== input) input.value = String(Math.round(zoom * 100));
}
function applyView() {
  el.dataset.fit = fit;
  el.style.setProperty('--page-zoom', String(zoom));
  measureStage();
  const fl = document.getElementById('fitlabel');
  if (fl) fl.textContent = fit === 'width' ? '宽' : '高';
  updateZoomLabel();
  localStorage.setItem('readerFit', fit);
  localStorage.setItem('readerZoom', String(zoom));
}
function setZoom(z) { zoom = clampZoom(z); applyView(); }
function setFit(f) { fit = normFit(f); zoom = 1; applyView(); }
window.addEventListener('resize', measureStage);

// ---- view controls: fit toggle, zoom buttons, percent input, Ctrl+wheel ----
document.getElementById('zoomin').onclick = () => setZoom(stepZoom(zoom, 1));
document.getElementById('zoomout').onclick = () => setZoom(stepZoom(zoom, -1));
document.getElementById('fittoggle').onclick = () => setFit(fit === 'width' ? 'height' : 'width');
const zoomInput = document.getElementById('zoominput');
zoomInput.addEventListener('change', () => {
  setZoom(parsePercent(zoomInput.value, zoom));
  zoomInput.value = String(Math.round(zoom * 100)); // force-normalize the field on explicit commit
});
stage.addEventListener('wheel', (e) => {
  if (!e.ctrlKey) return;        // only Ctrl+wheel zooms; plain wheel scrolls
  e.preventDefault();            // suppress the browser's own ctrl-wheel zoom over the reader
  setZoom(stepZoom(zoom, e.deltaY < 0 ? 1 : -1));
}, { passive: false });

// ---- drag-to-pan an over-sized single/spread page (continuous scrolls on its own) ----
// Native trackpad/wheel/scrollbar already pan via the stage's overflow:auto; this
// adds grab-and-drag, and suppresses the edge page-turn click when a drag occurred.
let panning = false, panMoved = false, panX = 0, panY = 0, panSL = 0, panST = 0;
function stageOverflows() {
  return stage.scrollWidth > stage.clientWidth + 1 || stage.scrollHeight > stage.clientHeight + 1;
}
stage.addEventListener('pointerdown', (e) => {
  if (e.button !== 0 || !stageOverflows()) return;
  panning = true; panMoved = false;
  panX = e.clientX; panY = e.clientY; panSL = stage.scrollLeft; panST = stage.scrollTop;
  try { stage.setPointerCapture(e.pointerId); } catch (_) {}
  stage.classList.add('panning');
});
stage.addEventListener('pointermove', (e) => {
  if (!panning) return;
  const dx = e.clientX - panX, dy = e.clientY - panY;
  if (!panMoved && Math.hypot(dx, dy) > 4) panMoved = true;
  if (panMoved) { stage.scrollLeft = panSL - dx; stage.scrollTop = panST - dy; }
});
const endPan = (e) => {
  if (!panning) return;
  panning = false; stage.classList.remove('panning');
  try { stage.releasePointerCapture(e.pointerId); } catch (_) {}
};
stage.addEventListener('pointerup', endPan);
stage.addEventListener('pointercancel', endPan);
// capture-phase: a click that ends a drag must not reach the nav page-turn zones
stage.addEventListener('click', (e) => {
  if (panMoved) { e.stopPropagation(); e.preventDefault(); panMoved = false; }
}, true);

let contLazyObs = null, contPageObs = null, contScrollEl = null, contScrollFn = null;

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
// In spread mode cur is the left page of the shown pair; turning steps by
// spreadStep (1 or 2) with no re-alignment, so changing the step never moves
// the current view. goTo clamps to [0, total-1].
function advance() {
  if (mode === 'spread') goTo(cur + spreadStep);
  else goTo(cur + 1);
}
function back() {
  if (mode === 'spread') goTo(cur - spreadStep);
  else goTo(cur - 1);
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
  const pages = (cur + 1 < total) ? [cur, cur + 1] : [cur];
  const ordered = dir === 'rtl' ? pages.slice().reverse() : pages;
  const imgs = ordered.map((p) => `<img src="${pageURL(p)}" alt="page">`).join('');
  stage.innerHTML = '<button class="nav prev" id="prev" aria-label="上一页">‹</button><div class="spread">' + imgs + '</div><button class="nav next" id="next" aria-label="下一页">›</button>';
  bindZones(); preload(cur + 2);
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
  // Page tracking (counter + progress + strip highlight). Geometry-based so it
  // is robust to pages taller than the viewport — a visibility-ratio threshold
  // can never be met by such a page, which would freeze tracking. The current
  // page is the one straddling the container's top edge, else the topmost
  // intersecting page. The observer keeps `inter` (which pages are on screen);
  // a rAF-throttled scroll listener gives smooth updates within a tall page.
  const inter = new Set();
  function pickCurrent() {
    if (!inter.size) return;
    const ct = cont.getBoundingClientRect().top;
    let straddle = null, topmost = null, topmostY = Infinity;
    inter.forEach((n) => {
      const im = cont.querySelector(`.cpage[data-page="${n}"]`);
      if (!im) return;
      const r = im.getBoundingClientRect();
      if (r.top <= ct + 2 && r.bottom > ct + 2 && (straddle === null || n < straddle)) straddle = n;
      if (r.top < topmostY) { topmostY = r.top; topmost = n; }
    });
    const pick = straddle !== null ? straddle : topmost;
    if (pick === null || pick === cur) return;
    cur = pick;
    counter.textContent = `${cur + 1} / ${total}`;
    saveProgress(cur);
    updateStripActive();
  }
  let rafPending = false;
  const onContScroll = () => {
    if (rafPending) return;
    rafPending = true;
    requestAnimationFrame(() => { rafPending = false; pickCurrent(); });
  };
  contPageObs = new IntersectionObserver((es) => {
    es.forEach((e) => {
      const n = parseInt(e.target.dataset.page, 10);
      if (e.isIntersecting) inter.add(n); else inter.delete(n);
    });
    pickCurrent();
  }, { root: cont, threshold: [0, 0.5, 1] });
  cont.addEventListener('scroll', onContScroll, { passive: true });
  contScrollEl = cont; contScrollFn = onContScroll;
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
  if (contScrollEl && contScrollFn) { contScrollEl.removeEventListener('scroll', contScrollFn); contScrollEl = null; contScrollFn = null; }
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
  const t = e.target;
  const typing = t && (t.tagName === 'INPUT' || t.tagName === 'SELECT' || t.tagName === 'TEXTAREA');
  if (!typing) {
    if (e.key === '+' || e.key === '=') { setZoom(stepZoom(zoom, 1)); return; }
    if (e.key === '-') { setZoom(stepZoom(zoom, -1)); return; }
    if (e.key === '0') { setZoom(1); return; }
  }
  if (mode === 'continuous') return; // native scroll for arrows
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
  // do NOT re-render: changing the step keeps the current view; the new step
  // takes effect on the next page turn.
};

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
  measureStage(); // the strip changing height resizes the stage; keep fit=height accurate
};

// ---- back: return to the folder you came from (preserves its scroll/filters);
// fall back to the href (root) only when opened directly, not from in-app nav ----
document.getElementById('backlink').addEventListener('click', (e) => {
  let sameOrigin = false;
  try { sameOrigin = !!document.referrer && new URL(document.referrer).origin === location.origin; } catch (_) {}
  if (sameOrigin && history.length > 1) { e.preventDefault(); history.back(); }
});

// ---- init ----
applyDirLabel();
applyStepLabel();
applyStripCollapsed();
buildStrip();
if (total > 0) { setMode(mode); applyView(); } else counter.textContent = '无可显示页面';
