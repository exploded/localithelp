// Service-area map: a soft radius around Donvale, drawn with Leaflet + OSM tiles.
// Deliberately vague — the circle is centred on the suburb (rounded to ~1 km),
// there is no pin, and nothing here identifies a street address.
(function () {
    const el = document.getElementById('area-map');
    if (!el) return;

    const CENTRE = [-37.79, 145.18];   // Donvale, rounded
    const RADIUS_M = 15000;            // ~30 min drive

    function loadAsset(tag, attrs) {
        return new Promise((resolve, reject) => {
            const node = document.createElement(tag);
            Object.assign(node, attrs);
            node.onload = resolve;
            node.onerror = reject;
            document.head.appendChild(node);
        });
    }

    function init() {
        const map = L.map(el, {
            center: CENTRE,
            zoom: 10,
            zoomControl: true,
            scrollWheelZoom: false,       // don't hijack page scroll
            dragging: !L.Browser.mobile,  // one-finger drag keeps scrolling the page on phones
            attributionControl: true,
        });

        L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png', {
            maxZoom: 15,
            minZoom: 9,
            attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors',
        }).addTo(map);

        const area = L.circle(CENTRE, {
            radius: RADIUS_M,
            color: '#1d4ed8',
            weight: 2,
            opacity: 0.7,
            fillColor: '#1d4ed8',
            fillOpacity: 0.12,
            interactive: false,
        }).addTo(map);

        L.marker(CENTRE, {
            interactive: false,
            keyboard: false,
            icon: L.divIcon({ className: 'area-map-label', html: '<span>Donvale</span>', iconSize: [0, 0] }),
        }).addTo(map);

        map.fitBounds(area.getBounds(), { padding: [12, 12] });
        el.classList.add('is-ready');
    }

    function boot() {
        Promise.all([
            loadAsset('link', { rel: 'stylesheet', href: '/static/vendor/leaflet/leaflet.css' }),
            loadAsset('script', { src: '/static/vendor/leaflet/leaflet.js' }),
        ]).then(init).catch(() => { el.hidden = true; });
    }

    if ('IntersectionObserver' in window) {
        const io = new IntersectionObserver((entries) => {
            if (entries.some(e => e.isIntersecting)) { io.disconnect(); boot(); }
        }, { rootMargin: '300px 0px' });
        io.observe(el);
    } else {
        boot();
    }
})();
