// Click-to-enlarge for the screenshots, and nothing else.
//
// The manual is a static file tree that has to work from file:// with no build
// step and no network, so this stays dependency-free and defensive: if anything
// here fails, the pages still read fine — the pictures simply do not zoom.

(function () {
  'use strict';

  var box = document.getElementById('lightbox');
  if (!box) return;
  var img = box.querySelector('img');
  var closeBtn = box.querySelector('.lightbox-close');
  var lastFocus = null;

  function open(src, alt) {
    lastFocus = document.activeElement;
    img.src = src;
    img.alt = alt || '';
    box.hidden = false;
    document.body.style.overflow = 'hidden';
    closeBtn.focus();
  }

  function close() {
    box.hidden = true;
    img.src = '';
    document.body.style.overflow = '';
    // Send focus back where it came from, so keyboard readers do not lose
    // their place in the page after closing a screenshot.
    if (lastFocus && lastFocus.focus) lastFocus.focus();
  }

  document.addEventListener('click', function (e) {
    var btn = e.target.closest ? e.target.closest('.shot-btn') : null;
    if (btn) {
      var full = btn.getAttribute('data-full');
      var inner = btn.querySelector('img');
      if (full) open(full, inner ? inner.alt : '');
      return;
    }
    if (e.target === box || (closeBtn && closeBtn.contains(e.target))) close();
  });

  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape' && !box.hidden) close();
  });
})();
