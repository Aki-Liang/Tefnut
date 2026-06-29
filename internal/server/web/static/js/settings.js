const initial = JSON.parse(document.getElementById('scan-data').textContent);

// reflect current scan settings into the form
document.querySelector(`input[name="mode"][value="${initial.mode}"]`).checked = true;
document.getElementById('iv').value = initial.interval;
document.getElementById('daily').value = initial.daily;

function syncModeArgs() {
  const mode = document.querySelector('input[name="mode"]:checked').value;
  document.querySelectorAll('.mode-args').forEach(el => {
    el.style.display = el.dataset.mode === mode ? '' : 'none';
  });
}
document.querySelectorAll('input[name="mode"]').forEach(r => r.addEventListener('change', syncModeArgs));
syncModeArgs();

// auto-fill name from path basename
document.getElementById('np-path').addEventListener('input', (e) => {
  const nameEl = document.getElementById('np-name');
  if (!nameEl.dataset.touched) {
    const parts = e.target.value.replace(/\/+$/, '').split('/');
    nameEl.value = parts[parts.length - 1] || '';
  }
});
document.getElementById('np-name').addEventListener('input', (e) => { e.target.dataset.touched = '1'; });

document.getElementById('addpath').addEventListener('submit', (e) => {
  e.preventDefault();
  const path = document.getElementById('np-path').value.trim();
  const name = document.getElementById('np-name').value.trim();
  if (!path) return;
  fetch('/api/settings/paths', {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name, path })
  }).then(r => {
    if (!r.ok) { r.json().then(j => alert(j.message || '添加失败')).catch(() => alert('添加失败')); return; }
    location.reload();
  }).catch(() => alert('添加失败'));
});

document.getElementById('paths').addEventListener('click', (e) => {
  if (!e.target.classList.contains('del')) return;
  const li = e.target.closest('li');
  if (!confirm('删除该库路径？其下漫画记录会在重扫后移除。')) return;
  fetch(`/api/settings/paths/${li.dataset.id}`, { method: 'DELETE' })
    .then(r => { if (!r.ok) { alert('删除失败'); return; } location.reload(); })
    .catch(() => alert('删除失败'));
});

document.getElementById('scanform').addEventListener('submit', (e) => {
  e.preventDefault();
  const mode = document.querySelector('input[name="mode"]:checked').value;
  const payload = {
    scanMode: mode,
    scanInterval: document.getElementById('iv').value.trim(),
    scanDailyTime: document.getElementById('daily').value.trim()
  };
  fetch('/api/settings', {
    method: 'PUT', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload)
  }).then(r => {
    if (!r.ok) { r.json().then(j => alert(j.message || '保存失败')).catch(() => alert('保存失败')); return; }
    alert('已保存，扫描设置已生效');
  }).catch(() => alert('保存失败'));
});
