// Appearance preference: "system" follows the device through CSS light-dark() (no script involved); an explicit
// "light" or "dark" is saved in this browser only and applied before first paint by public/theme-boot.js, both to
// the page (data-theme) and to the browser chrome tint (theme-color).
// Storage and attribute writes stay here; the boot script mirrors THEME_STORAGE_KEY and the two accepted values.

export const THEME_STORAGE_KEY = 'tendo.theme';
export const THEME_PREFERENCES = ['system', 'light', 'dark'] as const;
export type ThemePreference = (typeof THEME_PREFERENCES)[number];

/** Anything other than an explicit light/dark value means "follow the device" (missing, invalid or tampered storage). */
export function parseThemePreference(value: unknown): ThemePreference {
  return value === 'light' || value === 'dark' ? value : 'system';
}

export function readThemePreference(): ThemePreference {
  try {
    return parseThemePreference(window.localStorage.getItem(THEME_STORAGE_KEY));
  } catch {
    // Storage can be blocked (private modes, strict settings); the device decides, as for a first visit.
    return 'system';
  }
}

function writeThemePreference(preference: ThemePreference): void {
  try {
    if (preference === 'system') window.localStorage.removeItem(THEME_STORAGE_KEY);
    else window.localStorage.setItem(THEME_STORAGE_KEY, preference);
  } catch {
    // The choice still applies to this page; it simply will not survive a reload when storage is unavailable.
  }
}

/**
 * Points the browser chrome tint (theme-color) at the chosen theme. index.html declares one tagged meta per scheme
 * (`data-scheme`); an explicit choice keeps only its own (`all` / `not all`), "system" restores the device query.
 * Only the media query changes, so the colours stay in index.html. public/theme-boot.js mirrors this before first paint.
 */
function applyThemeColor(preference: ThemePreference, doc: Document): void {
  for (const meta of doc.querySelectorAll<HTMLMetaElement>('meta[name="theme-color"][data-scheme]')) {
    const scheme = meta.getAttribute('data-scheme');
    if (preference === 'system') meta.setAttribute('media', `(prefers-color-scheme: ${scheme})`);
    else meta.setAttribute('media', scheme === preference ? 'all' : 'not all');
  }
}

/** Sets or clears data-theme. Only an explicit choice pins the scheme; "system" leaves the attribute absent. */
export function applyThemePreference(preference: ThemePreference, root: HTMLElement = document.documentElement): void {
  if (preference === 'system') root.removeAttribute('data-theme');
  else root.setAttribute('data-theme', preference);
  applyThemeColor(preference, root.ownerDocument);
}

export function setThemePreference(preference: ThemePreference): void {
  writeThemePreference(preference);
  applyThemePreference(preference);
}
