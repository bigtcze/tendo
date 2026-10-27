import type { Dispatch, MutableRefObject, SetStateAction } from 'react';
import { clearPendingSubjectCreate, setPendingSubjectCreate, type PendingSubjectCreate } from './pendingSubjectCreate';
import { idempotencyKey } from '../../lib/idempotencyKey';
import { createSubject, type Subject } from './subjectsApi';
import type { SubjectValues, SubmitOutcome } from './SubjectForm';
import type { ListState, Notice } from './subjectsModel';
import { settleTerminalCreate } from './settleTerminalCreate';
import { refreshAfterCreate } from './refreshAfterCreate';
import { useAddSubjectForm } from './useAddSubjectForm';

export type CreateContext = {
  userId: string; householdId: string; generation: MutableRefObject<number>; addForm: MutableRefObject<number>;
  pendingCreate: PendingSubjectCreate | undefined; setPendingCreate: Dispatch<SetStateAction<PendingSubjectCreate | undefined>>;
  setCreateUncertain: Dispatch<SetStateAction<boolean>>; setNotice: Dispatch<SetStateAction<Notice | null | undefined>>;
  setAdding: Dispatch<SetStateAction<boolean>>; setMembers: () => void;
  loadingMore: boolean; mutationPending: boolean; busy: MutableRefObject<boolean>;
  setEditing: Dispatch<SetStateAction<import('./subjectsModel').Editing | null>>;
  setActionFailed: Dispatch<SetStateAction<boolean>>; setLoadingMore: Dispatch<SetStateAction<boolean>>;
  pagingFocus: MutableRefObject<HTMLElement | null>; pageLoad: MutableRefObject<number>;
  setCursorStack: Dispatch<SetStateAction<string[]>>; setCurrentCursor: Dispatch<SetStateAction<string | undefined>>;
  setList: Dispatch<SetStateAction<ListState>>; onSignedOut: () => void;
};

export function useSubjectCreate(ctx: CreateContext) {
  const { userId, householdId, generation, addForm, pendingCreate, setPendingCreate, setCreateUncertain, setNotice,
    setAdding, setLoadingMore, pagingFocus, pageLoad, setCursorStack,
    setCurrentCursor, setList, onSignedOut } = ctx;

  function finishAdd(form: number, subject: Subject): SubmitOutcome {
    if (form !== addForm.current) return { kind: 'handled' };
    setAdding(false);
    setNotice({ kind: 'added', subject });
    return { kind: 'done' };
  }

  async function add(values: SubjectValues): Promise<SubmitOutcome> {
    const started = generation.current;
    const form = addForm.current;
    const body = { name: values.name, type: values.type, ...(values.linkedUserId ? { linkedUserId: values.linkedUserId } : {}) };
    const pending = pendingCreate ?? { version: 1 as const, key: idempotencyKey(), body };
    if (!pendingCreate) {
      setPendingSubjectCreate(userId, householdId, pending);
      setPendingCreate(pending);
    }
    const result = await createSubject(householdId, pending.body, pending.key);
    if (result.kind === 'unauthenticated') {
      onSignedOut();
      return { kind: 'handled' };
    }
    if (result.kind === 'ok') {
      clearPendingSubjectCreate(userId, householdId);
      setPendingCreate(undefined);
      setCreateUncertain(false);
      if (started === generation.current) {
        return refreshAfterCreate({
          householdId, subject: result.subject, generation, form, finishAdd, setLoadingMore, pagingFocus,
          pageLoad, setCursorStack, setCurrentCursor, setList, onSignedOut,
        });
      }
      return finishAdd(form, result.subject);
    }
    const terminal = settleTerminalCreate(result, userId, householdId, setPendingCreate, setCreateUncertain, setNotice);
    if (terminal?.error) return terminal.error;
    if (terminal?.outcome) return terminal.outcome;
    if (started !== generation.current) return { kind: 'handled' };
    setCreateUncertain(true);
    setNotice(null);
    return { kind: 'handled' };
  }

  const form = useAddSubjectForm(ctx);
  return { add, finishAdd, ...form };
}
