import type { Dispatch, MutableRefObject, SetStateAction } from 'react';
import type { Editing, Notice } from './subjectsModel';
import type { Subject } from './subjectsApi';
import { useGuardedMutation } from './useGuardedMutation';
import { useSubjectEditing } from './useSubjectEditing';
import { useSubjectArchive } from './useSubjectArchive';

export function useSubjectActions(ctx: {
  householdId: string; showArchived: boolean; loadingMore: boolean; mutationPending: boolean; adding: boolean;
  editing: Editing | null; busy: MutableRefObject<boolean>; setBusyId: Dispatch<SetStateAction<string | null>>;
  setMutationPending: Dispatch<SetStateAction<boolean>>; setActionFailed: Dispatch<SetStateAction<boolean>>;
  generation: MutableRefObject<number>; archivePage: MutableRefObject<number | null>; pageLoad: MutableRefObject<number>;
  setNotice: Dispatch<SetStateAction<Notice | null | undefined>>; setEditing: Dispatch<SetStateAction<Editing | null>>;
  setAdding: Dispatch<SetStateAction<boolean>>; resetMembers: () => void; updateRow: (subject: Subject) => void;
  reload: () => void; reloadPage: (cursor: string | undefined, stack: string[], focusId: string) => Promise<void>;
  currentCursor: string | undefined; cursorStack: string[]; focusKey: MutableRefObject<string | null>;
  setList: Dispatch<SetStateAction<import('./subjectsModel').ListState>>; noticeRef: MutableRefObject<HTMLDivElement | null>;
  onSignedOut: () => void;
}) {
  const shared = useGuardedMutation(ctx);
  const edit = useSubjectEditing({ ...ctx, ...shared });
  const archive = useSubjectArchive({ ...ctx, ...shared });
  return { ...shared, ...edit, ...archive };
}
