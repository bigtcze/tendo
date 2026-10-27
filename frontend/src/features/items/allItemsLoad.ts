import { getFilteredItemsSummary, listAllItems, type Item, type ItemSummary } from './itemsApi';
import { activeFilters, type Context } from './allItemsUrl';

export type LoadOutcome =
  | { kind: 'ok'; summary: ItemSummary; items: Item[]; nextCursor: string | null }
  | { kind: 'superseded' | 'unauthenticated' | 'invalidQuery' | 'stale' | 'error' };

/**
 * Loads the filtered summary, then the page rows for its evaluation date.
 * `current` is checked after each response so a superseded load never issues or applies a later request.
 */
export async function fetchAllItemsPage(householdId: string, context: Context, current: () => boolean): Promise<LoadOutcome> {
  const filters = activeFilters(context);
  const summaryResult = await getFilteredItemsSummary(householdId, filters);
  if (!current()) return { kind: 'superseded' };
  if (summaryResult.kind === 'unauthenticated') return { kind: 'unauthenticated' };
  if (summaryResult.kind !== 'ok') return { kind: 'error' };
  const pageResult = await listAllItems(householdId, context.status, filters, summaryResult.summary.evaluatedOn, context.cursor);
  if (!current()) return { kind: 'superseded' };
  switch (pageResult.kind) {
    case 'ok': return { kind: 'ok', summary: summaryResult.summary, items: pageResult.items, nextCursor: pageResult.nextCursor };
    case 'unauthenticated':
    case 'invalidQuery':
    case 'stale': return { kind: pageResult.kind };
    default: return { kind: 'error' };
  }
}
