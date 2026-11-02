import type { MessageKey } from '../../i18n';
import type { Recurrence } from './itemsApi';

type Translate = (key: MessageKey, values?: Record<string, string>) => string;

function intervalKey(unit: Recurrence['intervalUnit'], count: number, locale: string): MessageKey {
  const category = new Intl.PluralRules(locale).select(count);
  const categories: Record<string, string> = { en: category === 'one' ? 'one' : 'other', cs: category === 'one' || category === 'few' ? category : 'other' };
  return `items.every.${unit}.${categories[locale] ?? 'other'}` as MessageKey;
}

/** One-line row summary: the interval plus whether the next date keeps the plan (Repeat) or follows completion (Fluid). */
export function repeatSummary(t: Translate, recurrence: Recurrence, locale: string): string {
  const interval = t(intervalKey(recurrence.intervalUnit, recurrence.intervalValue, locale), { count: String(recurrence.intervalValue) });
  const unit = interval.replace(/^.*?\d+ /u, '');
  return `${recurrence.intervalValue} ${unit} · ${t(recurrence.mode === 'after_completion' ? 'items.repeatMode.fluid' : 'items.repeatMode.fixed')}`;
}
