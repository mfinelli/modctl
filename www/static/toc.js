(function () {
  'use strict';

  // Mobile collapsible
  const tocToggle = document.getElementById('toc-toggle');
  const tocList = document.getElementById('toc-list');

  if (tocToggle && tocList) {
    tocToggle.addEventListener('click', function () {
      const open = tocList.classList.toggle('is-open');
      tocToggle.setAttribute('aria-expanded', open);
    });
  }

  // Scroll highlighting
  const tocLinks = document.querySelectorAll('.docs-toc a');
  if (!tocLinks.length) return;

  // Build a map of id -> toc link
  const linkMap = {};
  tocLinks.forEach(function (link) {
    const id = new URL(link.href).hash.slice(1);
    if (id) linkMap[id] = link;
  });

  const headings = Array.from(
    document.querySelectorAll('#main-content h1, #main-content h2, #main-content h3')
  ).filter(h => h.id && linkMap[h.id]);

  if (!headings.length) return;

  let activeId = null;

  function setActive(id) {
    if (id === activeId) return;
    activeId = id;
    tocLinks.forEach(l => l.classList.remove('active'));
    if (id && linkMap[id]) {
      linkMap[id].classList.add('active');
    }
  }

  // Use IntersectionObserver to track which heading is nearest the top
  const observer = new IntersectionObserver(function (entries) {
    entries.forEach(function (entry) {
      entry.target.dataset.visible = entry.isIntersecting ? '1' : '0';
    });

    // Find the topmost visible heading
    const visible = headings.filter(h => h.dataset.visible === '1');
    if (visible.length) {
      setActive(visible[0].id);
      return;
    }

    // If no heading is visible, find the last one that scrolled past the top
    let closest = null;
    for (let i = headings.length - 1; i >= 0; i--) {
      if (headings[i].getBoundingClientRect().top <= 80) {
        closest = headings[i];
        break;
      }
    }
    if (closest) setActive(closest.id);
  }, {
    rootMargin: '-60px 0px -80% 0px',
    threshold: 0,
  });

  headings.forEach(h => observer.observe(h));
}());
