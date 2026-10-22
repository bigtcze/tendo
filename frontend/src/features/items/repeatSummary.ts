import type { MessageKey } from '../../i18n';
import type { Recurrence } from './itemsApi';

type Translate = (key: MessageKey, values?: Record<string, string>) => string;

function intervalKey(unit: Recurrence['intervalUnit'], count: number, locale: string): MessageKey {
  const category = new Intl.PluralRules(locale).select(count);
  const supported: Record<string, readonly string[]> = { en: ['one', 'other'], cs: ['one', 'few', 'other'] };
  return `items.every.${unit}.${supported[locale]?.includes(category) ? category : 'other'}` as MessageKey;
}

/** One-line row summary: the interval plus whether the next date keeps the plan (Repeat) or follows completion (Fluid). */
export function repeatSummary(t: Translate, recurrence: Recurrence, locale: string): string {
  const interval = t(intervalKey(recurrence.intervalUnit, recurrence.intervalValue, locale), { count: String(recurrence.intervalValue) });
  return `${interval} · ${t(recurrence.mode === 'after_completion' ? 'items.repeatMode.fluid' : 'items.repeatMode.fixed')}`;
}
