import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { cn, typeScaleTokens } from './cn';

const css = readFileSync('src/index.css', 'utf8');
// Every `@theme` block (Tailwind allows several, e.g. `@theme inline`), so a size token added in a later block is
// still checked. The blocks hold only declarations, so each ends at its first closing brace.
const themeBlocks = [...css.matchAll(/@theme\b[^{]*\{([^}]*)\}/g)].map(match => match[1]);
const themeBlock = themeBlocks.join('\n');
// Size tokens only: `--text-h1`, not `--text-h1--line-height`, and not the `--text-ink`/`--text-muted` colour roles
// that live outside the @theme blocks.
const declaredSizes = [...new Set([...themeBlock.matchAll(/--text-([a-z0-9-]+?):/g)].map(match => match[1]).filter(name => !name.includes('--')))];

describe('cn', () => {
  it('knows every type-scale size token declared in the theme', () => {
    expect(declaredSizes.length).toBeGreaterThan(0);
    expect([...typeScaleTokens].sort()).toEqual([...declaredSizes].sort());
  });

  it.each(declaredSizes)('keeps text-%s next to a text colour', token => {
    expect(cn(`text-${token}`, 'text-ink')).toBe(`text-${token} text-ink`);
    expect(cn('text-muted', `text-${token}`)).toBe(`text-muted text-${token}`);
  });

  it('keeps the size and colour of a button-like base with a colour override', () => {
    expect(cn('min-h-11 text-control font-semibold text-on-accent', 'text-danger'))
      .toBe('min-h-11 text-control font-semibold text-danger');
  });

  it('still lets a later size replace an earlier size and a later colour replace an earlier colour', () => {
    expect(cn('text-control', 'text-small')).toBe('text-small');
    expect(cn('text-body', 'text-sm')).toBe('text-sm');
    expect(cn('text-ink', 'text-muted')).toBe('text-muted');
  });

  // tailwind-merge's default conflict rule: a later font-size class drops an earlier `leading-*`, because a
  // font-size utility may carry its own line height. Tailwind CSS itself would keep both classes.
  it('drops a leading override that a later size token resets, following tailwind-merge\'s default conflict', () => {
    expect(cn('leading-6', 'text-meta')).toBe('text-meta');
    expect(cn('text-meta', 'leading-6')).toBe('text-meta leading-6');
  });
});

describe('type scale', () => {
  it('keeps form controls at 16 px so iOS Safari does not zoom the page when a field gets focus', () => {
    expect(themeBlock).toMatch(/--text-control:\s*1rem;/);
  });
});
