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
  return <div className="space-y-1 text-small text-muted">
    {attention ? <p>{attention}</p> : null}
    {lastDone ? <p>{lastDone}</p> : null}
  </div>;
}

function Repeat({ recurrence, t, locale }: { recurrence: Recurrence; t: Translate; locale: string }) {
  const summary = repeatLabel(recurrence, locale, t);
  return <div>
    <p className="font-semibold text-ink">{summary.sentence}</p>
    <p className="mt-0.5 text-caption text-muted">{summary.mode}</p>
  </div>;
}

/**
 * Read-only item facts as one sheet of hairline-divided lines on a single raised surface: the same
 * alignment and rhythm as a Home list, with no per-fact cards. The state itself lives in the header
 * label. An item with none of these facts gets no surface at all rather than an empty bordered sliver.
 */
export function ItemDetailReadView({ item, t, locale, responsible }: { item: Item; t: Translate; locale: string; responsible: string | null }) {
  const hasTimeline = Boolean(item.done || item.lastCompletedOn || (item.attentionOn && !item.done));
  if (!hasTimeline && !item.recurrence && !responsible && !item.notes) return null;
  return <div data-item-facts className="mt-5 divide-y divide-line overflow-hidden rounded-xl border border-line bg-raised">
    {hasTimeline ? <div className="space-y-1 px-4 py-3">
      {item.done ? <p className="text-small text-muted">{t('itemDetail.done')}</p> : null}
      <Dates item={item} t={t} locale={locale} />
    </div> : null}
    {item.recurrence ? <div className="px-4 py-3"><Repeat recurrence={item.recurrence} t={t} locale={locale} /></div> : null}
    {responsible ? <p className="px-4 py-3 text-small text-muted">{responsible}</p> : null}
    {item.notes ? <div className="px-4 py-3">
      <h2 className="text-caption font-semibold uppercase tracking-[0.04em] text-muted">{t('items.notes')}</h2>
      <p className="mt-1.5 whitespace-pre-wrap break-words text-ink">{item.notes}</p>
    </div> : null}
  </div>;
}
