import { describe, expect, it } from 'vitest';
import { cs } from './cs';
import { en } from './en';

describe('locale catalogs', () => {
  it('Czech and English define identical key sets', () => {
    expect(Object.keys(cs).sort()).toEqual(Object.keys(en).sort());
  });

  it('has no empty translations', () => {
    for (const [key, value] of [...Object.entries(en), ...Object.entries(cs)]) {
      expect(value.trim(), key).not.toBe('');
    }
  });

  it('keeps placeholders in sync', () => {
    for (const key of Object.keys(en) as (keyof typeof en)[]) {
      const placeholders = (s: string) => (s.match(/\{\w+\}/g) ?? []).sort();
      expect(placeholders(cs[key]), key).toEqual(placeholders(en[key]));
    }
  });
});
