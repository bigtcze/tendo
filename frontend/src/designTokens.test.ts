import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { contrastRatio } from '../e2e/contrast';
import { designTokens } from './designTokens';

const css = readFileSync('src/index.css', 'utf8');
const colorsIn = (block: string) => Object.fromEntries([...block.matchAll(/(--[\w-]+):\s*(#[0-9a-fA-F]{6})/g)].map(([, key, value]) => [key, value.toLowerCase()]));
// Light values are the plain :root fallback; every token also has one light-dark(light, dark) declaration that the browser resolves per color-scheme.
const fallbackLight = colorsIn(css.match(/:root\s*\{([^}]+)\}/)?.[1] ?? '');
const lightDarkPairs = Object.fromEntries(
  [...css.matchAll(/(--[\w-]+):\s*light-dark\((#[0-9a-fA-F]{6}),\s*(#[0-9a-fA-F]{6})\)/g)].map(([, key, light, dark]) => [key, { light: light.toLowerCase(), dark: dark.toLowerCase() }]),
);
const cssColors = (theme: 'light' | 'dark') => theme === 'light'
  ? fallbackLight
  : Object.fromEntries(Object.entries(lightDarkPairs).map(([key, pair]) => [key, pair.dark]));
function hex(value: string) {
  const match = value.match(/^#([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i);
  if (!match) throw new Error(`Invalid token hex: ${value}`);
  return { r: Number.parseInt(match[1], 16), g: Number.parseInt(match[2], 16), b: Number.parseInt(match[3], 16), a: 1 };
}
const cssNames = {
  paper: '--surface-paper', raised: '--surface-raised', sand: '--surface-sunken', ink: '--text-ink', muted: '--text-muted',
  line: '--border-line', field: '--border-field', accent: '--accent', accentStrong: '--accent-strong', onAccent: '--on-accent', focus: '--focus-ring',
  navSelected: '--nav-selected', needsAttention: '--state-needs-attention', needsAttentionBg: '--state-needs-attention-bg', inProgress: '--state-in-progress', inProgressBg: '--state-in-progress-bg',
  waiting: '--state-waiting', waitingBg: '--state-waiting-bg', upcoming: '--state-upcoming', upcomingBg: '--state-upcoming-bg', paused: '--state-paused', pausedBg: '--state-paused-bg',
  done: '--state-done', doneBg: '--state-done-bg', danger: '--state-danger', dangerBg: '--state-danger-bg', success: '--state-success', successBg: '--state-success-bg',
} as const;
type TokenName = keyof typeof designTokens.light;
type Pair = { label: string; foreground: TokenName; background: TokenName; minimum: number; light: number; dark: number };
const documentedPairs: Pair[] = [
  { label: 'Ink / paper', foreground: 'ink', background: 'paper', minimum: 4.5, light: 16.08, dark: 15.75 },
  { label: 'Muted / paper', foreground: 'muted', background: 'paper', minimum: 4.5, light: 6.23, dark: 7.36 },
  { label: 'Ink / raised', foreground: 'ink', background: 'raised', minimum: 4.5, light: 17.1, dark: 14.52 },
  { label: 'Muted / raised', foreground: 'muted', background: 'raised', minimum: 4.5, light: 6.63, dark: 6.79 },
  { label: 'Ink / sunken', foreground: 'ink', background: 'sand', minimum: 4.5, light: 14.57, dark: 13.49 },
  { label: 'Muted / sunken', foreground: 'muted', background: 'sand', minimum: 4.5, light: 5.65, dark: 6.31 },
  { label: 'On-accent / accent', foreground: 'onAccent', background: 'accent', minimum: 4.5, light: 6.49, dark: 9.48 },
  { label: 'On-accent / accent-strong', foreground: 'onAccent', background: 'accentStrong', minimum: 4.5, light: 9.23, dark: 11.71 },
  { label: 'Accent / paper', foreground: 'accent', background: 'paper', minimum: 4.5, light: 6.05, dark: 10.18 },
  { label: 'Accent / raised', foreground: 'accent', background: 'raised', minimum: 4.5, light: 6.44, dark: 9.38 },
  { label: 'Focus / paper', foreground: 'focus', background: 'paper', minimum: 3, light: 6.05, dark: 11.75 },
  { label: 'Focus / raised', foreground: 'focus', background: 'raised', minimum: 3, light: 6.44, dark: 10.83 },
  { label: 'Field border / paper', foreground: 'field', background: 'paper', minimum: 3, light: 3.71, dark: 4.93 },
  { label: 'Control border / raised', foreground: 'field', background: 'raised', minimum: 3, light: 3.95, dark: 4.54 },
  { label: 'Switch unchecked track / thumb', foreground: 'field', background: 'raised', minimum: 3, light: 3.95, dark: 4.54 },
  { label: 'Switch selected track / thumb', foreground: 'accent', background: 'raised', minimum: 3, light: 6.44, dark: 9.38 },
  { label: 'Nav text / selected', foreground: 'ink', background: 'navSelected', minimum: 4.5, light: 13.43, dark: 12.78 },
  { label: 'Accent / selected nav', foreground: 'accent', background: 'navSelected', minimum: 3, light: 5.06, dark: 8.26 },
  { label: 'Needs attention / subtle', foreground: 'needsAttention', background: 'needsAttentionBg', minimum: 4.5, light: 5.34, dark: 7.61 },
  { label: 'In progress / subtle', foreground: 'inProgress', background: 'inProgressBg', minimum: 4.5, light: 5.59, dark: 7.71 },
  { label: 'Waiting / subtle', foreground: 'waiting', background: 'waitingBg', minimum: 4.5, light: 5.23, dark: 8.36 },
  { label: 'Coming up / subtle', foreground: 'upcoming', background: 'upcomingBg', minimum: 4.5, light: 5.34, dark: 7.97 },
  { label: 'Ink / coming up subtle', foreground: 'ink', background: 'upcomingBg', minimum: 4.5, light: 14.75, dark: 12.75 },
  { label: 'Paused / subtle', foreground: 'paused', background: 'pausedBg', minimum: 5, light: 5.37, dark: 6.66 },
  { label: 'Done / subtle', foreground: 'done', background: 'doneBg', minimum: 4.5, light: 5.31, dark: 8.55 },
  { label: 'Error / subtle', foreground: 'danger', background: 'dangerBg', minimum: 4.5, light: 5.89, dark: 7.57 },
  { label: 'Success / subtle', foreground: 'success', background: 'successBg', minimum: 4.5, light: 5.31, dark: 8.55 },
];

describe('CSS theme declarations', () => {
  it('pairs every token light-dark() with a light fallback that cannot drift', () => {
    expect(Object.keys(lightDarkPairs).sort()).toEqual(Object.keys(fallbackLight).sort());
    for (const [key, pair] of Object.entries(lightDarkPairs)) expect(pair.light, key).toBe(fallbackLight[key]);
  });

  it('pins color-scheme to the explicit preference and lets the device decide by default', () => {
    const lightDarkBoundary = css.slice(css.indexOf('@supports (color: light-dark('), css.indexOf('\n}\n\n@keyframes settle'));
    expect(lightDarkBoundary).toContain(":root[data-theme='light'] { color-scheme: light; }");
    expect(lightDarkBoundary).toContain(":root[data-theme='dark'] { color-scheme: dark; }");
    expect(lightDarkBoundary).toContain('color-scheme: light dark;');
    expect(lightDarkBoundary).toContain('@media (prefers-color-scheme: dark)');
    expect(lightDarkBoundary).toContain(":root:not([data-theme]) { color-scheme: dark; }");
    expect(lightDarkBoundary.match(/@media \(prefers-color-scheme: dark\)/g)).toHaveLength(1);
    expect(css.slice(0, css.indexOf('@supports (color: light-dark('))).toContain('color-scheme: light;');
    expect(css.slice(0, css.indexOf('@supports (color: light-dark('))).not.toContain('color-scheme: dark;');
  });
});

for (const theme of ['light', 'dark'] as const) {
  describe(`${theme} design tokens`, () => {
    it('matches CSS source and documented contrast ratios', () => {
      const cssValues = cssColors(theme);
      const tokens = designTokens[theme];
      for (const [key, cssKey] of Object.entries(cssNames)) expect(cssValues[cssKey]).toBe(tokens[key as TokenName]);
      for (const pair of documentedPairs) {
        const ratio = contrastRatio(hex(tokens[pair.foreground]), hex(tokens[pair.background]));
        expect(ratio, `${theme}: ${pair.label}`).toBeGreaterThanOrEqual(pair.minimum);
        expect(Number(ratio.toFixed(2)), `${theme}: documented ${pair.label}`).toBe(theme === 'light' ? pair.light : pair.dark);
      }
    });
  });
}
