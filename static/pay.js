// Price <-> duration calculator. The button flips which side the guest types;
// the server recomputes the duration from the amount, so this is display only.
document.addEventListener('htmx:load', function (e) {
  var form = e.target.querySelector ? e.target.querySelector('.pay-form') || (e.target.matches('.pay-form') ? e.target : null) : null;
  if (!form || form.dataset.ready) return;
  form.dataset.ready = '1';
  var price = +form.dataset.price, mins = +form.dataset.mins;
  var amount = form.querySelector('#amount'), minutes = form.querySelector('#minutes');
  var sum = form.querySelector('#summary'), btn = form.querySelector('#mode');
  var priceField = form.querySelector('#price-field'), timeField = form.querySelector('#time-field');
  var byTime = false;

  function fmt(m) {
    var h = Math.floor(m / 60), r = m % 60;
    return (h ? h + ' h ' : '') + (r || !h ? r + ' min' : '');
  }
  function update() {
    if (byTime) {
      var units = Math.max(1, Math.ceil((+minutes.value || 0) / mins));
      amount.value = units * price;
    }
    var u = Math.floor((+amount.value || 0) / price);
    sum.textContent = byTime
      ? fmt(u * mins) + ' costs ' + amount.value + ' Ar'
      : amount.value + ' Ar gives ' + fmt(u * mins);
  }
  amount.addEventListener('input', update);
  minutes.addEventListener('input', update);
  btn.addEventListener('click', function () {
    byTime = !byTime;
    priceField.hidden = byTime;
    timeField.hidden = !byTime;
    btn.textContent = byTime ? 'Choose by price' : 'Choose by time';
    if (byTime) minutes.value = Math.floor((+amount.value || price) / price) * mins;
    update();
  });
  update();
});
