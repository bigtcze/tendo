import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { applyThemePreference, parseThemePreference, readThemePreference, setThemePreference, THEME_STORAGE_KEY } from './theme';

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
