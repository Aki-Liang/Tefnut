(function () {
  var KEY = 'sidebarCollapsed';
  if (localStorage.getItem(KEY) === '1') {
    document.body.classList.add('sidebar-collapsed');
  }
  var btn = document.getElementById('sidebar-toggle');
  if (btn) {
    btn.addEventListener('click', function () {
      var collapsed = document.body.classList.toggle('sidebar-collapsed');
      localStorage.setItem(KEY, collapsed ? '1' : '0');
    });
  }
})();
