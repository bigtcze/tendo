import type { Dispatch, MutableRefObject, SetStateAction } from 'react';
import { getSubject, type Subject } from './subjectsApi';
import type { Editing } from './subjectsModel';

export function useSubjectEditing(ctx: {
  householdId: string; showArchived: boolean; loadingMore: boolean; mutationPending: boolean; adding: boolean;
  editing: Editing | null; busy: MutableRefObject<boolean>; generation: MutableRefObject<number>;
  guarded: (id: string, work: () => Promise<void>) => Promise<void>; gone: () => void; changedMeanwhile: () => void;
  setNotice: Dispatch<SetStateAction<import('./subjectsModel').Notice | null | undefined>>;
  updateRow: (subject: Subject) => void; setAdding: Dispatch<SetStateAction<boolean>>;
  resetMembers: () => void; setEditing: Dispatch<SetStateAction<Editing | null>>;
  focusKey: MutableRefObject<string | null>; onSignedOut: () => void; setActionFailed: Dispatch<SetStateAction<boolean>>;
}) {
  function closeEditing(id: string) {
    ctx.focusKey.current = `row:${id}`;
    ctx.setEditing(null);
  }
  function startEditing(subject: Subject) {
    if (ctx.loadingMore || ctx.mutationPending || ctx.adding || ctx.editing) return;
    if (ctx.busy.current) return;
    const started = ctx.generation.current;
    void ctx.guarded(subject.id, async () => {
      ctx.setNotice(null);
      const result = await getSubject(ctx.householdId, subject.id);
      if (result.kind === 'unauthenticated') {
        ctx.onSignedOut();
        return;
      }
      if (started !== ctx.generation.current) return;
      if (result.kind === 'ok') {
        if (result.subject.archived !== ctx.showArchived) {
          ctx.changedMeanwhile();
          return;
        }
        ctx.updateRow(result.subject);
        ctx.setAdding(false);
        ctx.resetMembers();
        ctx.setEditing({ subject: result.subject, etag: result.etag, conflict: false });
      } else if (result.kind === 'notFound') ctx.gone();
      else ctx.setActionFailed(true);
    });
  }
  return { startEditing, closeEditing };
}
