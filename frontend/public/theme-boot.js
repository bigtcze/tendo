// Applies a saved explicit light/dark choice before first paint. Classic, synchronous, same-origin: the CSP allows
// script-src 'self' only, so this stays an external file (no inline script, no CSP change). Must mirror
// src/theme/theme.ts (THEME_STORAGE_KEY, the accepted values and the theme-color media switch); theme-boot.test.ts
// checks the parity.
(function () {
  try {
    var stored = window.localStorage.getItem('tendo.theme');
    if (stored === 'light' || stored === 'dark') {
      document.documentElement.setAttribute('data-theme', stored);
      // Browser chrome tint: keep only the theme-color meta of the chosen scheme (theme.ts applyThemeColor).
      // This script sits after the theme-color metas in index.html, so they are already parsed.
      var metas = document.querySelectorAll('meta[name="theme-color"][data-scheme]');
      for (var i = 0; i < metas.length; i++) {
        metas[i].setAttribute('media', metas[i].getAttribute('data-scheme') === stored ? 'all' : 'not all');
      }
    }
  } catch {
    // Storage blocked: no attribute, so the device decides ("system").
  }
})();
