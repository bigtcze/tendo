import { clearPendingSubjectCreate, type PendingSubjectCreate } from './pendingSubjectCreate';
import type { Dispatch, SetStateAction } from 'react';
import type { Notice } from './subjectsModel';
import type { CreateResult } from './subjectsApi';

export function settleTerminalCreate(
  result: CreateResult,
  userId: string,
  householdId: string,
  setPendingCreate: Dispatch<SetStateAction<PendingSubjectCreate | undefined>>,
  setCreateUncertain: Dispatch<SetStateAction<boolean>>,
  setNotice: Dispatch<SetStateAction<Notice | null | undefined>>,
): { outcome?: { kind: 'handled' } | { kind: 'failed' }; error?: Extract<CreateResult, { kind: 'invalid' }> } | undefined {
  if (result.kind !== 'invalid' && result.kind !== 'reused' && result.kind !== 'definitive') return undefined;
  clearPendingSubjectCreate(userId, householdId);
  setPendingCreate(undefined);
  setCreateUncertain(false);
  if (result.kind === 'reused') {
    setNotice({ kind: 'reused' });
    return { outcome: { kind: 'handled' } };
  }
  if (result.kind === 'invalid') return { error: result };
  return { outcome: { kind: 'failed' } };
}
