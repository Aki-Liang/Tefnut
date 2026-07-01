const el = document.querySelector('.detail');
const id = el.dataset.id;
let rating = parseInt(el.dataset.rating, 10) || 0;

// back: return to where you came from, else fall back to the href
document.getElementById('backlink').addEventListener('click', (e) => {
  let sameOrigin = false;
  try { sameOrigin = !!document.referrer && new URL(document.referrer).origin === location.origin; } catch (_) {}
  if (sameOrigin && history.length > 1) { e.preventDefault(); history.back(); }
});

function patchMeta(payload) {
  return fetch(`/api/comics/${id}`, {
    method: 'PATCH', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  }).then((r) => { if (!r.ok) throw new Error('save'); });
}

// rating stars: click n sets n; click the current rating clears to 0
const starsBox = document.getElementById('d-stars');
function renderStars() {
  starsBox.innerHTML = '';
  for (let i = 1; i <= 5; i++) {
    const s = document.createElement('button');
    s.type = 'button';
    s.className = 'star' + (i <= rating ? ' on' : '');
    s.textContent = i <= rating ? '★' : '☆';
    s.addEventListener('click', () => {
      const next = rating === i ? 0 : i;
      patchMeta({ rating: next }).then(() => { rating = next; renderStars(); }).catch(() => alert('保存失败'));
    });
    starsBox.appendChild(s);
  }
}
renderStars();

// author
const author = document.getElementById('d-author');
author.addEventListener('change', () => patchMeta({ author: author.value }).catch(() => alert('保存失败')));

// tags: delete via event delegation; add appends a chip from the POST response
const tagsBox = document.getElementById('d-tags');
function makeChip(t) {
  const span = document.createElement('span');
  span.className = 'tag'; span.dataset.id = t.id;
  span.append(document.createTextNode(t.name + ' '));
  const x = document.createElement('button');
  x.className = 'del'; x.type = 'button'; x.setAttribute('aria-label', '删除'); x.textContent = '×';
  span.appendChild(x);
  return span;
}
tagsBox.addEventListener('click', (e) => {
  if (!e.target.classList.contains('del')) return;
  const chip = e.target.closest('.tag');
  fetch(`/api/comics/${id}/tags/${chip.dataset.id}`, { method: 'DELETE' })
    .then((r) => { if (!r.ok) throw new Error(); chip.remove(); })
    .catch(() => alert('删除标签失败'));
});
// ---- themed tag combobox: filter existing tags, add on pick/Enter ----
const newtag = document.getElementById('d-newtag');
const suggest = document.getElementById('d-suggest');
let allTags = [];      // [{id,name}] of existing tags
let sugItems = [];     // current filtered list
let sugActive = -1;
fetch('/api/tags').then((r) => r.json()).then((j) => { allTags = j.data || []; }).catch(() => {});

function isAttached(t) { return !!tagsBox.querySelector(`.tag[data-id="${t.id}"]`); }
function closeSuggest() { suggest.hidden = true; suggest.innerHTML = ''; sugItems = []; sugActive = -1; newtag.setAttribute('aria-expanded', 'false'); }
function setActive(i) { sugActive = i; [...suggest.children].forEach((c, j) => c.classList.toggle('active', j === i)); }
function renderSuggest() {
  const q = newtag.value.trim().toLowerCase();
  sugItems = !q ? [] : allTags.filter((t) => t.name.toLowerCase().includes(q) && !isAttached(t)).slice(0, 8);
  if (!sugItems.length) { closeSuggest(); return; }
  suggest.innerHTML = '';
  sugItems.forEach((t, i) => {
    const o = document.createElement('div');
    o.className = 'dd-option'; o.setAttribute('role', 'option'); o.textContent = t.name;
    o.addEventListener('mousemove', () => setActive(i));
    o.addEventListener('mousedown', (e) => { e.preventDefault(); addTag(t.name); }); // mousedown beats input blur
    suggest.appendChild(o);
  });
  sugActive = -1;
  suggest.hidden = false; newtag.setAttribute('aria-expanded', 'true');
}
function addTag(name) {
  name = (name || '').trim();
  if (!name) return;
  closeSuggest(); // close the popup immediately on pick, before the round-trip
  fetch(`/api/comics/${id}/tags`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name }),
  })
    .then((r) => (r.ok ? r.json() : Promise.reject()))
    .then((j) => {
      const t = j.data;
      if (t && !tagsBox.querySelector(`.tag[data-id="${t.id}"]`)) {
        tagsBox.appendChild(makeChip(t));
        if (!allTags.some((x) => x.id === t.id)) allTags = [...allTags, t]; // immutable append
      }
      newtag.value = ''; // clear only on success, so a failed add keeps the text to retry
    })
    .catch(() => alert('添加标签失败'));
}
newtag.addEventListener('input', renderSuggest);
newtag.addEventListener('keydown', (e) => {
  if (suggest.hidden) { if (e.key === 'Enter') { e.preventDefault(); addTag(newtag.value); } return; }
  if (e.key === 'ArrowDown') { e.preventDefault(); setActive((sugActive + 1) % sugItems.length); }
  else if (e.key === 'ArrowUp') { e.preventDefault(); setActive((sugActive - 1 + sugItems.length) % sugItems.length); }
  else if (e.key === 'Enter') { e.preventDefault(); addTag(sugActive >= 0 ? sugItems[sugActive].name : newtag.value); }
  else if (e.key === 'Escape') { closeSuggest(); }
});
document.addEventListener('click', (e) => { if (!e.target.closest('.combobox')) closeSuggest(); });
document.getElementById('d-addtag').addEventListener('submit', (e) => { e.preventDefault(); addTag(newtag.value); });

// ---- page thumbnail grid (lazy) → click opens the reader at that page ----
const pageCount = parseInt(el.dataset.pages, 10) || 0;
const grid = document.getElementById('thumb-grid');
for (let i = 0; i < pageCount; i++) {
  const a = document.createElement('a');
  a.className = 'thumb-cell';
  a.href = `/read/${id}#p=${i}`;
  const im = document.createElement('img');
  im.loading = 'lazy';
  im.src = `/api/comics/${id}/pages/${i}/thumb`;
  im.alt = `第 ${i + 1} 页`;
  a.appendChild(im);
  grid.appendChild(a);
}
