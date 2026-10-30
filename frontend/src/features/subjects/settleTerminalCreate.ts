import { clearPendingSubjectCreate, type PendingSubjectCreate } from './pendingSubjectCreate';
import type { Dispatch, SetStateAction } from 'react';
import type { Notice } from './subjectsModel';
import type { CreateResult } from './subjectsApi';

type TerminalCreateContext = {
  userId: string;
  householdId: string;
  key: string;
  setPendingCreate: Dispatch<SetStateAction<PendingSubjectCreate | undefined>>;
  setCreateUncertain: Dispatch<SetStateAction<boolean>>;
  setNotice: Dispatch<SetStateAction<Notice | null | undefined>>;
};

export function settleTerminalCreate(
  result: CreateResult,
  { userId, householdId, key, setPendingCreate, setCreateUncertain, setNotice }: TerminalCreateContext,
): { outcome?: { kind: 'handled' } | { kind: 'failed' }; error?: Extract<CreateResult, { kind: 'invalid' }> } | undefined {
  if (result.kind !== 'invalid' && result.kind !== 'reused' && result.kind !== 'definitive') return undefined;
  clearPendingSubjectCreate(userId, householdId, key);
  setPendingCreate(undefined);
  setCreateUncertain(false);
  if (result.kind === 'reused') {
    setNotice({ kind: 'reused' });
    return { outcome: { kind: 'handled' } };
  }
  if (result.kind === 'invalid') return { error: result };
  return { outcome: { kind: 'failed' } };
}
