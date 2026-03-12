(function () {
  'use strict';

  const toggle = document.getElementById('nav-toggle');
  const target = document.getElementById('nav-menu');

  if (!toggle || !target) return;

  toggle.addEventListener('click', function () {
    const open = target.classList.toggle('is-open');
    toggle.setAttribute('aria-expanded', open);
  });

  // Close when clicking the overlay
  const overlay = document.getElementById('docs-overlay');
  if (overlay) {
    overlay.addEventListener('click', function () {
      target.classList.remove('is-open');
      toggle.setAttribute('aria-expanded', false);
    });
  }

  // Close when clicking outside
  document.addEventListener('click', function (e) {
    if (!toggle.contains(e.target) && !target.contains(e.target)) {
      target.classList.remove('is-open');
      toggle.setAttribute('aria-expanded', false);
    }
  });

  // Close when a nav link is clicked (useful on docs sidebar)
  target.querySelectorAll('a').forEach(function (link) {
    link.addEventListener('click', function () {
      target.classList.remove('is-open');
      toggle.setAttribute('aria-expanded', false);
    });
  });
}());
