import { useEffect, useState } from 'react';
import { listMembers, type Member } from '../members/membersApi';
import { listActiveSubjects, type Subject } from './itemsApi';

export type Facet<T> = { state: 'loading' | 'ready' | 'failed'; values: T[] };
export type AllItemsFacets = { subjects: Facet<Subject>; members: Facet<Member> };
type Page<T> = { kind: 'ok'; values: T[]; nextCursor: string | null } | { kind: string };

async function loadAllPages<T>(fetchPage: (cursor?: string) => Promise<Page<T>>, limitMessage: string): Promise<T[]> {
  const values: T[] = [];
  let cursor: string | undefined;
  for (let page = 0; page < 20; page++) {
    const result = await fetchPage(cursor);
    if (!('values' in result)) throw result;
    values.push(...result.values);
    if (!result.nextCursor) return values;
    cursor = result.nextCursor;
  }
  throw new Error(limitMessage);
}

function loadSubjects(householdId: string, archived: boolean): Promise<Subject[]> {
  return loadAllPages(async (cursor) => {
    const result = await listActiveSubjects(householdId, archived, cursor);
    return result.kind === 'ok' ? { kind: 'ok', values: result.subjects, nextCursor: result.nextCursor } : result;
  }, 'Subject page limit reached');
}

async function loadActiveAndArchivedSubjects(householdId: string): Promise<Subject[]> {
  const active = await loadSubjects(householdId, false);
  const archived = await loadSubjects(householdId, true);
  return [...active, ...archived.filter((subject) => !active.some((entry) => entry.id === subject.id))];
}

function loadMembers(householdId: string): Promise<Member[]> {
  return loadAllPages(async (cursor) => {
    const result = await listMembers(householdId, cursor);
    return result.kind === 'ok' ? { kind: 'ok', values: result.items, nextCursor: result.nextCursor } : result;
  }, 'Member page limit reached');
}

/** Loads the subject and member filter choices once per household; a failed facet hides only its own filter. */
export function useAllItemsFacets(householdId: string, onSignedOut: () => void, onNoHousehold: () => void): AllItemsFacets {
  const [subjects, setSubjects] = useState<Facet<Subject>>({ state: 'loading', values: [] });
  const [members, setMembers] = useState<Facet<Member>>({ state: 'loading', values: [] });
  useEffect(() => {
    let cancelled = false;
    function track<T>(load: Promise<T[]>, set: (facet: Facet<T>) => void) {
      void load.then((values) => { if (!cancelled) set({ state: 'ready', values }); }).catch((failure: { kind?: string }) => {
        if (cancelled) return;
        if (failure.kind === 'unauthenticated') onSignedOut();
        if (failure.kind === 'noHousehold') onNoHousehold();
        set({ state: 'failed', values: [] });
      });
    }
    track(loadActiveAndArchivedSubjects(householdId), setSubjects);
    track(loadMembers(householdId), setMembers);
    return () => { cancelled = true; };
  }, [householdId, onSignedOut, onNoHousehold]);
  return { subjects, members };
}
