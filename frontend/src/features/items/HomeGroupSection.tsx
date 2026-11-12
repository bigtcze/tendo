import { Button } from '../../components/ui/button';
import { NavLink } from '../../app/NavLink';
import { useI18n } from '../../i18n';
import { groupTone, groupContextKey } from './homeGroupTone';
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
  const contextKey = groupContextKey[group.id];
  return (
    <section id={`group-${group.id}`} className="min-w-0">
      <div className="mb-2 flex flex-wrap items-center gap-x-3 gap-y-1">
        <h2
          ref={headingRef}
          data-group-heading={group.id}
          tabIndex={-1}
          className="flex min-w-0 flex-wrap items-center gap-2 text-small font-semibold uppercase tracking-[0.04em] text-ink focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
        >
          <span aria-hidden="true" className={`size-2 shrink-0 rounded-full ${groupTone[group.id].dot}`} />
          <span>{t(group.key)}</span>{' '}
          <span className="text-meta font-medium normal-case tracking-normal text-muted">{countLabel}</span>
        </h2>
        <div className="ml-auto flex items-center gap-3">
          {contextKey ? <span className="hidden text-meta text-muted lg:inline">{t(contextKey)}</span> : null}
          {showAll ? (
            <NavLink
              href={`/items?status=${group.id}`}
              data-group-all={group.id}
              onNavigate={onOpenAll}
              className="inline-flex min-h-11 items-center text-meta font-medium text-muted underline decoration-line underline-offset-4 hover:text-accent hover:decoration-accent focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
            >
              {summary > state.items.length ? t('items.group.seeAll', { count: String(summary) }) : t('items.group.seeAllNoCount')}
            </NavLink>
          ) : null}
        </div>
      </div>
      <ul className="divide-y divide-line overflow-hidden rounded-2xl border border-line bg-raised">{children}</ul>
      {state.cursor && state.items.length < HOME_GROUP_ROW_CAP ? (
        <Button ref={moreRef} type="button" data-group-more={group.id} aria-disabled={state.loadingMore} variant="quiet" size="small" className="mt-2" onClick={onShowMore}>
          {state.loadingMore
            ? t('items.moreLoading')
            : t('items.showMore', { count: String(Math.max(0, summary - state.items.length)) })}
        </Button>
      ) : null}
    </section>
  );
}
