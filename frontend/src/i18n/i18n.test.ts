import { describe, expect, it } from 'vitest';
import { cs } from './cs';
import { en } from './en';

describe('locale catalogs', () => {
  it('Czech and English define identical key sets', () => {
    expect(Object.keys(cs).sort()).toEqual(Object.keys(en).sort());
  });

  it.each([
    { locale: 'en', unit: 'month', count: 1, expected: 'month' },
    { locale: 'en', unit: 'month', count: 2, expected: 'months' },
    { locale: 'cs', unit: 'month', count: 1, expected: 'měsíc' },
    { locale: 'cs', unit: 'month', count: 2, expected: 'měsíce' },
    { locale: 'cs', unit: 'month', count: 5, expected: 'měsíců' },
  ])('plural rules give natural $locale unit words for $count $unit', ({ locale, unit, count, expected }) => {
    const category = new Intl.PluralRules(locale).select(count);
    const catalog = locale === 'cs' ? cs : en;
    const unitWord = catalog[`items.unitWord.${unit}.${category}` as keyof typeof en];
    expect(unitWord).toBe(expected);
  });

  it('uses warm, concrete examples and natural repeat copy', () => {
    expect(en['items.title.placeholder']).toContain('Check');
    expect(cs['items.title.placeholder']).toContain('Zkontrolovat');
    expect(en['items.repeatExample.fixed']).toContain('even if you finish late');
    expect(cs['items.repeatExample.fixed']).toContain('později');
    expect(en['items.repeatExample.after']).toContain('{interval} after you finish');
    expect(cs['items.repeatExample.after']).toContain('{interval} po dokončení');
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
