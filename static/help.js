// Auto-advancing demo: shows one step at a time, loops, tap a dot to jump.
// Without JS every step simply stays stacked on the page.
(function () {
  var box = document.getElementById('slides'), dotsBox = document.getElementById('dots');
  if (!box || !dotsBox) return;
  var slides = box.querySelectorAll('.slide'), i = 0, timer;
  box.classList.add('js');
  slides.forEach(function (_, n) {
    var d = document.createElement('button');
    d.type = 'button';
    d.className = 'dot-btn';
    d.addEventListener('click', function () { show(n); restart(); });
    dotsBox.appendChild(d);
  });
  function show(n) {
    i = n;
    slides.forEach(function (s, k) { s.classList.toggle('on', k === n); });
    dotsBox.childNodes.forEach(function (d, k) { d.classList.toggle('on', k === n); });
  }
  function restart() {
    clearInterval(timer);
    timer = setInterval(function () { show((i + 1) % slides.length); }, 3500);
  }
  show(0);
  restart();
})();
