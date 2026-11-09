import type { RefObject } from 'react';
import { useI18n } from '../../i18n';
import { GroupStatusIcon } from './HomeIcons';
import { groups, type GroupData } from './homeGroups';
import { groupTone } from './homeGroupTone';
import type { ItemGroup, ItemSummary } from './itemsApi';
import { countPluralKey } from './itemsScreenHelpers';

type PillProps = {
  summary: ItemSummary;
  groupData: Record<ItemGroup, GroupData>;
  sectionHeadingRefs: RefObject<Map<ItemGroup, HTMLHeadingElement>>;
};

const BASE = 'inline-flex min-h-11 shrink-0 items-center gap-2 rounded-full border px-3.5 text-sm font-semibold '
  + 'transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus';

/** In-page status filters: each link focuses the heading of a group that actually rendered rows. */
export function HomeStatusPills({ summary, groupData, sectionHeadingRefs }: PillProps) {
  const { t, locale } = useI18n();
  const visible = groups.filter((group) => summary[group.count] > 0 && groupData[group.id].items.length > 0);
  if (visible.length < 2) return null;
  return (
    <nav
      aria-label={t('items.group.overview')}
      className={[
        'relative -mx-1 flex min-w-0 gap-2 overflow-x-auto overscroll-x-contain whitespace-nowrap',
        // 4px padding on every side clears the 2px outline + 2px offset focus ring inside the scroll clip.
        'p-1 scroll-p-1 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden',
      ].join(' ')}
      onFocusCapture={(event) => {
        const link = event.target;
        const nav = event.currentTarget;
        if (!(link instanceof HTMLElement)) return;
        const navRect = nav.getBoundingClientRect();
        const linkRect = link.getBoundingClientRect();
        const style = getComputedStyle(nav);
        const left = Number.parseFloat(style.paddingLeft) || 0;
        const right = Number.parseFloat(style.paddingRight) || 0;
        if (linkRect.left < navRect.left + left) nav.scrollLeft -= navRect.left + left - linkRect.left;
        else if (linkRect.right > navRect.right - right) nav.scrollLeft += linkRect.right - navRect.right + right;
      }}
    >
      {visible.map((group) => {
        const count = summary[group.count];
        return (
          <a
            key={group.id}
            href={`#group-${group.id}`}
            aria-label={`${t(group.key)} ${t(countPluralKey(count, locale), { count: String(count) })}`}
            onClick={(event) => {
              if (event.defaultPrevented || event.button !== 0) return;
              if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
              event.preventDefault();
              sectionHeadingRefs.current.get(group.id)?.focus();
            }}
            className={`${BASE} ${groupTone[group.id].pill}`}
          >
            <GroupStatusIcon group={group.id} className="size-4" />
            <span>{t(group.key)}</span>{' '}
            <span>{count}</span>
          </a>
        );
      })}
    </nav>
  );
}
