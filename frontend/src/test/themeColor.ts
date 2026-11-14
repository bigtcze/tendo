import { readFileSync } from 'node:fs';

// Test helper: copies the real theme-color meta tags from index.html into the jsdom head, so the theme code is
// exercised against the shipped markup rather than a hand-written imitation of it.
const indexHtml = readFileSync('index.html', 'utf8');
const themeColorTags = [...indexHtml.matchAll(/<meta\b[^>]*\bname="theme-color"[^>]*>/gi)].map((match) => match[0]);

export function installThemeColorMetas(): void {
  document.head.innerHTML = themeColorTags.join('');
}

/** The media query each tagged theme-color meta currently carries, keyed by the scheme it paints. */
export function themeColorMedia(): Record<string, string | null> {
  const media: Record<string, string | null> = {};
  for (const meta of document.head.querySelectorAll('meta[name="theme-color"]')) {
    media[meta.getAttribute('data-scheme') ?? '?'] = meta.getAttribute('media');
  }
  return media;
}
