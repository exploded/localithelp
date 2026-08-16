(function() {
    'use strict';

    var groups = JSON.parse(JSON.stringify(window.__GROUPS__ || []));
    var baseCost = window.__BASE_COST__ || 2000;
    var app = document.getElementById('admin-app');
    var statusEl = document.getElementById('admin-status');

    function escapeHTML(s) {
        var div = document.createElement('div');
        div.textContent = s;
        return div.innerHTML;
    }

    function renderAll() {
        var html = '<div class="admin-group">';
        html += '<div class="admin-group-header"><div class="admin-group-fields">';
        html += '<div class="admin-field"><label>Base Platform Cost ($)</label>';
        html += '<input type="number" class="admin-input" id="admin-base-cost" value="' + baseCost + '">';
        html += '</div></div></div></div>';
        for (var i = 0; i < groups.length; i++) {
            html += renderGroup(groups[i], i);
        }
        app.innerHTML = html;
        document.getElementById('admin-base-cost').addEventListener('input', function() {
            baseCost = parseInt(this.value) || 0;
        });
        bindEvents();
    }

    function renderGroup(g, gi) {
        var h = '<div class="admin-group" data-gi="' + gi + '">';
        h += '<div class="admin-group-header">';
        h += '<div class="admin-group-fields">';
        h += '<div class="admin-field"><label>Label</label><input type="text" class="admin-input g-label" value="' + escapeHTML(g.label) + '"></div>';
        h += '<div class="admin-field"><label>Field Name</label><input type="text" class="admin-input g-name" value="' + escapeHTML(g.name) + '"></div>';
        h += '<div class="admin-field admin-field--wide"><label>Hint</label><input type="text" class="admin-input g-hint" value="' + escapeHTML(g.hint) + '"></div>';
        h += '<div class="admin-field admin-field--narrow"><label>Order</label><input type="number" class="admin-input g-sort" value="' + g.sort_order + '"></div>';
        h += '</div>';
        h += '<button type="button" class="btn-icon btn-delete-group" title="Delete group">&times;</button>';
        h += '</div>';

        h += '<table class="admin-table"><thead><tr>';
        h += '<th>Value</th><th>Display Name</th><th>Cost ($)</th><th>Cost Label</th><th>Default</th><th>Order</th><th></th>';
        h += '</tr></thead><tbody>';

        var opts = g.options || [];
        for (var j = 0; j < opts.length; j++) {
            var o = opts[j];
            h += '<tr data-oi="' + j + '">';
            h += '<td><input type="text" class="admin-input o-value" value="' + escapeHTML(o.value) + '"></td>';
            h += '<td><input type="text" class="admin-input o-name" value="' + escapeHTML(o.name) + '"></td>';
            h += '<td><input type="number" class="admin-input o-cost" value="' + o.cost + '"></td>';
            h += '<td><input type="text" class="admin-input o-costlabel" value="' + escapeHTML(o.cost_label) + '"></td>';
            h += '<td class="td-center"><input type="radio" name="default_' + gi + '" class="o-default"' + (o.is_default ? ' checked' : '') + '></td>';
            h += '<td><input type="number" class="admin-input o-sort" value="' + o.sort_order + '"></td>';
            h += '<td><button type="button" class="btn-icon btn-delete-option" title="Delete">&times;</button></td>';
            h += '</tr>';
        }

        h += '</tbody></table>';
        h += '<button type="button" class="btn btn--ghost btn-add-option" style="margin-top:0.5rem;padding:0.5rem 1rem;font-size:0.65rem;">+ Add Option</button>';
        h += '</div>';
        return h;
    }

    function bindEvents() {
        // Group field changes
        app.querySelectorAll('.admin-group').forEach(function(el) {
            var gi = parseInt(el.dataset.gi);

            el.querySelector('.g-label').addEventListener('input', function() { groups[gi].label = this.value; });
            el.querySelector('.g-name').addEventListener('input', function() { groups[gi].name = this.value; });
            el.querySelector('.g-hint').addEventListener('input', function() { groups[gi].hint = this.value; });
            el.querySelector('.g-sort').addEventListener('input', function() { groups[gi].sort_order = parseInt(this.value) || 0; });

            // Delete group
            el.querySelector('.btn-delete-group').addEventListener('click', function() {
                if (confirm('Delete group "' + groups[gi].label + '" and all its options?')) {
                    groups.splice(gi, 1);
                    renderAll();
                }
            });

            // Add option
            el.querySelector('.btn-add-option').addEventListener('click', function() {
                if (!groups[gi].options) groups[gi].options = [];
                groups[gi].options.push({
                    id: 0,
                    group_id: groups[gi].id || 0,
                    value: '',
                    name: '',
                    cost: 0,
                    cost_label: '$0',
                    is_default: false,
                    sort_order: groups[gi].options.length
                });
                renderAll();
                // Focus the new value input
                var lastGroup = app.querySelectorAll('.admin-group')[gi];
                var rows = lastGroup.querySelectorAll('tbody tr');
                if (rows.length) rows[rows.length - 1].querySelector('.o-value').focus();
            });

            // Option field changes
            el.querySelectorAll('tbody tr').forEach(function(tr) {
                var oi = parseInt(tr.dataset.oi);

                tr.querySelector('.o-value').addEventListener('input', function() { groups[gi].options[oi].value = this.value; });
                tr.querySelector('.o-name').addEventListener('input', function() { groups[gi].options[oi].name = this.value; });
                tr.querySelector('.o-cost').addEventListener('input', function() { groups[gi].options[oi].cost = parseInt(this.value) || 0; });
                tr.querySelector('.o-costlabel').addEventListener('input', function() { groups[gi].options[oi].cost_label = this.value; });
                tr.querySelector('.o-sort').addEventListener('input', function() { groups[gi].options[oi].sort_order = parseInt(this.value) || 0; });

                tr.querySelector('.o-default').addEventListener('change', function() {
                    // Uncheck all others in this group
                    for (var k = 0; k < groups[gi].options.length; k++) {
                        groups[gi].options[k].is_default = (k === oi);
                    }
                });

                tr.querySelector('.btn-delete-option').addEventListener('click', function() {
                    groups[gi].options.splice(oi, 1);
                    renderAll();
                });
            });
        });
    }

    // Add group button
    document.getElementById('btn-add-group').addEventListener('click', function() {
        groups.push({
            id: 0,
            name: 'feature_new',
            label: 'New Group',
            hint: '',
            sort_order: groups.length,
            options: []
        });
        renderAll();
        window.scrollTo(0, document.body.scrollHeight);
    });

    // Save button
    document.getElementById('btn-save').addEventListener('click', function() {
        var btn = this;
        btn.disabled = true;
        btn.textContent = 'Saving...';
        statusEl.style.display = 'none';

        fetch('/api/admin/save', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ groups: groups, base_cost: baseCost })
        })
        .then(function(r) { return r.json(); })
        .then(function(data) {
            if (data.error) {
                showStatus('Error: ' + data.error, true);
            } else {
                showStatus('Changes saved successfully.', false);
            }
        })
        .catch(function() {
            showStatus('Failed to save. Please try again.', true);
        })
        .finally(function() {
            btn.disabled = false;
            btn.textContent = 'Save All Changes';
        });
    });

    function showStatus(msg, isError) {
        statusEl.textContent = msg;
        statusEl.className = 'admin-status' + (isError ? ' admin-status--error' : ' admin-status--success');
        statusEl.style.display = 'block';
        setTimeout(function() { statusEl.style.display = 'none'; }, 4000);
    }

    renderAll();
})();
