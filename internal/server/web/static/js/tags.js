document.getElementById('newtagform').addEventListener('submit', (e) => {
  e.preventDefault();
  const input = document.getElementById('newtagname');
  const name = input.value.trim();
  if (!name) return;
  fetch('/api/tags', {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name })
  }).then(r => {
    if (!r.ok) { alert('新建标签失败'); return; }
    location.reload();
  }).catch(() => alert('新建标签失败'));
});

document.getElementById('taglist').addEventListener('click', (e) => {
  const li = e.target.closest('li');
  if (!li) return;
  const id = li.dataset.id;
  if (e.target.classList.contains('rename')) {
    const name = li.querySelector('.tname').value.trim();
    fetch(`/api/tags/${id}`, {
      method: 'PATCH', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name })
    }).then(r => { if (!r.ok) alert('重命名失败（可能重名）'); else location.reload(); })
      .catch(() => alert('重命名失败'));
  }
  if (e.target.classList.contains('del')) {
    if (!confirm('确认删除该标签？')) return;
    fetch(`/api/tags/${id}`, { method: 'DELETE' }).then(r => {
      if (!r.ok) { alert('删除标签失败'); return; }
      location.reload();
    }).catch(() => alert('删除标签失败'));
  }
});
