import { Button } from '../../components/ui/button';
import { NavLink } from '../../app/NavLink';
import { useI18n } from '../../i18n';
import { HOME_GROUP_ROW_CAP, groups, type GroupData } from './homeGroups';

export function HomeGroupSection({ group, summary, state, countLabel, children, headingRef, moreRef, onShowMore, onOpenAll }: {
  group: (typeof groups)[number];
  summary: number;
  state: GroupData;
  countLabel: string;
  children: React.ReactNode;
  headingRef: (node: HTMLHeadingElement | null) => void;
  moreRef: (node: HTMLButtonElement | null) => void;
  onShowMore: () => void;
  onOpenAll: () => void;
}) {
  const { t } = useI18n();
  if (!state.items.length) return null;
  const showAll = state.items.length >= HOME_GROUP_ROW_CAP && (state.cursor || state.overflow || summary > state.items.length);
  return (
    <section id={`group-${group.id}`} className="min-w-0">
      <div className="flex items-baseline justify-between gap-x-4 pb-1">
        <h2
          ref={headingRef}
          data-group-heading={group.id}
          tabIndex={-1}
          className="flex min-w-0 flex-wrap items-baseline gap-x-2 text-meta font-semibold uppercase tracking-[0.08em] text-muted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
        >
          <span>{t(group.key)}</span>{' '}
          <span className="text-meta font-normal normal-case tracking-normal text-muted">{countLabel}</span>
        </h2>
        {showAll ? (
          <NavLink
            href={`/items?status=${group.id}`}
            data-group-all={group.id}
            onNavigate={onOpenAll}
            className="inline-flex min-h-11 shrink-0 items-center text-meta font-medium text-muted underline decoration-line underline-offset-4 hover:text-ink hover:decoration-field focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
          >
            {summary > state.items.length ? t('items.group.seeAll', { count: String(summary) }) : t('items.group.seeAllNoCount')}
          </NavLink>
        ) : null}
      </div>
      <ul className="border-t border-line">{children}</ul>
      {state.cursor && state.items.length < HOME_GROUP_ROW_CAP ? (
        <Button ref={moreRef} type="button" data-group-more={group.id} aria-disabled={state.loadingMore} variant="quiet" size="small" className="mt-1" onClick={onShowMore}>
          {state.loadingMore
            ? t('items.moreLoading')
            : t('items.showMore', { count: String(Math.max(0, summary - state.items.length)) })}
        </Button>
      ) : null}
    </section>
  );
}
