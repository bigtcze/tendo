import type { Translate } from '../../i18n';
import type { Item, Recurrence } from './itemsApi';
import { detailStatusKey, pluralKey } from './itemEditModel';

export function repeatLabel(recurrence: Recurrence, locale: string, t: Translate) {
  return {
    interval: t(pluralKey(recurrence.intervalUnit, recurrence.intervalValue, locale), { count: String(recurrence.intervalValue) }),
    mode: t(recurrence.mode === 'after_completion' ? 'itemDetail.repeat.fluid' : 'itemDetail.repeat.fixed'),
  };
}

/** The read-only item card: state, dates, repeat policy and notes as the server reports them. */
export function ItemDetailReadView({ item, t, locale, subjectName }: { item: Item; t: Translate; locale: string; subjectName: string }) {
  const repeat = item.recurrence ? repeatLabel(item.recurrence, locale, t) : null;
  const attentionKey = item.attention === 'upcoming' ? 'itemDetail.nextAttention' : 'itemDetail.attentionDate';
  const prettyDate = (value: string) => new Intl.DateTimeFormat(locale === 'cs' ? 'cs-CZ' : 'en-GB', { day: 'numeric', month: 'long', timeZone: 'UTC' }).format(new Date(`${value}T00:00:00Z`));
  return <div className="item-facts mt-6 space-y-5 border-y border-line bg-white/50 p-5 sm:p-7">
    <p className="font-medium">{t(detailStatusKey(item))}</p>
    <p className="text-muted">{t('itemDetail.forSubject', { subject: subjectName })}</p>
    {item.attentionOn ? <p className="text-muted">{t(attentionKey, { date: prettyDate(item.attentionOn) })}</p> : null}
    {item.lastCompletedOn ? <p className="text-muted">{t('itemDetail.lastDone', { date: prettyDate(item.lastCompletedOn) })}</p> : null}
    {repeat ? <p className="text-muted">{t('itemDetail.repeatSummary', { interval: repeat.interval, mode: repeat.mode.replace(/\.$/, '') })}</p> : null}
    {item.notes ? <div className="border-t border-line pt-4">
      <h2 className="text-sm font-medium text-muted">{t('items.notes')}</h2>
      <p className="mt-2 whitespace-pre-wrap break-words">{item.notes}</p>
    </div> : null}
  </div>;
}
