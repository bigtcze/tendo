import { readFileSync } from 'node:fs';
import { URL as NodeURL, fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';
import { designTokens } from './designTokens';

// The shell's brand assets are plain files: index.html links them, Vite copies public/ to the dist root,
// and the Go handler serves them by extension. These checks keep the declared links, the manifest and the
// drawn colours from drifting apart from each other and from the design tokens.
// Node's URL is used explicitly: in this jsdom environment the global URL resolves relative paths against
// the document origin instead of the module's file: base.
const publicPath = fileURLToPath(new NodeURL('../public/', import.meta.url));
const readPublic = (href: string) => readFileSync(`${publicPath}${href.replace(/^\//, '')}`);
const html = readFileSync(fileURLToPath(new NodeURL('../index.html', import.meta.url)), 'utf8');

function attribute(tag: string, name: string): string | undefined {
  return new RegExp(`\\b${name}="([^"]*)"`).exec(tag)?.[1];
}
const tag = (name: 'link' | 'meta') => [...html.matchAll(new RegExp(`<${name}\\b[^>]*>`, 'gi'))].map((match) => match[0]);
const links = tag('link');
const metas = tag('meta');

describe('shell brand assets', () => {
  it('keeps the Tendo document title', () => {
    expect(html).toMatch(/<title>Tendo<\/title>/);
  });

  it('links an SVG icon, an iOS touch icon and the web app manifest to files that exist', () => {
    for (const expected of [
      { rel: 'icon', href: '/favicon.svg', type: 'image/svg+xml' },
      { rel: 'apple-touch-icon', href: '/apple-touch-icon.png', type: undefined },
      { rel: 'manifest', href: '/manifest.webmanifest', type: undefined },
    ]) {
      const link = links.find((candidate) => attribute(candidate, 'rel') === expected.rel);
      expect(link, `rel="${expected.rel}"`).toBeDefined();
      expect(attribute(link!, 'href'), `rel="${expected.rel}" href`).toBe(expected.href);
      if (expected.type) expect(attribute(link!, 'type'), `rel="${expected.rel}" type`).toBe(expected.type);
      expect(() => readPublic(expected.href), `public${expected.href}`).not.toThrow();
    }
  });

  it('declares light and dark theme colours from the paper token', () => {
    const themeColors = metas
      .filter((meta) => attribute(meta, 'name') === 'theme-color')
      .map((meta) => [attribute(meta, 'media'), attribute(meta, 'content')]);
    expect(themeColors).toEqual([
      ['(prefers-color-scheme: light)', designTokens.light.paper],
      ['(prefers-color-scheme: dark)', designTokens.dark.paper],
    ]);
    const colorScheme = metas.find((meta) => attribute(meta, 'name') === 'color-scheme');
    expect(attribute(colorScheme!, 'content')).toBe('light dark');
  });

  it('describes an ordinary browser app with matching Tendo icons', () => {
    const manifest = JSON.parse(readPublic('/manifest.webmanifest').toString('utf8')) as {
      name: string; short_name: string; start_url: string; display: string;
      background_color: string; theme_color: string;
      icons: Array<{ src: string; sizes: string; type: string; purpose?: string }>;
    };
    expect(manifest.name).toBe('Tendo');
    expect(manifest.short_name).toBe('Tendo');
    expect(manifest.start_url).toBe('/');
    expect(manifest.display).toBe('browser');
    expect(manifest.background_color).toBe(designTokens.light.paper);
    // The manifest tint matches the document's theme-color meta tags, which follow the paper token.
    expect(manifest.theme_color).toBe(designTokens.light.paper);
    expect(manifest.icons.map((icon) => icon.sizes)).toEqual(['192x192', '512x512']);
    for (const icon of manifest.icons) {
      const bytes = readPublic(icon.src);
      expect(icon.type, icon.src).toBe('image/png');
      // PNG signature, then the IHDR width/height the manifest declares in `sizes`.
      expect([...bytes.subarray(0, 8)], icon.src).toEqual([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]);
      const [width, height] = icon.sizes.split('x').map(Number);
      expect([bytes.readUInt32BE(16), bytes.readUInt32BE(20)], `${icon.src} IHDR`).toEqual([width, height]);
    }
  });

  it('draws the brand tile in both browser themes from the accent tokens', () => {
    const svg = readPublic('/favicon.svg').toString('utf8');
    // A 32-unit square tile keeps the glyph crisp at 16 and 32 px without relying on an external font.
    expect(svg).toMatch(/viewBox="0 0 32 32"/);
    expect(svg).not.toContain('<text');
    const darkRule = /@media\s*\(\s*prefers-color-scheme:\s*dark\s*\)\s*\{([\s\S]*)\}/i.exec(svg);
    expect(darkRule, 'a prefers-color-scheme dark rule').not.toBeNull();
    // The default (light) declarations must hold outside the dark override, so a browser without dark support
    // and the raster exports keep the light tile.
    const defaultPart = svg.replace(darkRule![0], '').toLowerCase();
    expect(defaultPart).toContain(designTokens.light.accent.toLowerCase());
    expect(defaultPart).toContain(designTokens.light.onAccent.toLowerCase());
    expect(defaultPart).not.toContain(designTokens.dark.accent.toLowerCase());
    expect(darkRule![1].toLowerCase()).toContain(designTokens.dark.accent.toLowerCase());
    expect(darkRule![1].toLowerCase()).toContain(designTokens.dark.onAccent.toLowerCase());
  });
});
