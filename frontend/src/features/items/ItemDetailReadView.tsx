import type { Translate } from '../../i18n';
import type { Item, Recurrence } from './itemsApi';
import { formatDate } from './formatDate';
import { intervalWords } from './repeatSummary';

export function repeatLabel(recurrence: Recurrence, locale: string, t: Translate) {
  return {
    interval: intervalWords(t, recurrence, locale),
    sentence: t('items.repeatEvery', { interval: intervalWords(t, recurrence, locale) }),
    mode: t(recurrence.mode === 'after_completion' ? 'itemDetail.repeat.fluid' : 'itemDetail.repeat.fixed'),
  };
}

function attentionText({ item, t, locale }: { item: Item; t: Translate; locale: string }) {
  if (!item.attentionOn || item.done) return null;
  return t(item.attention === 'upcoming' ? 'itemDetail.nextAttention' : 'itemDetail.attentionDate', {
    date: formatDate(item.attentionOn, locale),
  });
}

function Dates({ item, t, locale }: { item: Item; t: Translate; locale: string }) {
  const attention = attentionText({ item, t, locale });
  const lastDone = item.lastCompletedOn ? t('itemDetail.lastDone', { date: formatDate(item.lastCompletedOn, locale) }) : null;
  if (!attention && !lastDone) return null;
  return <div className="space-y-1 text-muted">
    {attention ? <p>{attention}</p> : null}
    {lastDone ? <p>{lastDone}</p> : null}
  </div>;
}

function Repeat({ recurrence, t, locale }: { recurrence: Recurrence; t: Translate; locale: string }) {
  const summary = repeatLabel(recurrence, locale, t);
  return <div>
    <p className="font-medium text-ink">{summary.sentence}</p>
    <p className="mt-1 text-small text-muted">{summary.mode}</p>
  </div>;
}

/**
 * Read-only item facts in everyday language on one hairline-divided surface; the state itself lives in the
 * header pill and the completion controls stay with the action band. An item with none of these facts gets
 * no surface at all rather than an empty bordered sliver.
 */
export function ItemDetailReadView({ item, t, locale, responsible }: { item: Item; t: Translate; locale: string; responsible: string | null }) {
  const hasTimeline = Boolean(item.done || item.lastCompletedOn || (item.attentionOn && !item.done));
  if (!hasTimeline && !item.recurrence && !responsible && !item.notes) return null;
  return <div data-item-facts className="mt-6 divide-y divide-line overflow-hidden rounded-2xl border border-line bg-raised">
    {hasTimeline ? <div className="space-y-2 p-4 sm:px-5">
      {item.done ? <p className="text-muted">{t('itemDetail.done')}</p> : null}
      <Dates item={item} t={t} locale={locale} />
    </div> : null}
    {item.recurrence ? <div className="p-4 sm:px-5"><Repeat recurrence={item.recurrence} t={t} locale={locale} /></div> : null}
    {responsible ? <p className="p-4 text-muted sm:px-5">{responsible}</p> : null}
    {item.notes ? <div className="p-4 sm:px-5">
      <h2 className="text-small font-semibold text-muted">{t('items.notes')}</h2>
      <p className="mt-2 whitespace-pre-wrap break-words text-ink">{item.notes}</p>
    </div> : null}
  </div>;
}
