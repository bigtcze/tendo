import type { Dispatch, MutableRefObject, SetStateAction } from 'react';
import { getPendingSubjectCreate, type PendingSubjectCreate } from './pendingSubjectCreate';
import type { Editing, Notice } from './subjectsModel';

export function useAddSubjectForm(ctx: {
  userId: string; householdId: string; addForm: MutableRefObject<number>;
  loadingMore: boolean; mutationPending: boolean; busy: MutableRefObject<boolean>;
  setPendingCreate: Dispatch<SetStateAction<PendingSubjectCreate | undefined>>;
  setCreateUncertain: Dispatch<SetStateAction<boolean>>; setNotice: Dispatch<SetStateAction<Notice | null | undefined>>;
  setAdding: Dispatch<SetStateAction<boolean>>; setMembers: () => void;
  setEditing: Dispatch<SetStateAction<Editing | null>>; setActionFailed: Dispatch<SetStateAction<boolean>>;
}) {
  function startAdding() {
    if (ctx.loadingMore || ctx.mutationPending || ctx.busy.current) return;
    ctx.setMembers();
    const pending = getPendingSubjectCreate(ctx.userId, ctx.householdId);
    ctx.setPendingCreate(pending);
    if (pending) {
      ctx.setCreateUncertain(true);
      ctx.setNotice(null);
    } else {
      ctx.setCreateUncertain(false);
      ctx.setNotice(null);
    }
    ctx.setEditing(null);
    ctx.setActionFailed(false);
    ctx.addForm.current += 1;
    ctx.setAdding(true);
  }
  function cancelAdding() {
    ctx.setNotice(null);
    ctx.setAdding(false);
  }
  return { startAdding, cancelAdding };
}
