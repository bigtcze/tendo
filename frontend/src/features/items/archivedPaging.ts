import type { operations } from '../../../../api/generated/api';

type ListItemsQuery = NonNullable<operations['listItems']['parameters']['query']>;
type StreamPage<T> =
  | { kind: 'ok'; items: T[]; nextCursor: string | null }
  | { kind: 'stale'; parameter?: string }
  | { kind: 'invalidQuery' }
  | { kind: 'unauthenticated' | 'error' | 'noHousehold' };

/** UI cursor wraps two server streams: 'a:<c>' continues unfinished archived items,
 * 'b:' starts completed archived items and 'b:<c>' continues them. Anything else is stale. */
type ParsedCursor = { archivedDone: boolean; serverCursor: string } | { kind: 'stale'; parameter: 'cursor' };

function parseArchivedCursor(cursor: string | undefined): ParsedCursor {
  const token = cursor ?? '';
  const archivedDone = token.startsWith('b:');
  if (token && !archivedDone && !(token.startsWith('a:') && token.length > 2)) {
    return { kind: 'stale', parameter: 'cursor' };
  }
  return { archivedDone, serverCursor: token.slice(2) };
}

/** Loads one All items page of the archived status across the unfinished and completed server streams. */
export async function archivedPaging<T>(
  common: Omit<ListItemsQuery, 'limit' | 'archived' | 'done' | 'cursor'>,
  cursor: string | undefined,
  fetchStream: (query: ListItemsQuery) => Promise<StreamPage<T>>,
): Promise<StreamPage<T>> {
  const parsed = parseArchivedCursor(cursor);
  if ('kind' in parsed) return parsed;
  const { archivedDone, serverCursor } = parsed;
  const first = await fetchStream({ limit: 25, ...common, archived: true, done: archivedDone, ...(serverCursor ? { cursor: serverCursor } : {}) });
  if (first.kind !== 'ok') return first;
  if (archivedDone) return { kind: 'ok', items: first.items, nextCursor: first.nextCursor ? `b:${first.nextCursor}` : null };
  if (first.nextCursor) return { kind: 'ok', items: first.items, nextCursor: `a:${first.nextCursor}` };
  if (first.items.length === 25) {
    // Only offer the boundary page when completed archived items actually exist.
    const probe = await fetchStream({ limit: 1, ...common, archived: true, done: true });
    if (probe.kind !== 'ok') return probe;
    return { kind: 'ok', items: first.items, nextCursor: probe.items.length ? 'b:' : null };
  }
  const second = await fetchStream({ limit: 25 - first.items.length, ...common, archived: true, done: true });
  if (second.kind !== 'ok') return second;
  return { kind: 'ok', items: [...first.items, ...second.items], nextCursor: second.nextCursor ? `b:${second.nextCursor}` : null };
}
