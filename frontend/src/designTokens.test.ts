import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { contrastRatio } from '../e2e/contrast';
import { designTokens } from './designTokens';

const css = readFileSync('src/index.css', 'utf8');
const cssColors = (selector: string) => {
  const block = css.match(new RegExp(`${selector}\\s*\\{([^}]+)\\}`))?.[1] ?? '';
  return Object.fromEntries([...block.matchAll(/(--[\w-]+):\s*(#[0-9a-fA-F]{6})/g)].map(([, key, value]) => [key, value.toLowerCase()]));
};
function hex(value: string) {
  const match = value.match(/^#([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i);
  if (!match) throw new Error(`Invalid token hex: ${value}`);
  return { r: Number.parseInt(match[1], 16), g: Number.parseInt(match[2], 16), b: Number.parseInt(match[3], 16), a: 1 };
}
const cssNames = {
  paper: '--surface-paper', raised: '--surface-raised', sand: '--surface-sunken', ink: '--text-ink', muted: '--text-muted',
  line: '--border-line', field: '--border-field', accent: '--accent', accentStrong: '--accent-strong', onAccent: '--on-accent', focus: '--focus-ring',
  needsAttention: '--state-needs-attention', needsAttentionBg: '--state-needs-attention-bg', inProgress: '--state-in-progress', inProgressBg: '--state-in-progress-bg',
  waiting: '--state-waiting', waitingBg: '--state-waiting-bg', paused: '--state-paused', pausedBg: '--state-paused-bg', done: '--state-done', doneBg: '--state-done-bg',
  danger: '--state-danger', dangerBg: '--state-danger-bg', success: '--state-success', successBg: '--state-success-bg',
} as const;
type TokenName = keyof typeof designTokens.light;
type Pair = { label: string; foreground: TokenName; background: TokenName; minimum: number; light: number; dark: number };
const documentedPairs: Pair[] = [
  { label: 'Ink / paper', foreground: 'ink', background: 'paper', minimum: 4.5, light: 12.79, dark: 14.06 },
  { label: 'Muted / paper', foreground: 'muted', background: 'paper', minimum: 4.5, light: 5.66, dark: 9.61 },
  { label: 'Ink / raised', foreground: 'ink', background: 'raised', minimum: 4.5, light: 13.67, dark: 11.97 },
  { label: 'Muted / raised', foreground: 'muted', background: 'raised', minimum: 4.5, light: 6.05, dark: 8.18 },
  { label: 'Ink / sunken', foreground: 'ink', background: 'sand', minimum: 4.5, light: 11.32, dark: 10.06 },
  { label: 'Muted / sunken', foreground: 'muted', background: 'sand', minimum: 4.5, light: 5.01, dark: 6.87 },
  { label: 'On-accent / accent', foreground: 'onAccent', background: 'accent', minimum: 4.5, light: 6.25, dark: 8.79 },
  { label: 'On-accent / accent-strong', foreground: 'onAccent', background: 'accentStrong', minimum: 4.5, light: 9.27, dark: 11.03 },
  { label: 'Accent / paper', foreground: 'accent', background: 'paper', minimum: 4.5, light: 5.85, dark: 8.73 },
  { label: 'Focus / paper', foreground: 'focus', background: 'paper', minimum: 3, light: 8.67, dark: 10.96 },
  { label: 'Field border / paper', foreground: 'field', background: 'paper', minimum: 3, light: 3.85, dark: 7.48 },
  { label: 'Needs attention / subtle', foreground: 'needsAttention', background: 'needsAttentionBg', minimum: 4.5, light: 6.14, dark: 7.8 },
  { label: 'In progress / subtle', foreground: 'inProgress', background: 'inProgressBg', minimum: 4.5, light: 6.23, dark: 7.91 },
  { label: 'Waiting / subtle', foreground: 'waiting', background: 'waitingBg', minimum: 4.5, light: 5.63, dark: 7.21 },
  { label: 'Paused / subtle', foreground: 'paused', background: 'pausedBg', minimum: 4.5, light: 5.45, dark: 7.01 },
  { label: 'Done / subtle', foreground: 'done', background: 'doneBg', minimum: 4.5, light: 5.96, dark: 7.29 },
  { label: 'Error / subtle', foreground: 'danger', background: 'dangerBg', minimum: 4.5, light: 5.59, dark: 7.44 },
  { label: 'Success / subtle', foreground: 'success', background: 'successBg', minimum: 4.5, light: 5.96, dark: 7.29 },
];

for (const theme of ['light', 'dark'] as const) {
  describe(`${theme} design tokens`, () => {
    it('matches CSS source and documented contrast ratios', () => {
      const selector = theme === 'light' ? ':root' : ":root\\[data-theme='dark'\\]";
      const cssValues = cssColors(selector);
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
