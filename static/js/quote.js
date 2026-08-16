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

    // Load draft data if present
    if (window.__DRAFT__) {
        var d = window.__DRAFT__;
        ['name', 'mobile', 'address', 'description'].forEach(function(f) {
            var el = document.getElementById(f);
            if (el && d[f]) el.value = d[f];
        });
        // Restore feature radio selections
        if (d.features) {
            Object.keys(d.features).forEach(function(key) {
                var radio = form.querySelector('input[name="' + key + '"][value="' + d.features[key] + '"]');
                if (radio) radio.checked = true;
            });
        }
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

    // Submit Quote via Stripe
    btnSubmit.addEventListener('click', function() {
        // Validate required fields
        var name = document.getElementById('name');
        var email = document.getElementById('email');
        var desc = document.getElementById('description');
        if (!name.value.trim()) { name.focus(); name.reportValidity(); return; }
        if (!email.value.trim()) { email.focus(); email.reportValidity(); return; }
        if (!desc.value.trim()) { desc.focus(); desc.reportValidity(); return; }

        btnSubmit.disabled = true;
        btnSubmit.textContent = 'Redirecting to payment...';

        fetch('/api/create-checkout', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(getFormData())
        })
        .then(function(r) { return r.json(); })
        .then(function(data) {
            if (data.error) {
                alert('Error: ' + data.error);
                btnSubmit.disabled = false;
                btnSubmit.innerHTML = 'Get Your Quote &mdash; $5';
                return;
            }
            // Redirect to Stripe Checkout
            var stripeKey = document.getElementById('stripe-key').dataset.key;
            var stripe = Stripe(stripeKey);
            stripe.redirectToCheckout({ sessionId: data.session_id });
        })
        .catch(function(err) {
            alert('Failed to create checkout session. Please try again.');
            btnSubmit.disabled = false;
            btnSubmit.innerHTML = 'Get Your Quote &mdash; $5';
        });
    });
})();
