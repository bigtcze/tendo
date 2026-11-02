import type { MessageKey } from '../../i18n';
import type { Recurrence } from './itemsApi';

type Translate = (key: MessageKey, values?: Record<string, string>) => string;

/** One-line row summary: the interval plus whether the next date keeps the plan (Repeat) or follows completion (Fluid). */
export function repeatSummary(t: Translate, recurrence: Recurrence, locale: string): string {
  return `${intervalWords(t, recurrence, locale)} · ${t(recurrence.mode === 'after_completion' ? 'items.repeatMode.fluid' : 'items.repeatMode.fixed')}`;
}

/** "2 weeks" / "5 let": the count with its plural-aware unit word, without string surgery on a sentence. */
export function intervalWords(t: Translate, recurrence: Recurrence, locale: string): string {
  return `${recurrence.intervalValue} ${unitWord(t, locale, recurrence.intervalValue, recurrence.intervalUnit)}`;
}

/** The unit word that agrees with a count ("1 rok", "2 roky", "5 let"); fractional "many" falls back to "other". */
export function unitWord(t: Translate, locale: string, count: number, unit: Recurrence['intervalUnit']): string {
  const category = new Intl.PluralRules(locale).select(count);
  return t(`items.unitWord.${unit}.${category === 'one' || category === 'few' ? category : 'other'}` as MessageKey);
}
