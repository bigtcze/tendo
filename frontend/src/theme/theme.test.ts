import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { applyThemePreference, parseThemePreference, readThemePreference, setThemePreference, THEME_STORAGE_KEY } from './theme';
import { installThemeColorMetas, shippedThemeColorMedia, themeColorMedia } from '../test/themeColor';

describe('parseThemePreference', () => {
  it.each([
    ['light', 'light'],
    ['dark', 'dark'],
    ['system', 'system'],
  ])('accepts stored %s', (value, expected) => {
    expect(parseThemePreference(value)).toBe(expected);
  });

  it.each([null, undefined, '', 'DARK', ' dark', 'blue', '{"theme":"dark"}', 0, true])('treats invalid value %j as system', (value) => {
    expect(parseThemePreference(value)).toBe('system');
  });
});

describe('theme storage', () => {
  beforeEach(() => {
    localStorage.clear();
    document.documentElement.removeAttribute('data-theme');
  });
  afterEach(() => {
    vi.restoreAllMocks();
    localStorage.clear();
    document.documentElement.removeAttribute('data-theme');
  });

  it('reads an explicit saved choice and defaults to system when nothing is saved', () => {
    expect(readThemePreference()).toBe('system');
    localStorage.setItem(THEME_STORAGE_KEY, 'dark');
    expect(readThemePreference()).toBe('dark');
  });

  it('falls back to system for tampered storage values', () => {
    localStorage.setItem(THEME_STORAGE_KEY, 'neon');
    expect(readThemePreference()).toBe('system');
  });

  it('falls back to system when storage throws on read', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new DOMException('blocked', 'SecurityError');
    });
    expect(readThemePreference()).toBe('system');
  });

  it('stores light and dark, and removes the key for system, so storage holds only explicit choices', () => {
    setThemePreference('dark');
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('dark');
    setThemePreference('light');
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('light');
    setThemePreference('system');
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBeNull();
  });

  it('still applies the choice for this page when storage refuses writes', () => {
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new DOMException('full', 'QuotaExceededError');
    });
    setThemePreference('dark');
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark');
  });

  it('does not throw when storage refuses removal', () => {
    vi.spyOn(Storage.prototype, 'removeItem').mockImplementation(() => {
      throw new DOMException('blocked', 'SecurityError');
    });
    expect(() => setThemePreference('system')).not.toThrow();
    expect(document.documentElement.hasAttribute('data-theme')).toBe(false);
  });
});

describe('applyThemePreference', () => {
  beforeEach(() => document.documentElement.removeAttribute('data-theme'));
  afterEach(() => document.documentElement.removeAttribute('data-theme'));

  it('pins data-theme for light and dark so color-scheme follows the explicit choice', () => {
    applyThemePreference('dark');
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark');
    applyThemePreference('light');
    expect(document.documentElement.getAttribute('data-theme')).toBe('light');
  });

  it('removes data-theme for system so the device preference governs through CSS', () => {
    applyThemePreference('dark');
    applyThemePreference('system');
    expect(document.documentElement.hasAttribute('data-theme')).toBe(false);
  });
});

describe('browser theme-color follows the in-app choice', () => {
  beforeEach(() => installThemeColorMetas());
  afterEach(() => {
    document.head.innerHTML = '';
    document.documentElement.removeAttribute('data-theme');
  });

  it('pins the dark tint for an explicit Dark choice, whatever the device prefers', () => {
    applyThemePreference('dark');
    expect(themeColorMedia()).toEqual({ light: 'not all', dark: 'all' });
  });

  it('pins the light tint for an explicit Light choice', () => {
    applyThemePreference('light');
    expect(themeColorMedia()).toEqual({ light: 'all', dark: 'not all' });
  });

  it('hands the tint back to the device for System', () => {
    applyThemePreference('dark');
    applyThemePreference('system');
    expect(themeColorMedia()).toEqual({ light: '(prefers-color-scheme: light)', dark: '(prefers-color-scheme: dark)' });
    // Restoring System must give back exactly the markup index.html ships, so a later markup edit cannot drift.
    expect(themeColorMedia()).toEqual(shippedThemeColorMedia());
  });

  it('updates the tint when the choice is saved, not only on the next load', () => {
    setThemePreference('dark');
    expect(themeColorMedia()).toEqual({ light: 'not all', dark: 'all' });
  });
});
