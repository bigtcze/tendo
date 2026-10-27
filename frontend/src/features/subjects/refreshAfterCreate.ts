import type { Dispatch, MutableRefObject, SetStateAction } from 'react';
import { listSubjects, type Subject } from './subjectsApi';
import type { SubmitOutcome } from './SubjectForm';
import { insertById, type ListState } from './subjectsModel';

type RefreshAfterCreateContext = {
  householdId: string;
  subject: Subject;
  generation: MutableRefObject<number>;
  form: number;
  finishAdd: (form: number, subject: Subject) => SubmitOutcome;
  setLoadingMore: Dispatch<SetStateAction<boolean>>;
  pagingFocus: MutableRefObject<HTMLElement | null>;
  pageLoad: MutableRefObject<number>;
  setCursorStack: Dispatch<SetStateAction<string[]>>;
  setCurrentCursor: Dispatch<SetStateAction<string | undefined>>;
  setList: Dispatch<SetStateAction<ListState>>;
  onSignedOut: () => void;
};

export async function refreshAfterCreate(ctx: RefreshAfterCreateContext): Promise<SubmitOutcome> {
  const refreshToken = ++ctx.generation.current;
  ctx.setLoadingMore(false);
  ctx.pagingFocus.current = null;
  const refreshed = await listSubjects(ctx.householdId, { archived: false });
  if (refreshToken !== ctx.generation.current) return ctx.finishAdd(ctx.form, ctx.subject);
  if (refreshed.kind === 'unauthenticated') {
    ctx.onSignedOut();
    return { kind: 'handled' };
  }
  if (refreshed.kind === 'ok') {
    ctx.pageLoad.current += 1;
    ctx.setCursorStack([]);
    ctx.setCurrentCursor(undefined);
    const refreshedItems = refreshed.nextCursor || ctx.subject.archived
      ? refreshed.items
      : insertById(refreshed.items, ctx.subject);
    ctx.setList({ kind: 'ready', items: refreshedItems, nextCursor: refreshed.nextCursor });
  }
  return ctx.finishAdd(ctx.form, ctx.subject);
}
