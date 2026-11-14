import type { Page } from '@playwright/test';

/**
 * The browser chrome tint: the first theme-color meta whose media query matches (a meta without media always does).
 * Headless browsers draw no chrome, so this resolves the tint the way a browser picks it from the document.
 * Init scripts cannot import helpers, so recordParserCompletionTheme in theme.spec.ts keeps an inline copy.
 */
export const effectiveThemeColor = (page: Page) => page.evaluate(() => [...document.querySelectorAll('meta[name="theme-color"]')]
  .find((meta) => !meta.getAttribute('media') || window.matchMedia(meta.getAttribute('media')!).matches)
  ?.getAttribute('content') ?? null);
