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
document.getElementById('d-addtag').addEventListener('submit', (e) => {
  e.preventDefault();
  const input = document.getElementById('d-newtag');
  const name = input.value.trim();
  if (!name) return;
  fetch(`/api/comics/${id}/tags`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name }),
  })
    .then((r) => (r.ok ? r.json() : Promise.reject()))
    .then((j) => {
      const t = j.data;
      if (t && !tagsBox.querySelector(`.tag[data-id="${t.id}"]`)) tagsBox.appendChild(makeChip(t));
      input.value = '';
    })
    .catch(() => alert('添加标签失败'));
});

// autocomplete existing tags
fetch('/api/tags')
  .then((r) => r.json())
  .then((j) => {
    const dl = document.getElementById('d-taglist');
    (j.data || []).forEach((t) => { const o = document.createElement('option'); o.value = t.name; dl.appendChild(o); });
  })
  .catch(() => {});
