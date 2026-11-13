import type { RefObject } from 'react';
import { useI18n } from '../../i18n';
import type { Member } from '../members/membersApi';
import type { SubjectType } from '../subjects/subjectsApi';
import type { ItemGroup, ItemSummary } from './itemsApi';
import { countPluralKey } from './itemsScreenHelpers';
import { groups, type GroupData } from './homeGroups';
import { HomeGroupSection } from './HomeGroupSection';
import { HomeItemRow } from './HomeItemRow';

type HomeGroupListProps = {
  summary: ItemSummary;
  groupData: Record<ItemGroup, GroupData>;
  sectionHeadingRefs: RefObject<Map<ItemGroup, HTMLHeadingElement>>;
  showMoreButtons: RefObject<Map<ItemGroup, HTMLButtonElement>>;
  subjectName: (subjectId: string) => string;
  subjectType: (subjectId: string) => SubjectType | undefined;
  userId: string;
  members: Member[];
  membersLoaded: boolean;
  busy: boolean;
  onShowMore: (group: ItemGroup) => void;
  onOpenAllItems: (group: ItemGroup) => void;
  onDone: (itemId: string) => void;
  onOpenItem: (itemId: string) => void;
};

/** Attention first, then the quieter groups; one reading column, never a grid of cards. */
export function HomeGroupList(props: HomeGroupListProps) {
  const { summary, groupData, sectionHeadingRefs, showMoreButtons, subjectName, subjectType, userId, members, membersLoaded, busy } = props;
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
      {state.items.map((item) => <HomeItemRow
        key={item.id}
        item={item}
        group={group.id}
        evaluatedOn={summary.evaluatedOn}
        subjectName={subjectName(item.subjectId)}
        subjectType={subjectType(item.subjectId)}
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
  return <div data-home-layout="list" className="min-w-0 space-y-8">
    {groups.map(renderGroup)}
  </div>;
}
