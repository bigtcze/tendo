import { listMembers, type Member } from '../members/membersApi';

const MAX_MEMBER_PAGES = 20;

export type MemberCollection = { kind: 'ok'; members: Member[] } | { kind: 'stale' } | { kind: 'unauthenticated' } | { kind: 'unavailable' };

/**
 * Walks the member list up to 20 pages. `isCurrent` is checked after every page so an obsolete walk
 * stops at once; a failed page or a list longer than the cap is `unavailable`.
 */
export async function collectMembers(householdId: string, isCurrent: () => boolean): Promise<MemberCollection> {
  const members: Member[] = [];
  let cursor: string | undefined;
  for (let page = 0; page < MAX_MEMBER_PAGES; page++) {
    const listed = await listMembers(householdId, cursor);
    if (!isCurrent()) return { kind: 'stale' };
    if (listed.kind === 'unauthenticated') return { kind: 'unauthenticated' };
    if (listed.kind !== 'ok') return { kind: 'unavailable' };
    members.push(...listed.items);
    if (!listed.nextCursor) return { kind: 'ok', members };
    cursor = listed.nextCursor;
  }
  return { kind: 'unavailable' };
}
