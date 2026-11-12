import type { ReactNode } from 'react';
import type { Translate } from '../../i18n';
import type { Item, Recurrence } from './itemsApi';
import { formatDate } from './formatDate';
import { intervalWords, unitWord } from './repeatSummary';

export function repeatLabel(recurrence: Recurrence, locale: string, t: Translate) {
  return {
    interval: intervalWords(t, recurrence, locale),
    // A one-off interval reads as the cadence it is: "every year", not "every 1 year".
    sentence: recurrence.intervalValue === 1
      ? t('items.repeatEveryOne', { unit: unitWord(t, locale, 1, recurrence.intervalUnit) })
      : t('items.repeatEvery', { interval: intervalWords(t, recurrence, locale) }),
    mode: t(recurrence.mode === 'after_completion' ? 'itemDetail.repeat.fluid' : 'itemDetail.repeat.fixed'),
  };
}

function Repeat({ recurrence, t, locale }: { recurrence: Recurrence; t: Translate; locale: string }) {
  const summary = repeatLabel(recurrence, locale, t);
  return <>
    <span className="block text-ink">{summary.sentence}</span>
    <span className="mt-0.5 block text-meta text-muted">{summary.mode}</span>
  </>;
}

export type FactRow = { key: string; label: string; value: ReactNode };

/** Facts as labelled values, in reading order; the caller supplies the labels in the active language. */
export function FactList({ rows }: { rows: FactRow[] }) {
  if (!rows.length) return null;
  return <dl data-item-facts className="mt-7 border-t border-line">
    {rows.map((row) => (
      <div key={row.key} className="grid gap-x-6 gap-y-0.5 border-b border-line py-3 last:border-b-0 sm:grid-cols-[9.5rem_minmax(0,1fr)]">
        <dt className="text-meta leading-6 text-muted">{row.label}</dt>
        <dd className="min-w-0 text-body text-ink [overflow-wrap:anywhere]">{row.value}</dd>
      </div>
    ))}
  </dl>;
}

/**
 * Read-only item facts as a labelled definition list: due, last completion, repeat, responsibility
 * and notes in the order a person reads them. The state itself lives beside the title.
 */
export function ItemDetailReadView({ item, t, locale, responsibleName }: {
  item: Item; t: Translate; locale: string; responsibleName: string | null;
}) {
  const rows: FactRow[] = [];
  if (!item.done && item.attentionOn) {
    rows.push({ key: 'due', label: t('itemDetail.facts.when'), value: formatDate(item.attentionOn, locale) });
  }
  if (item.lastCompletedOn) {
    rows.push({ key: 'last', label: t('itemDetail.facts.lastCompleted'), value: formatDate(item.lastCompletedOn, locale) });
  }
  if (item.recurrence) {
    rows.push({ key: 'repeat', label: t('itemDetail.facts.repeat'), value: <Repeat recurrence={item.recurrence} t={t} locale={locale} /> });
  }
  if (responsibleName) rows.push({ key: 'responsible', label: t('itemDetail.facts.responsible'), value: responsibleName });
  if (item.notes) {
    rows.push({ key: 'notes', label: t('itemDetail.facts.notes'), value: <span className="whitespace-pre-wrap">{item.notes}</span> });
  }
  return <FactList rows={rows} />;
}
