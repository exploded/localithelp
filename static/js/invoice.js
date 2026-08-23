// Invoice editor: live line/total maths, add/remove rows, copy-to-clipboard.
(function () {
    'use strict';

    var DISCOUNT = 'Seniors Card discount';

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
    function desc(tr) { return tr.querySelector('input[name=desc]').value.trim(); }
    // A discount line is only auto-managed while its description still carries a
    // percentage; strip that and it becomes an ordinary line the user owns.
    function discountPct(tr) {
        var d = desc(tr);
        if (d.indexOf(DISCOUNT) === -1) return null;
        var m = /(\d+(?:\.\d+)?)\s*%/.exec(d);
        return m ? Number(m[1]) : null;
    }

    var body = document.getElementById('inv-lines-body');
    if (body) {
        var total = document.getElementById('inv-total');
        function discountRow() {
            var found = null;
            body.querySelectorAll('tr').forEach(function (tr) {
                if (!found && discountPct(tr) != null) found = tr;
            });
            return found;
        }
        function recalc() {
            var rows = body.querySelectorAll('tr');
            var disc = discountRow(), sub = 0;
            // Subtotal of everything the discount applies to.
            rows.forEach(function (tr) {
                if (tr === disc || !desc(tr)) return;
                sub += Math.round((Number(tr.querySelector('.inv-qty').value) || 0) * cents(tr.querySelector('.inv-unit').value));
            });
            // Re-derive the discount, unless the user is typing into its own
            // amount — editing that by hand has to win while the caret is there.
            if (disc) {
                var qty = disc.querySelector('.inv-qty'), unit = disc.querySelector('.inv-unit');
                unit.title = 'Recalculated from the lines above whenever they change';
                if (document.activeElement !== qty && document.activeElement !== unit) {
                    qty.value = '1';
                    unit.value = (-Math.round(Math.max(0, sub) * discountPct(disc) / 100) / 100).toFixed(2);
                }
            }
            var sum = 0;
            rows.forEach(function (tr) {
                var qty = Number(tr.querySelector('.inv-qty').value) || 0;
                var unit = cents(tr.querySelector('.inv-unit').value);
                var line = Math.round(qty * unit);
                tr.querySelector('.inv-line').textContent = (desc(tr) || unit) ? money(line) : '';
                if (desc(tr)) sum += line;
            });
            total.textContent = money(sum);
        }
        function addRow(description, qty, unit) {
            var tr = document.createElement('tr');
            tr.innerHTML = '<td><input class="admin-input" name="desc" maxlength="200"></td>' +
                '<td class="td-num"><input class="admin-input inv-qty" name="qty" inputmode="decimal"></td>' +
                '<td class="td-num"><input class="admin-input inv-unit" name="unit" inputmode="decimal"></td>' +
                '<td class="td-num inv-line"></td>' +
                '<td><button type="button" class="btn-icon inv-remove" title="Remove line">&times;</button></td>';
            tr.querySelector('input[name=desc]').value = description || '';
            tr.querySelector('.inv-qty').value = qty == null ? '1' : qty;
            tr.querySelector('.inv-unit').value = unit == null ? '' : unit;
            // Keep the discount last, so it always reads as applying to the
            // lines above it.
            var disc = discountRow();
            if (disc && disc !== tr && (description || '').indexOf(DISCOUNT) === -1) body.insertBefore(tr, disc);
            else body.appendChild(tr);
            tr.querySelector('input[name=desc]').focus();
            recalc();
        }
        body.addEventListener('input', recalc);
        body.addEventListener('focusout', recalc);
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
        var sen = document.getElementById('inv-add-seniors');
        if (sen) sen.addEventListener('click', function () {
            if (discountRow()) { alert('A seniors discount line is already on this invoice.'); return; }
            var sum = 0;
            body.querySelectorAll('tr').forEach(function (tr) {
                if (desc(tr)) sum += Math.round((Number(tr.querySelector('.inv-qty').value) || 0) * cents(tr.querySelector('.inv-unit').value));
            });
            if (sum <= 0) { alert('Add the fee and labour lines first, then apply the discount.'); return; }
            // Leave the amount to recalc — it owns the sum from here on.
            addRow(DISCOUNT + ' — ' + sen.getAttribute('data-pct') + '%', 1, '');
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
