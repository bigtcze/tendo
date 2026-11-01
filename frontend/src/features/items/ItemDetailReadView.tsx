import type { Translate } from '../../i18n';
import type { Item, Recurrence } from './itemsApi';
import { formatDate } from './formatDate';
import { detailStatusKey, pluralKey } from './itemEditModel';

export function repeatLabel(recurrence: Recurrence, locale: string, t: Translate) {
  return {
    interval: t(pluralKey(recurrence.intervalUnit, recurrence.intervalValue, locale), { count: String(recurrence.intervalValue) }),
    mode: t(recurrence.mode === 'after_completion' ? 'itemDetail.repeat.fluid' : 'itemDetail.repeat.fixed'),
  };
}

/** The read-only item card: state, dates, repeat policy and notes as the server reports them. */
export function ItemDetailReadView({ item, t, locale }: { item: Item; t: Translate; locale: string }) {
  const repeat = item.recurrence ? repeatLabel(item.recurrence, locale, t) : null;
  const attentionKey = item.attention === 'upcoming' ? 'itemDetail.nextAttention' : 'itemDetail.attentionDate';
  return <div className="detail-facts mt-6 space-y-5 border-y border-line py-5 sm:py-7">
    <p className="font-medium">{t(detailStatusKey(item))}</p>
    {item.attentionOn ? <p className="text-muted">{t(attentionKey, { date: formatDate(item.attentionOn, locale) })}</p> : null}
    {item.lastCompletedOn ? <p className="text-muted">{t('itemDetail.lastDone', { date: formatDate(item.lastCompletedOn, locale) })}</p> : null}
    {repeat ? <div><p>{repeat.interval}</p><p className="mt-1 text-sm text-muted">{repeat.mode}</p></div> : null}
    {item.notes ? <div className="border-t border-line pt-4">
      <h2 className="text-sm font-medium text-muted">{t('items.notes')}</h2>
      <p className="mt-2 whitespace-pre-wrap break-words">{item.notes}</p>
    </div> : null}
  </div>;
}
