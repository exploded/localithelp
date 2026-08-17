// Invoice editor: live line/total maths, add/remove rows, copy-to-clipboard.
(function () {
    'use strict';

    function money(cents) {
        var neg = cents < 0; if (neg) cents = -cents;
        var d = Math.floor(cents / 100), f = cents % 100;
        return (neg ? '-' : '') + '$' + d.toString().replace(/\B(?=(\d{3})+(?!\d))/g, ',') + '.' + (f < 10 ? '0' : '') + f;
    }
    function cents(s) {
        s = String(s || '').replace(/[$,\s]/g, '');
        if (!s) return 0;
        var n = Number(s);
        return isNaN(n) ? 0 : Math.round(n * 100);
    }

    var body = document.getElementById('inv-lines-body');
    if (body) {
        var total = document.getElementById('inv-total');
        function recalc() {
            var sum = 0;
            body.querySelectorAll('tr').forEach(function (tr) {
                var qty = Number(tr.querySelector('.inv-qty').value) || 0;
                var unit = cents(tr.querySelector('.inv-unit').value);
                var line = Math.round(qty * unit);
                var desc = tr.querySelector('input[name=desc]').value.trim();
                tr.querySelector('.inv-line').textContent = (desc || unit) ? money(line) : '';
                if (desc) sum += line;
            });
            total.textContent = money(sum);
        }
        function addRow(desc, qty, unit) {
            var tr = document.createElement('tr');
            tr.innerHTML = '<td><input class="admin-input" name="desc" maxlength="200"></td>' +
                '<td class="td-num"><input class="admin-input inv-qty" name="qty" inputmode="decimal"></td>' +
                '<td class="td-num"><input class="admin-input inv-unit" name="unit" inputmode="decimal"></td>' +
                '<td class="td-num inv-line"></td>' +
                '<td><button type="button" class="btn-icon inv-remove" title="Remove line">&times;</button></td>';
            tr.querySelector('input[name=desc]').value = desc || '';
            tr.querySelector('.inv-qty').value = qty == null ? '1' : qty;
            tr.querySelector('.inv-unit').value = unit == null ? '' : unit;
            body.appendChild(tr);
            tr.querySelector('input[name=desc]').focus();
            recalc();
        }
        body.addEventListener('input', recalc);
        body.addEventListener('click', function (e) {
            var btn = e.target.closest('.inv-remove');
            if (!btn) return;
            var rows = body.querySelectorAll('tr');
            if (rows.length > 1) btn.closest('tr').remove(); else { btn.closest('tr').querySelectorAll('input').forEach(function (i) { i.value = ''; }); }
            recalc();
        });
        document.getElementById('inv-add').addEventListener('click', function () { addRow('', 1, ''); });
        var lab = document.getElementById('inv-add-labour');
        lab.addEventListener('click', function () {
            var units = prompt('How many 15-minute blocks?', '2');
            if (!units) return;
            addRow('Labour — ' + units + ' × 15 min', units, lab.getAttribute('data-rate'));
        });
        recalc();
    }

    document.querySelectorAll('.copy-btn').forEach(function (b) {
        b.addEventListener('click', function () {
            var v = b.getAttribute('data-copy');
            var done = function () { var t = b.textContent; b.textContent = '✓'; setTimeout(function () { b.textContent = t; }, 1200); };
            if (navigator.clipboard && navigator.clipboard.writeText) navigator.clipboard.writeText(v).then(done, function () { prompt('Copy:', v); });
            else prompt('Copy:', v);
        });
    });
})();
