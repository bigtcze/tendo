import { useState } from 'react';
import { clearPendingItemCreate, getPendingItemCreate, type PendingItemCreate } from './pendingItemCreate';

export type CreateNotice = 'uncertain' | 'reused' | null;

/**
 * Recovery state for an add-item submission whose outcome may be unknown. A stored pending create reopens
 * locked ("check again") until the server confirms; an invalid replay unlocks it as an editable draft.
 */
export function usePendingCreate(userId: string, householdId: string) {
  const [pendingCreate, setPendingCreate] = useState<PendingItemCreate | undefined>(
    () => getPendingItemCreate(userId, householdId),
  );
  const [createUncertain, setCreateUncertain] = useState(() => Boolean(getPendingItemCreate(userId, householdId)));
  const [recoveryEditable, setRecoveryEditable] = useState(false);
  const [recoverySubjectId, setRecoverySubjectId] = useState<string | null>(null);
  const [createNotice, setCreateNotice] = useState<CreateNotice>(
    () => getPendingItemCreate(userId, householdId) ? 'uncertain' : null,
  );

  function settle(uncertain: boolean, editable: boolean) {
    clearPendingItemCreate(userId, householdId);
    setPendingCreate(undefined);
    setCreateUncertain(uncertain);
    setRecoveryEditable(editable);
  }

  return {
    pendingCreate, createUncertain, recoveryEditable, recoverySubjectId, createNotice,
    /** Reloads any stored pending create when the form opens; returns whether one exists. */
    reopen() {
      const pending = getPendingItemCreate(userId, householdId);
      setPendingCreate(pending);
      setCreateUncertain(Boolean(pending));
      setCreateNotice(pending ? 'uncertain' : null);
      setRecoveryEditable(false);
      return Boolean(pending);
    },
    track(pending: PendingItemCreate) { setPendingCreate(pending); },
    signedOut() { clearPendingItemCreate(userId, householdId); setPendingCreate(undefined); },
    invalid(subjectId: string) {
      setCreateNotice(null);
      settle(false, true);
      setRecoverySubjectId(subjectId);
    },
    uncertain() { setCreateUncertain(true); setRecoveryEditable(false); setCreateNotice('uncertain'); },
    rejected(reused: boolean) {
      settle(false, false);
      if (reused) setCreateNotice('reused');
    },
    created() { settle(false, false); setCreateNotice(null); },
  };
}
