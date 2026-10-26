import type { ItemGroup, ItemSummary, ItemGroupPage } from './itemsApi';
import { groups, HOME_GROUP_ROW_CAP, type GroupData } from './homeGroups';
import { listItemGroup } from './itemsApi';

export const emptyGroups = (): Record<ItemGroup, GroupData> => ({
  needs: { items: [], cursor: null, loadingMore: false, overflow: false },
  inProgress: { items: [], cursor: null, loadingMore: false, overflow: false },
  waiting: { items: [], cursor: null, loadingMore: false, overflow: false },
  upcoming: { items: [], cursor: null, loadingMore: false, overflow: false },
  paused: { items: [], cursor: null, loadingMore: false, overflow: false },
});

export function mergeGroupPage(old: GroupData, result: Extract<ItemGroupPage, { kind: 'ok' }>): GroupData {
  const seen = new Set(old.items.map((item) => item.id));
  const merged = [...old.items, ...result.items.filter((item) => !seen.has(item.id))];
  return {
    items: merged.slice(0, HOME_GROUP_ROW_CAP),
    cursor: result.nextCursor,
    loadingMore: false,
    overflow: old.overflow || merged.length > HOME_GROUP_ROW_CAP,
  };
}

export async function fetchHomeGroups(householdId: string, summary: ItemSummary) {
  return Promise.all(
    groups
      .filter((group) => summary[group.count] > 0)
      .map(async (group) => [
        group.id,
        await listItemGroup(householdId, group.id, summary.evaluatedOn),
      ] as const),
  );
}

export function isShowMoreBlocked(state: GroupData, inFlight: boolean, hasSummary: boolean, mounted: boolean): boolean {
  return !state.cursor || state.items.length >= HOME_GROUP_ROW_CAP || inFlight || !hasSummary || !mounted;
}
