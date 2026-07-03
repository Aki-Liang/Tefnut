// Library scan button: trigger a scan and mirror the real in-flight state.
// The button spins while ANY scan runs (manual, scheduled, or watch-triggered),
// so a click during a background scan never looks like a dead button.
(function () {
  const btn = document.getElementById('scan-btn');
  if (!btn) return;
  let active = false; // we observed a scan running → reload when it finishes

  function setSpinning(on) {
    btn.classList.toggle('spinning', on);
    btn.disabled = on;
  }

  function poll() {
    fetch('/api/scan/status')
      .then((r) => { if (!r.ok) throw new Error(String(r.status)); return r.json(); })
      .then((j) => {
        const scanning = !!(j.data && j.data.scanning);
        if (scanning) {
          active = true;
          setSpinning(true);
          setTimeout(poll, 2000);
        } else if (active) {
          location.reload(); // scan finished — show the new comics/covers
        } else {
          setSpinning(false);
        }
      })
      .catch(() => { active = false; setSpinning(false); });
  }

  btn.addEventListener('click', () => {
    active = true;
    setSpinning(true);
    fetch('/api/scan', { method: 'POST' })
      .then(() => setTimeout(poll, 1500))
      .catch(() => { active = false; setSpinning(false); });
  });

  // On load: reflect a scan already running in the background.
  poll();
})();
