import type { ReactNode } from 'react';
import type { Translate } from '../../i18n';
import type { Item } from './itemsApi';
import { formatDate } from './formatDate';
import { intervalWords } from './repeatSummary';

/** One fact: a visible term above (or beside, from small screens up) its bare value, divided by hairlines. */
function Fact({ name, term, children }: { name: string; term: string; children: ReactNode }) {
  return <div data-item-fact={name} className="grid gap-x-4 gap-y-1 py-3 sm:grid-cols-[9rem_minmax(0,1fr)]">
    <dt className="text-meta font-medium text-muted sm:pt-0.5">{term}</dt>
    <dd className="min-w-0 break-words text-ink">{children}</dd>
  </div>;
}

/**
 * Read-only item facts in everyday language: one flat labelled list whose terms carry the labels, so the
 * values stay bare. The state itself lives in the header pill and the completion controls stay with the
 * action band. An item with none of these facts gets no list at all rather than an empty bordered sliver.
 */
export function ItemDetailReadView({ item, t, locale, responsible }: { item: Item; t: Translate; locale: string; responsible: string | null }) {
  const attention = item.attentionOn && !item.done ? formatDate(item.attentionOn, locale) : null;
  const lastDone = item.lastCompletedOn ? formatDate(item.lastCompletedOn, locale) : null;
  const recurrence = item.recurrence;
  if (!attention && !lastDone && !recurrence && !responsible && !item.notes) return null;
  return <dl data-item-facts className="mt-6 divide-y divide-line border-t border-line">
    {attention ? <Fact name="attention" term={t('itemDetail.fact.attention')}>{attention}</Fact> : null}
    {lastDone ? <Fact name="lastDone" term={t('itemDetail.fact.lastDone')}>{lastDone}</Fact> : null}
    {recurrence ? <Fact name="repeat" term={t('itemDetail.fact.repeat')}>
      <p>{intervalWords(t, recurrence, locale)}</p>
      <p className="mt-1 text-small text-muted">{t(recurrence.mode === 'after_completion' ? 'itemDetail.repeat.fluid' : 'itemDetail.repeat.fixed')}</p>
    </Fact> : null}
    {responsible ? <Fact name="responsible" term={t('itemDetail.fact.responsible')}>{responsible}</Fact> : null}
    {item.notes ? <Fact name="notes" term={t('items.notes')}>
      <p className="whitespace-pre-wrap break-words">{item.notes}</p>
    </Fact> : null}
  </dl>;
}
