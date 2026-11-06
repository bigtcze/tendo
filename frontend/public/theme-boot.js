// Applies a saved explicit light/dark choice before first paint. Classic, synchronous, same-origin: the CSP allows
// script-src 'self' only, so this stays an external file (no inline script, no CSP change). Must mirror
// src/theme/theme.ts (THEME_STORAGE_KEY and the accepted values); theme-boot.test.ts checks the parity.
(function () {
  try {
    var stored = window.localStorage.getItem('tendo.theme');
    if (stored === 'light' || stored === 'dark') document.documentElement.setAttribute('data-theme', stored);
  } catch {
    // Storage blocked: no attribute, so the device decides ("system").
  }
})();
