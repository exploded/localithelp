// Scroll-triggered reveal animation
const observer = new IntersectionObserver((entries) => {
    entries.forEach((entry) => {
        if (entry.isIntersecting) {
            const siblings = entry.target.parentElement.querySelectorAll('.reveal');
            const index = Array.from(siblings).indexOf(entry.target);
            entry.target.style.transitionDelay = `${index * 0.08}s`;
            entry.target.classList.add('visible');
            observer.unobserve(entry.target);
        }
    });
}, { threshold: 0.15 });

document.querySelectorAll('.reveal').forEach(el => observer.observe(el));
