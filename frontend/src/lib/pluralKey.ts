import type { MessageKey } from '../i18n';

/**
 * The message key that agrees with a count in the reader's language: the caller names the family (`items.group.count`,
 * `install.add.count`) and this picks the category both catalogs define. Czech needs `one`, `few` and `other`;
 * English uses `one` and `other`; any other category (fractions) reads as `other`.
 */
export function pluralKey(prefix: string, count: number, locale: string): MessageKey {
  const category = new Intl.PluralRules(locale).select(count);
  const supported: Record<string, readonly string[]> = {
    en: ['one', 'other'],
    cs: ['one', 'few', 'other'],
  };
  return `${prefix}.${supported[locale]?.includes(category) ? category : 'other'}` as MessageKey;
}
