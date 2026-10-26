import type { Dispatch, MutableRefObject, SetStateAction } from 'react';
import { setArchived, type Subject } from './subjectsApi';
import { insertById, type Editing, type Notice } from './subjectsModel';

export function useSubjectArchive(ctx: {
  householdId: string; showArchived: boolean; loadingMore: boolean; mutationPending: boolean; adding: boolean;
  editing: Editing | null; busy: MutableRefObject<boolean>; generation: MutableRefObject<number>;
  guarded: (id: string, work: () => Promise<void>) => Promise<void>; gone: () => void; changedMeanwhile: () => void;
  archivePage: MutableRefObject<number | null>; pageLoad: MutableRefObject<number>;
  setNotice: Dispatch<SetStateAction<Notice | null | undefined>>; setActionFailed: Dispatch<SetStateAction<boolean>>;
  setList: Dispatch<SetStateAction<import('./subjectsModel').ListState>>;
  reloadPage: (cursor: string | undefined, stack: string[], focusId: string) => Promise<void>;
  currentCursor: string | undefined; cursorStack: string[]; focusKey: MutableRefObject<string | null>;
  noticeRef: MutableRefObject<HTMLDivElement | null>; onSignedOut: () => void;
}) {
  function changeArchived(subject: Subject, archived: boolean, isUndo: boolean) {
    if (ctx.loadingMore || ctx.mutationPending || ctx.adding || ctx.editing) return;
    if (ctx.busy.current) return;
    const started = ctx.generation.current;
    if (!isUndo) ctx.archivePage.current = ctx.pageLoad.current;
    void ctx.guarded(subject.id, async () => {
      if (!isUndo) ctx.setNotice(null);
      const result = await setArchived(ctx.householdId, subject.id, archived);
      if (result.kind === 'unauthenticated') {
        ctx.onSignedOut();
        return;
      }
      if (started !== ctx.generation.current) return;
      if (result.kind === 'ok') {
        if (isUndo) {
          ctx.setNotice(null);
          const archivedOn = ctx.archivePage.current;
          ctx.archivePage.current = null;
          if (archivedOn === ctx.pageLoad.current) {
            ctx.focusKey.current = `row:${subject.id}`;
            ctx.setList((current) => current.kind === 'ready' ? { ...current, items: insertById(current.items, result.subject) } : current);
          } else {
            await ctx.reloadPage(ctx.currentCursor, ctx.cursorStack, subject.id);
          }
        } else {
          ctx.setList((current) => current.kind === 'ready'
            ? { ...current, items: current.items.filter((s) => s.id !== subject.id) } : current);
          ctx.setNotice(archived ? { kind: 'archived', subject } : { kind: 'restored', subject });
        }
      } else if (result.kind === 'notFound') ctx.gone();
      else if (result.kind === 'conflict') ctx.changedMeanwhile();
      else {
        ctx.setActionFailed(true);
        if (isUndo) ctx.noticeRef.current?.focus();
      }
    });
  }
  function undoArchive(done: Notice & { kind: 'archived' | 'restored' }) {
    changeArchived(done.subject, done.kind === 'restored', true);
  }
  return { changeArchived, undoArchive };
}
