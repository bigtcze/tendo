import { readFileSync } from 'node:fs';

// Test helper: copies the real theme-color meta tags from index.html into the jsdom head, so the theme code is
// exercised against the shipped markup rather than a hand-written imitation of it.
const indexHtml = readFileSync('index.html', 'utf8');
const themeColorTags = [...indexHtml.matchAll(/<meta\b[^>]*\bname="theme-color"[^>]*>/gi)].map((match) => match[0]);

export function installThemeColorMetas(): void {
  document.head.innerHTML = themeColorTags.join('');
}

function mediaByScheme(root: ParentNode): Record<string, string | null> {
  const media: Record<string, string | null> = {};
  for (const meta of root.querySelectorAll('meta[name="theme-color"]')) {
    media[meta.getAttribute('data-scheme') ?? '?'] = meta.getAttribute('media');
  }
  return media;
}

/** The media query each tagged theme-color meta currently carries, keyed by the scheme it paints. */
export function themeColorMedia(): Record<string, string | null> {
  return mediaByScheme(document.head);
}

/** The media queries exactly as index.html ships them, before any script touches them. */
export function shippedThemeColorMedia(): Record<string, string | null> {
  const template = document.createElement('template');
  template.innerHTML = themeColorTags.join('');
  return mediaByScheme(template.content);
}
