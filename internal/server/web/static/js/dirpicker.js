// Server-side directory picker modal in the 格/PANEL style. The browser's
// native folder dialog can only pick client-side folders (and hides absolute
// paths), so library paths are chosen by browsing the server's allowedRoots
// via GET /api/fs/dirs. Exposes window.openDirPicker(onPick).
(function () {
  let overlay = null;
  let onPick = null;
  let current = null;   // absolute path being viewed; null = roots view
  let rootCount = 0;    // >1 roots → 上级 at a root returns to the roots view
  let els = null;

  function h(tag, cls, text) {
    const el = document.createElement(tag);
    if (cls) el.className = cls;
    if (text !== undefined) el.textContent = text;
    return el;
  }

  function build() {
    overlay = h('div', 'dp-overlay');
    overlay.hidden = true;
    const modal = h('div', 'dp-modal');

    const head = h('div', 'dp-head');
    const title = h('h3', '', '选择库目录');
    const close = h('button', 'dp-close', '×');
    close.type = 'button';
    close.setAttribute('aria-label', '关闭');
    head.append(title, close);

    const jumpRow = h('div', 'dp-jump-row');
    const jump = h('input');
    jump.placeholder = '输入绝对路径回车直达';
    jump.autocomplete = 'off';
    jump.spellcheck = false;
    const go = h('button', '', '前往');
    go.type = 'button';
    jumpRow.append(jump, go);

    const cur = h('div', 'dp-path-row');
    const up = h('button', 'dp-up', '上级');
    up.type = 'button';
    const curPath = h('span', 'dp-cur', '');
    cur.append(up, curPath);

    const list = h('ul', 'dp-list');
    const err = h('div', 'dp-err', '');

    const foot = h('div', 'dp-foot');
    const cancel = h('button', '', '取消');
    cancel.type = 'button';
    const confirm = h('button', 'dp-confirm', '选定当前目录');
    confirm.type = 'button';
    foot.append(cancel, confirm);

    modal.append(head, jumpRow, cur, list, err, foot);
    overlay.appendChild(modal);
    document.body.appendChild(overlay);
    els = { jump, go, up, curPath, list, err, confirm };

    close.addEventListener('click', hide);
    cancel.addEventListener('click', hide);
    overlay.addEventListener('click', (e) => { if (e.target === overlay) hide(); });
    document.addEventListener('keydown', (e) => {
      // an Escape (or Enter below) that ends an IME composition must not
      // count as a modal command — Chinese dir names are typed via IME here
      if (e.key === 'Escape' && !e.isComposing && !overlay.hidden) hide();
    });
    confirm.addEventListener('click', () => {
      if (!current) return;
      const cb = onPick;
      hide();
      if (cb) cb(current);
    });
    up.addEventListener('click', () => {
      if (up.dataset.to) loadDir(up.dataset.to);
      else loadRoots(false);
    });
    go.addEventListener('click', () => { if (jump.value.trim()) loadDir(jump.value.trim()); });
    jump.addEventListener('keydown', (e) => {
      if (e.key === 'Enter' && !e.isComposing) { e.preventDefault(); if (jump.value.trim()) loadDir(jump.value.trim()); }
    });
    list.addEventListener('click', (e) => {
      const li = e.target.closest('li[data-path]');
      if (li) loadDir(li.dataset.path);
    });
  }

  function hide() { overlay.hidden = true; onPick = null; }

  function setError(msg) { els.err.textContent = msg || ''; }

  function api(url) {
    return fetch(url).then((r) =>
      r.json().then((j) => {
        if (!r.ok || j.code !== 0) throw new Error(j.message || '加载失败');
        return j.data;
      })
    );
  }

  function renderList(items, enterable) {
    els.list.innerHTML = '';
    if (items.length === 0) {
      els.list.appendChild(h('li', 'dp-empty', enterable ? '无子目录' : '没有可用的库根，请检查 library.allowedRoots 配置'));
      return;
    }
    items.forEach((d) => {
      const li = h('li', '', d.name);
      li.dataset.path = d.path;
      els.list.appendChild(li);
    });
  }

  // Roots view: current = null, confirm disabled. With a single root we jump
  // straight into it (per spec) unless the user explicitly navigated up.
  function loadRoots(autoEnter) {
    setError('');
    api('/api/fs/dirs')
      .then((data) => {
        rootCount = data.roots.length;
        if (autoEnter && rootCount === 1) { loadDir(data.roots[0].path); return; }
        current = null;
        els.curPath.textContent = '库根列表';
        els.up.disabled = true;
        els.up.dataset.to = '';
        els.confirm.disabled = true;
        renderList(data.roots, false);
      })
      .catch((e) => setError(e.message));
  }

  function loadDir(path) {
    setError('');
    api('/api/fs/dirs?path=' + encodeURIComponent(path))
      .then((data) => {
        current = data.path;
        els.curPath.textContent = data.path;
        els.up.dataset.to = data.parent || '';
        // at a root: multi-root setups go back to the roots view; with a
        // single root there is nowhere up to go.
        els.up.disabled = !data.parent && rootCount <= 1;
        els.confirm.disabled = false;
        els.jump.value = '';
        renderList(data.dirs, true);
      })
      // keep confirm in sync with current: a failed jump from a valid dir
      // leaves the view (and confirm) intact, but a failed auto-enter on
      // open has no selectable dir yet
      .catch((e) => { setError(e.message); els.confirm.disabled = !current; });
  }

  window.openDirPicker = function (cb) {
    if (!overlay) build();
    onPick = cb;
    setError('');
    overlay.hidden = false;
    loadRoots(true);
    els.jump.focus();
  };
})();
