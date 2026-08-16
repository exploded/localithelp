(function() {
    'use strict';

    const BASE_COST = window.__BASE_COST__ || 2000;
    const form = document.getElementById('quote-form');
    const featuresCostEl = document.getElementById('features-cost');
    const totalRangeEl = document.getElementById('total-range');
    const btnSubmit = document.getElementById('btn-submit');

    function formatCurrency(n) {
        return '$' + n.toLocaleString('en-AU');
    }

    function roundTo500(n) {
        return Math.ceil(n / 500) * 500;
    }

    function calculateCost() {
        var features = 0;
        form.querySelectorAll('input[type="radio"]:checked').forEach(function(r) {
            features += parseInt(r.dataset.cost || '0', 10);
        });
        var total = BASE_COST + features;
        featuresCostEl.textContent = formatCurrency(features);
        // Show an approximate range to encourage getting the full quote
        var lower = roundTo500(total * 0.8);
        var upper = roundTo500(total * 1.4);
        totalRangeEl.textContent = formatCurrency(lower) + ' – ' + formatCurrency(upper);
        return total;
    }

    // Format the base cost display with proper currency formatting
    var baseCostDisplay = document.getElementById('base-cost-display');
    if (baseCostDisplay) baseCostDisplay.textContent = formatCurrency(BASE_COST);

    // Recalculate on any radio change
    form.addEventListener('change', calculateCost);
    calculateCost();

    function getFormData() {
        var data = {};
        // Text fields
        ['name', 'email', 'mobile', 'address', 'description'].forEach(function(f) {
            var el = document.getElementById(f);
            if (el) data[f] = el.value;
        });
        // Radio selections
        form.querySelectorAll('input[type="radio"]:checked').forEach(function(r) {
            data[r.name] = r.value;
            data[r.name + '_cost'] = parseInt(r.dataset.cost || '0', 10);
        });
        data.base_cost = BASE_COST;
        data.total_cost = calculateCost();
        return data;
    }

    // Submit: server checks Turnstile, stores the request and emails a confirmation link.
    var errorEl = document.getElementById('quote-error');
    var IDLE_LABEL = 'Email me my quote';

    function showError(msg) {
        errorEl.className = 'quote-submit-error';
        errorEl.textContent = msg;
        errorEl.hidden = false;
        btnSubmit.disabled = false;
        btnSubmit.textContent = IDLE_LABEL;
        // Turnstile tokens are single-use — get a fresh one for the retry.
        if (window.turnstile) { try { window.turnstile.reset(); } catch (e) {} }
    }

    // 409: this email already has a quote. Not an error to retry — hide the
    // submit button so it's clear nothing more can be requested here. If they
    // change the email (typo, different address) the form comes back.
    var emailEl = document.getElementById('email');
    function showAlreadyQuoted(msg) {
        errorEl.className = 'quote-submit-notice';
        errorEl.textContent = 'Nothing new was sent. ' + msg;
        errorEl.hidden = false;
        btnSubmit.hidden = true;
        btnSubmit.disabled = false;
        btnSubmit.textContent = IDLE_LABEL;
        if (window.turnstile) { try { window.turnstile.reset(); } catch (e) {} }
        emailEl.addEventListener('input', function restore() {
            emailEl.removeEventListener('input', restore);
            errorEl.hidden = true;
            btnSubmit.hidden = false;
        });
    }

    btnSubmit.addEventListener('click', function() {
        var name = document.getElementById('name');
        var email = document.getElementById('email');
        var desc = document.getElementById('description');
        if (!name.value.trim()) { name.focus(); name.reportValidity(); return; }
        if (!email.value.trim() || !email.checkValidity()) { email.focus(); email.reportValidity(); return; }
        if (!desc.value.trim()) { desc.focus(); desc.reportValidity(); return; }

        var data = getFormData();
        var tsInput = form.querySelector('input[name="cf-turnstile-response"]');
        if (tsInput) {
            if (!tsInput.value) { showError('Please complete the verification check above.'); return; }
            data['cf-turnstile-response'] = tsInput.value;
        }

        errorEl.hidden = true;
        btnSubmit.disabled = true;
        btnSubmit.textContent = 'Sending...';

        fetch('/api/quote', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(data)
        })
        .then(function(r) { return r.json().then(function(res) { res._status = r.status; return res; }); })
        .then(function(res) {
            if (res._status === 409) { showAlreadyQuoted(res.error); return; }
            if (res.error) { showError(res.error); return; }
            window.location.href = '/quote/sent?e=' + encodeURIComponent(res.email || data.email);
        })
        .catch(function() {
            showError('Could not send your request. Please check your connection and try again.');
        });
    });
})();
