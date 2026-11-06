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
    // Fixed cadence keeps the planned dates (a late finish moves to the next planned date, possibly several intervals on,
    // so it must not promise "one interval after the planned date"); after-completion counts one interval from the finish.
    expect(en['items.repeatExample.fixed']).toContain('planned dates');
    expect(en['items.repeatExample.fixed']).toContain('finish late, the next one is the next planned date');
    expect(cs['items.repeatExample.fixed']).toContain('plánovaných termínech');
    expect(cs['items.repeatExample.fixed']).toContain('dokončíte později, další bude v nejbližším plánovaném termínu');
    for (const catalog of [en, cs]) expect(catalog['items.repeatExample.fixed']).not.toContain('{interval}');
    expect(en['items.repeatExample.after']).toContain('{interval} after the day you finish');
    expect(cs['items.repeatExample.after']).toContain('za {interval} ode dne, kdy ji dokončíte');
  });

  it('keeps internal and technical vocabulary out of user-facing copy', () => {
    const jargon = /\b(etag|tenant|fluid|api|workflow|cursor|idempotenc\w*|recurrence|payload|http|uuid|sla)\b|overdue|po termínu|zpožděn/i;
    for (const [key, value] of [...Object.entries(en), ...Object.entries(cs)]) {
      expect(value, key).not.toMatch(jargon);
    }
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
