import type { Translate } from '../../i18n';
import type { Item, Recurrence } from './itemsApi';
import { formatDate } from './formatDate';
import { detailStatusKey } from './itemEditModel';
import { intervalWords } from './repeatSummary';

export function repeatLabel(recurrence: Recurrence, locale: string, t: Translate) {
  return {
    interval: intervalWords(t, recurrence, locale),
    sentence: t('items.repeatEvery', { interval: intervalWords(t, recurrence, locale) }),
    mode: t(recurrence.mode === 'after_completion' ? 'itemDetail.repeat.fluid' : 'itemDetail.repeat.fixed'),
  };
}

function Dates({ item, t, locale }: { item: Item; t: Translate; locale: string }) {
  return <div className="space-y-1 text-muted">
    {item.attentionOn && !item.done
      ? <p>{t(item.attention === 'upcoming' ? 'itemDetail.nextAttention' : 'itemDetail.attentionDate', {
        date: formatDate(item.attentionOn, locale),
      })}</p>
      : null}
    {item.lastCompletedOn
      ? <p>{t('itemDetail.lastDone', { date: formatDate(item.lastCompletedOn, locale) })}</p>
      : null}
  </div>;
}

function Repeat({ recurrence, t, locale }: { recurrence: Recurrence; t: Translate; locale: string }) {
  const summary = repeatLabel(recurrence, locale, t);
  return <div>
    <p className="font-medium">{summary.sentence}</p>
    <p className="mt-1 text-sm text-muted">{summary.mode}</p>
  </div>;
}

/** Read-only item facts in everyday language; completion controls stay with actions. */
export function ItemDetailReadView({ item, t, locale, responsible }: { item: Item; t: Translate; locale: string; responsible: string | null }) {
  return <div className="mt-6 space-y-4 rounded-2xl border border-line bg-raised p-5 sm:p-7">
    <p className="font-medium">{t(detailStatusKey(item))}</p>
    {item.done ? <p className="text-sm text-muted">{t('itemDetail.done')}</p> : null}
    <Dates item={item} t={t} locale={locale} />
    {item.recurrence ? <Repeat recurrence={item.recurrence} t={t} locale={locale} /> : null}
    {responsible ? <p className="text-sm text-muted">{responsible}</p> : null}
    {item.notes ? <div className="border-t border-line pt-4">
      <h2 className="text-sm font-semibold text-muted">{t('items.notes')}</h2>
      <p className="mt-2 whitespace-pre-wrap break-words">{item.notes}</p>
    </div> : null}
  </div>;
}
