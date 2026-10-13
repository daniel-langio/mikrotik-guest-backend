// Server renders timestamps as UTC ISO strings (it doesn't know the
// viewer's timezone); this converts them to the browser's local time.
function localizeTimes(root) {
  root.querySelectorAll('.localtime[data-iso]').forEach(function (el) {
    el.textContent = new Date(el.dataset.iso).toLocaleTimeString();
  });
}
document.addEventListener('DOMContentLoaded', function () { localizeTimes(document); });
document.addEventListener('htmx:afterSwap', function (e) { localizeTimes(e.target); });

// Countdown is evaluated client-side from each session's absolute expiry
// instant, so it ticks every second instead of only updating on the
// dashboard's 5s poll (and stays correct even if a poll is briefly delayed).
function tickCountdowns() {
  document.querySelectorAll('.countdown[data-expires]').forEach(function (el) {
    var remainingMs = new Date(el.dataset.expires) - Date.now();
    if (remainingMs <= 0) {
      el.textContent = 'expired';
      return;
    }
    var totalSec = Math.floor(remainingMs / 1000);
    var h = Math.floor(totalSec / 3600);
    var m = Math.floor((totalSec % 3600) / 60);
    var s = totalSec % 60;
    el.textContent = h + ':' + String(m).padStart(2, '0') + ':' + String(s).padStart(2, '0') + ' left';
  });
}
setInterval(tickCountdowns, 1000);
document.addEventListener('DOMContentLoaded', tickCountdowns);
document.addEventListener('htmx:afterSwap', tickCountdowns);
