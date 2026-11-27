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

  it('never sends the person to server files or settings names', () => {
    // The person setting up a household may not run the server; server file and variable names belong in the admin
    // docs, never in the app (UX "Language": human wording only).
    const serverInternals = /(?<![\w.])\.env\b|\bTENDO_[A-Z_]+|\benvironment variable|proměnn\p{L}* prostředí/iu;
    for (const [key, value] of [...Object.entries(en), ...Object.entries(cs)]) {
      expect(value, key).not.toMatch(serverInternals);
    }
  });

  it('names the setup code in its errors the way its field label does', () => {
    // A second name for the same code ("instalační kód" next to the "Jednorázový kód …" label) reads as a different
    // code the person was never given.
    const keys = ['onboarding.error.setupCodeEmpty', 'onboarding.error.setupCode'] as const;
    for (const key of keys) {
      expect(en[key], key).toMatch(/\bsetup code\b/i);
      expect(cs[key], key).toMatch(/(?<!\p{L})jednorázový kód(?!\p{L})/iu);
    }
    for (const [key, value] of Object.entries(cs)) expect(value, key).not.toMatch(/instalačn\p{L}*\s+kód/iu);
  });

  it('never blames the person’s connection for a failure Tendo cannot attribute to it', () => {
    // A failed request is as often a server error (5xx) as a lost network, and the client cannot tell which, so
    // failure copy asks to try again instead of sending the person to check a connection that may be fine.
    const blamesConnection = /check your connection|connection and try|zkontrolujte připojení/i;
    for (const [key, value] of [...Object.entries(en), ...Object.entries(cs)]) {
      expect(value, key).not.toMatch(blamesConnection);
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
