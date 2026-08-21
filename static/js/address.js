// Address autocomplete (Mappify, proxied via /admin/address-search).
// Attaches to every input.addr-input inside an .addr-wrap. Picking a
// suggestion fills the input with the full address, fills the form's hidden
// addr_* fields (when present), fills the input named by data-suburb-target
// (when present) and shows the "verified" tick. Typing by hand clears the
// structured fields again, so the server can tell a picked address from a
// hand-typed one.
(function () {
  "use strict";

  function debounce(fn, ms) {
    var t;
    return function () {
      var self = this, args = arguments;
      clearTimeout(t);
      t = setTimeout(function () { fn.apply(self, args); }, ms);
    };
  }

  document.querySelectorAll(".addr-wrap .addr-input").forEach(function (input) {
    var wrap = input.closest(".addr-wrap");
    var list = wrap.querySelector(".addr-suggestions");
    var badge = wrap.querySelector(".addr-verified");
    var form = input.form;
    var seq = 0;

    function setHidden(name, val) {
      var el = form && form.querySelector('input[name="' + name + '"]');
      if (el) el.value = val;
    }

    function close() {
      list.hidden = true;
      list.innerHTML = "";
    }

    function pick(s) {
      input.value = s.label;
      setHidden("addr_street", s.street);
      setHidden("addr_suburb", s.suburb);
      setHidden("addr_state", s.state);
      setHidden("addr_postcode", s.postcode);
      var target = input.dataset.suburbTarget && document.querySelector(input.dataset.suburbTarget);
      if (target) target.value = s.suburb;
      if (badge) badge.classList.add("visible");
      close();
    }

    var search = debounce(function () {
      var q = input.value.trim();
      if (q.length < 3) { close(); return; }
      var mySeq = ++seq;
      fetch((input.dataset.searchUrl || "/admin/address-search") + "?q=" + encodeURIComponent(q))
        .then(function (r) { return r.ok ? r.json() : []; })
        .then(function (results) {
          if (mySeq !== seq) return; // a newer search is in flight
          list.innerHTML = "";
          if (!results.length) { list.hidden = true; return; }
          results.forEach(function (s) {
            var li = document.createElement("li");
            li.className = "addr-suggestion";
            li.textContent = s.label;
            // mousedown (not click) so it beats the input's blur.
            li.addEventListener("mousedown", function (e) {
              e.preventDefault();
              pick(s);
            });
            list.appendChild(li);
          });
          list.hidden = false;
        })
        .catch(close);
    }, 300);

    input.addEventListener("input", function () {
      setHidden("addr_street", "");
      setHidden("addr_suburb", "");
      setHidden("addr_state", "");
      setHidden("addr_postcode", "");
      if (badge) badge.classList.remove("visible");
      search();
    });

    input.addEventListener("keydown", function (e) {
      if (list.hidden) return;
      var items = list.querySelectorAll(".addr-suggestion");
      if (!items.length) return;
      var idx = -1;
      items.forEach(function (el, i) { if (el.classList.contains("addr-active")) idx = i; });
      if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        e.preventDefault();
        if (idx >= 0) items[idx].classList.remove("addr-active");
        idx = e.key === "ArrowDown" ? (idx + 1) % items.length : (idx <= 0 ? items.length - 1 : idx - 1);
        items[idx].classList.add("addr-active");
      } else if (e.key === "Enter" && idx >= 0) {
        e.preventDefault();
        items[idx].dispatchEvent(new MouseEvent("mousedown"));
      } else if (e.key === "Escape") {
        close();
      }
    });

    input.addEventListener("blur", function () { setTimeout(close, 150); });
  });
})();
