import { readFileSync } from 'node:fs';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { applyThemePreference, THEME_PREFERENCES, THEME_STORAGE_KEY } from './theme';
import { installThemeColorMetas, themeColorMedia } from '../test/themeColor';

// The boot script is classic, dependency-free JavaScript; run its source exactly as the browser would.
const bootSource = readFileSync('public/theme-boot.js', 'utf8');
const runBoot = () => new Function(bootSource)();

describe('public/theme-boot.js', () => {
  beforeEach(() => {
    localStorage.clear();
    document.documentElement.removeAttribute('data-theme');
  });
  afterEach(() => {
    vi.restoreAllMocks();
    localStorage.clear();
    document.documentElement.removeAttribute('data-theme');
  });

  it.each(['light', 'dark'])('applies a saved %s choice before the app loads', (value) => {
    localStorage.setItem(THEME_STORAGE_KEY, value);
    runBoot();
    expect(document.documentElement.getAttribute('data-theme')).toBe(value);
  });

  it.each([null, 'system', 'neon', ''])('leaves the device in charge for %j', (value) => {
    if (value !== null) localStorage.setItem(THEME_STORAGE_KEY, value);
    runBoot();
    expect(document.documentElement.hasAttribute('data-theme')).toBe(false);
  });

  it('does not throw when storage is blocked', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new DOMException('blocked', 'SecurityError');
    });
    expect(() => runBoot()).not.toThrow();
    expect(document.documentElement.hasAttribute('data-theme')).toBe(false);
  });

  it.each(['light', 'dark'] as const)('pins the browser theme-color for a saved %s choice exactly as the app does', (value) => {
    installThemeColorMetas();
    applyThemePreference(value);
    const fromApp = themeColorMedia();
    installThemeColorMetas();
    localStorage.setItem(THEME_STORAGE_KEY, value);
    runBoot();
    expect(themeColorMedia()).toEqual(fromApp);
    expect(fromApp[value]).toBe('all');
    document.head.innerHTML = '';
  });

  it('leaves the device-driven theme-color untouched without a saved choice', () => {
    installThemeColorMetas();
    runBoot();
    expect(themeColorMedia()).toEqual({ light: '(prefers-color-scheme: light)', dark: '(prefers-color-scheme: dark)' });
    document.head.innerHTML = '';
  });

  it('uses the same storage key and accepted values as the theme module', () => {
    expect(bootSource).toContain(`'${THEME_STORAGE_KEY}'`);
    for (const preference of THEME_PREFERENCES.filter((value) => value !== 'system')) {
      expect(bootSource).toContain(`'${preference}'`);
    }
  });

  it('stays an external classic script so the strict CSP script-src self needs no change', () => {
    expect(bootSource).not.toMatch(/\bimport\b|\bexport\b/);
  });
});
