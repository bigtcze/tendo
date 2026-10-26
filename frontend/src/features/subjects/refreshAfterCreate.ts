import type { MutableRefObject, Dispatch, SetStateAction } from 'react';
import { listSubjects, type Subject } from './subjectsApi';
import { insertById, type ListState } from './subjectsModel';

export async function refreshAfterCreate(
  householdId: string,
  subject: Subject,
  generation: MutableRefObject<number>,
  form: number,
  finishAdd: (form: number, subject: Subject) => import('./SubjectForm').SubmitOutcome,
  setLoadingMore: Dispatch<SetStateAction<boolean>>,
  pagingFocus: MutableRefObject<HTMLElement | null>,
  pageLoad: MutableRefObject<number>,
  setCursorStack: Dispatch<SetStateAction<string[]>>,
  setCurrentCursor: Dispatch<SetStateAction<string | undefined>>,
  setList: Dispatch<SetStateAction<ListState>>,
  onSignedOut: () => void,
): Promise<{ refreshed: boolean; outcome?: import('./SubjectForm').SubmitOutcome }> {
  const refreshToken = ++generation.current;
  setLoadingMore(false);
  pagingFocus.current = null;
  const refreshed = await listSubjects(householdId, { archived: false });
  if (refreshToken !== generation.current) return { refreshed: true, outcome: finishAdd(form, subject) };
  if (refreshed.kind === 'unauthenticated') {
    onSignedOut();
    return { refreshed: true, outcome: { kind: 'handled' } };
  }
  if (refreshed.kind === 'ok') {
    pageLoad.current += 1;
    setCursorStack([]);
    setCurrentCursor(undefined);
    const refreshedItems = refreshed.nextCursor || subject.archived ? refreshed.items : insertById(refreshed.items, subject);
    setList({ kind: 'ready', items: refreshedItems, nextCursor: refreshed.nextCursor });
  }
  return { refreshed: true };
}
