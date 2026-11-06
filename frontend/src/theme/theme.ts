// Appearance preference: "system" follows the device through CSS light-dark() (no script involved); an explicit
// "light" or "dark" is saved in this browser only and applied before first paint by public/theme-boot.js.
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

/** Sets or clears data-theme. Only an explicit choice pins the scheme; "system" leaves the attribute absent. */
export function applyThemePreference(preference: ThemePreference, root: HTMLElement = document.documentElement): void {
  if (preference === 'system') root.removeAttribute('data-theme');
  else root.setAttribute('data-theme', preference);
}

export function setThemePreference(preference: ThemePreference): void {
  writeThemePreference(preference);
  applyThemePreference(preference);
}
