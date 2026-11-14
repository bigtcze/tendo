import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { cn, typeScaleTokens } from './cn';

const css = readFileSync('src/index.css', 'utf8');
const themeBlock = css.slice(css.indexOf('@theme {'), css.indexOf('}', css.indexOf('@theme {')));
// Size tokens only: `--text-h1`, not `--text-h1--line-height`, and not the `--text-ink`/`--text-muted` colour roles
// that live outside the @theme block.
const declaredSizes = [...themeBlock.matchAll(/--text-([a-z0-9-]+?):/g)].map(match => match[1]).filter(name => !name.includes('--'));

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

  it('drops a leading override that a later size token resets, as Tailwind font-size utilities do', () => {
    expect(cn('leading-6', 'text-meta')).toBe('text-meta');
    expect(cn('text-meta', 'leading-6')).toBe('text-meta leading-6');
  });
});
