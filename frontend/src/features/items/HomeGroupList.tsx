import type { RefObject } from 'react';
import { useI18n } from '../../i18n';
import type { Member } from '../members/membersApi';
import type { ItemGroup, ItemSummary } from './itemsApi';
import { countPluralKey } from './itemsScreenHelpers';
import { groups, type GroupData } from './homeGroups';
import { HomeGroupSection, ItemRow } from './HomeGroupSection';

type HomeGroupListProps = {
  summary: ItemSummary;
  groupData: Record<ItemGroup, GroupData>;
  sectionHeadingRefs: RefObject<Map<ItemGroup, HTMLHeadingElement>>;
  showMoreButtons: RefObject<Map<ItemGroup, HTMLButtonElement>>;
  subjectName: (subjectId: string) => string;
  userId: string;
  members: Member[];
  membersLoaded: boolean;
  busy: boolean;
  onShowMore: (group: ItemGroup) => void;
  onOpenAllItems: (group: ItemGroup) => void;
  onDone: (itemId: string) => void;
  onOpenItem: (itemId: string) => void;
};

const TWO_COLUMN_CLASS = 'grid grid-cols-1 items-start gap-8 lg:grid-cols-[minmax(0,1.35fr)_minmax(20rem,1fr)]';
const STACKED_CLASS = 'grid grid-cols-1 items-start gap-8';

/** Puts attention first and lets the quieter groups form an independent desktop column. */
export function HomeGroupList(props: HomeGroupListProps) {
  const { summary, groupData, sectionHeadingRefs, showMoreButtons, subjectName, userId, members, membersLoaded, busy } = props;
  const { t, locale } = useI18n();
  const renderGroup = (group: (typeof groups)[number]) => {
    const state = groupData[group.id];
    return <HomeGroupSection
      key={group.id}
      group={group}
      summary={summary[group.count]}
      state={state}
      countLabel={t(countPluralKey(summary[group.count], locale), { count: String(summary[group.count]) })}
      headingRef={(node) => {
        if (node) sectionHeadingRefs.current.set(group.id, node); else sectionHeadingRefs.current.delete(group.id);
      }}
      moreRef={(node) => { if (node) showMoreButtons.current.set(group.id, node); else showMoreButtons.current.delete(group.id); }}
      onShowMore={() => { if (!state.loadingMore) props.onShowMore(group.id); }}
      onOpenAll={() => props.onOpenAllItems(group.id)}
    >
      {state.items.map((item) => <ItemRow
        key={item.id}
        item={item}
        group={group.id}
        evaluatedOn={summary.evaluatedOn}
        subjectName={subjectName(item.subjectId)}
        locale={locale}
        userId={userId}
        members={members}
        membersLoaded={membersLoaded}
        busy={busy}
        onDone={() => props.onDone(item.id)}
        onOpenItem={props.onOpenItem}
      />)}
    </HomeGroupSection>;
  };
  const primary = groups[0]!;
  const secondary = groups.slice(1);
  const hasPrimary = groupData[primary.id].items.length > 0;
  const secondaryCount = secondary.reduce((total, group) => total + groupData[group.id].items.length, 0);
  const needsCount = groupData[primary.id].items.length;
  const secondaryDominates = hasPrimary && secondaryCount >= Math.max(20, needsCount * 3);
  const layoutClass = hasPrimary && !secondaryDominates ? TWO_COLUMN_CLASS : STACKED_CLASS;
  const layoutName = !hasPrimary ? 'secondary-only' : secondaryDominates ? 'balanced-stacked' : 'attention-and-secondary';
  return <div data-home-layout={layoutName} className={layoutClass}>
    {hasPrimary ? renderGroup(primary) : null}
    {secondary.map(renderGroup)}
  </div>;
}
