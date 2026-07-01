// Progressive-enhancement custom dropdown. Replaces the native <select> popup
// (which can't be themed) with a panel-surface menu matching the 格/PANEL system,
// while keeping the native <select> (hidden) as the source of truth for value,
// form submission, and the no-JS fallback.
(function () {
  let openWrap = null;

  function closeOpen() { if (openWrap) openWrap._close(); }

  function enhance(select) {
    if (select.dataset.fancy === '1' || select.options.length === 0) return;
    select.dataset.fancy = '1';
    select.classList.add('dd-native');

    const wrap = document.createElement('div');
    wrap.className = 'dd';
    select.parentNode.insertBefore(wrap, select);
    wrap.appendChild(select);

    const trigger = document.createElement('button');
    trigger.type = 'button';
    trigger.className = 'dd-trigger';
    trigger.setAttribute('aria-haspopup', 'listbox');
    trigger.setAttribute('aria-expanded', 'false');
    if (select.title) trigger.title = select.title;
    const label = document.createElement('span');
    label.className = 'dd-label';
    const chev = document.createElement('span');
    chev.className = 'dd-chev';
    chev.setAttribute('aria-hidden', 'true');
    trigger.append(label, chev);

    const menu = document.createElement('div');
    menu.className = 'dd-menu';
    menu.setAttribute('role', 'listbox');
    menu.tabIndex = -1;
    menu.hidden = true;

    wrap.append(trigger, menu);

    let active = -1;

    function buildOptions() {
      menu.innerHTML = '';
      Array.from(select.options).forEach((opt, i) => {
        const o = document.createElement('div');
        o.className = 'dd-option';
        o.setAttribute('role', 'option');
        o.dataset.i = String(i);
        o.textContent = opt.textContent;
        if (opt.disabled) o.setAttribute('aria-disabled', 'true');
        o.addEventListener('mousemove', () => setActive(i));
        o.addEventListener('click', () => choose(i));
        menu.appendChild(o);
      });
    }

    function syncLabel() {
      const opt = select.options[select.selectedIndex];
      label.textContent = opt ? opt.textContent : '';
      Array.from(menu.children).forEach((c) => {
        c.setAttribute('aria-selected', Number(c.dataset.i) === select.selectedIndex ? 'true' : 'false');
      });
    }

    function setActive(i) {
      active = i;
      Array.from(menu.children).forEach((c, j) => c.classList.toggle('active', j === i));
      const el = menu.children[i];
      if (el) el.scrollIntoView({ block: 'nearest' });
    }

    function place() {
      // open upward when there isn't room below (e.g. the reader's bottom bar)
      const r = trigger.getBoundingClientRect();
      const below = window.innerHeight - r.bottom;
      wrap.classList.toggle('up', below < Math.min(260, r.top));
    }

    function open() {
      if (!menu.hidden) return;
      closeOpen();
      buildOptions();
      syncLabel();
      menu.hidden = false;
      place();
      wrap.classList.add('open');
      trigger.setAttribute('aria-expanded', 'true');
      openWrap = wrap;
      setActive(select.selectedIndex >= 0 ? select.selectedIndex : 0);
      menu.focus();
    }

    function close() {
      if (menu.hidden) return;
      menu.hidden = true;
      wrap.classList.remove('open', 'up');
      trigger.setAttribute('aria-expanded', 'false');
      if (openWrap === wrap) openWrap = null;
    }
    wrap._close = close;

    function choose(i) {
      const opt = select.options[i];
      if (opt && opt.disabled) return;
      if (select.selectedIndex !== i) {
        select.selectedIndex = i;
        select.dispatchEvent(new Event('change', { bubbles: true }));
      }
      syncLabel();
      close();
      trigger.focus();
    }

    trigger.addEventListener('click', () => (menu.hidden ? open() : close()));
    trigger.addEventListener('keydown', (e) => {
      if (e.key === 'ArrowDown' || e.key === 'Enter' || e.key === ' ') { e.preventDefault(); open(); }
    });
    menu.addEventListener('keydown', (e) => {
      const n = menu.children.length;
      if (e.key === 'ArrowDown') { e.preventDefault(); setActive((active + 1) % n); }
      else if (e.key === 'ArrowUp') { e.preventDefault(); setActive((active - 1 + n) % n); }
      else if (e.key === 'Home') { e.preventDefault(); setActive(0); }
      else if (e.key === 'End') { e.preventDefault(); setActive(n - 1); }
      else if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); choose(active); }
      else if (e.key === 'Escape') { e.preventDefault(); close(); trigger.focus(); }
      else if (e.key === 'Tab') { close(); }
    });
    // keep the trigger label correct if other code changes the select value
    select.addEventListener('change', syncLabel);

    buildOptions();
    syncLabel();
    // a value set programmatically just after load (e.g. the reader's display mode) lands next frame
    requestAnimationFrame(syncLabel);
  }

  function enhanceAll(root) {
    (root || document).querySelectorAll('select').forEach(enhance);
  }

  document.addEventListener('click', (e) => {
    if (openWrap && !openWrap.contains(e.target)) closeOpen();
  });
  window.addEventListener('resize', closeOpen);

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', () => enhanceAll());
  } else {
    enhanceAll();
  }
})();
