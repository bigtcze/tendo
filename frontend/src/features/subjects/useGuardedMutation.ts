import type { Dispatch, MutableRefObject, SetStateAction } from 'react';
import type { Editing, Notice } from './subjectsModel';

export function useGuardedMutation(ctx: {
  busy: MutableRefObject<boolean>; setBusyId: Dispatch<SetStateAction<string | null>>;
  setMutationPending: Dispatch<SetStateAction<boolean>>; setActionFailed: Dispatch<SetStateAction<boolean>>;
  setEditing: Dispatch<SetStateAction<Editing | null>>; setNotice: Dispatch<SetStateAction<Notice | null | undefined>>;
  reload: () => void;
}) {
  async function guarded(id: string, work: () => Promise<void>) {
    if (ctx.busy.current) return;
    ctx.busy.current = true;
    ctx.setBusyId(id);
    ctx.setMutationPending(true);
    ctx.setActionFailed(false);
    try { await work(); } finally {
      ctx.busy.current = false;
      ctx.setBusyId(null);
      ctx.setMutationPending(false);
    }
  }
  function gone() {
    ctx.setEditing(null);
    ctx.setNotice({ kind: 'gone' });
    ctx.reload();
  }
  function changedMeanwhile() {
    ctx.setEditing(null);
    ctx.setNotice({ kind: 'changed' });
    ctx.reload();
  }
  return { guarded, gone, changedMeanwhile };
}
