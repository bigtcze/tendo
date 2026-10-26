import { useEffect, useRef, useState, type RefObject } from 'react';
import { useI18n } from '../../i18n';
import { listMembers, type Member } from '../members/membersApi';
import { idempotencyKey } from '../../lib/idempotencyKey';
import { getPendingItemCreate, setPendingItemCreate, type PendingItemCreate } from './pendingItemCreate';
import { usePendingCreate } from './usePendingCreate';
import { createItem, type CreateResult, type Invalid, type Subject } from './itemsApi';
import type { ItemFormValues } from './ItemForm';
import { useAddSubjectCatalogue } from './useAddSubjectCatalogue';

export type { SubjectState } from './useAddSubjectCatalogue';

const MAX_MEMBER_PAGES = 20;

export type { CreateNotice } from './usePendingCreate';
export type SubmitOutcome = { kind: 'done' | 'handled' | 'failed' } | Invalid;
type Lock = { begin: () => boolean; end: () => void };
type CreateOutcome = Exclude<CreateResult, { kind: 'ok' }>;

type AddItemOptions = {
  userId: string;
  householdId: string;
  mounted: RefObject<boolean>;
  lock: Lock;
  refresh: () => Promise<void>;
  invalidateHome: () => void;
  dataHousehold: RefObject<string | null>;
  labelNames: Map<string, string>;
  setNotice: (notice: { text: string }) => void;
  onSignedOut: () => void;
  addRef: RefObject<HTMLButtonElement | null>;
  memberLoadGenerationRef: RefObject<number>;
};

/** Add-item form state: catalogue and member loading, pending-create recovery and idempotent submission. */
export function useAddItem(options: AddItemOptions) {
  const { userId, householdId, mounted, lock, refresh, invalidateHome, dataHousehold, labelNames } = options;
  const { setNotice, onSignedOut, addRef, memberLoadGenerationRef } = options;
  const { t } = useI18n();
  const [members, setMembers] = useState<Member[]>([]);
  const recovery = usePendingCreate(userId, householdId);
  const { pendingCreate, createUncertain, recoveryEditable } = recovery;
  const [adding, setAdding] = useState(() => Boolean(getPendingItemCreate(userId, householdId)));
  const addingRef = useRef(Boolean(getPendingItemCreate(userId, householdId)));
  const returnFocusRef = useRef(false);
  const catalogue = useAddSubjectCatalogue({
    userId, householdId, mounted, addingRef, adding, pendingCreate, labelNames, onSignedOut,
  });
  const pendingValues = pendingCreate ? formValues(pendingCreate) : undefined;
  const recoveryTarget = pendingCreate?.body.subjectId ?? recovery.recoverySubjectId;

  async function openAddForm() {
    if (dataHousehold.current !== householdId) return;
    const token = ++memberLoadGenerationRef.current;
    addingRef.current = true;
    setAdding(true);
    catalogue.open(recovery.reopen());
    setMembers([]);
    const isCurrent = () => mounted.current && token === memberLoadGenerationRef.current && addingRef.current;
    const loaded = await collectMembers(householdId, isCurrent);
    if (loaded === 'stale') return;
    if (loaded === 'unauthenticated') onSignedOut();
    else setMembers(loaded);
  }

  function cancelAddForm() {
    memberLoadGenerationRef.current++;
    catalogue.cancel();
    addingRef.current = false;
    setAdding(false);
    returnFocusRef.current = true;
  }

  async function finishCreated(title: string, hadFocus: boolean): Promise<SubmitOutcome> {
    recovery.created();
    invalidateHome();
    memberLoadGenerationRef.current++;
    addingRef.current = false;
    returnFocusRef.current = true;
    setAdding(false);
    await refresh();
    setNotice({ text: t('items.notice.added', { title }) });
    if (hadFocus) requestAnimationFrame(() => addRef.current?.focus());
    return { kind: 'done' };
  }

  async function create(values: ItemFormValues): Promise<SubmitOutcome> {
    if (!lock.begin()) return { kind: 'handled' };
    const recoveryAttempt = createUncertain && !!pendingCreate;
    const hadFocus = document.activeElement === addRef.current;
    try {
      const pending = pendingCreate && !recoveryEditable ? pendingCreate : newPendingCreate(values);
      if (!pendingCreate) { setPendingItemCreate(userId, householdId, pending); recovery.track(pending); }
      const result = await createItem(householdId, pending.body, pending.key);
      if (result.kind === 'ok') return await finishCreated(result.item.title, hadFocus);
      const outcome = applyOutcome(result, recovery, () => { if (recoveryAttempt) catalogue.loadIfMissing(); }, values.subjectId);
      if (result.kind === 'unauthenticated') onSignedOut();
      return outcome;
    } finally { lock.end(); }
  }

  return {
    ...catalogue,
    members, pendingCreate, createUncertain, recoveryEditable, adding, createNotice: recovery.createNotice, pendingValues,
    retryDraft: recoveryEditable && !createUncertain ? pendingValues : undefined,
    recoverySubjects: recoveryCatalogue(recoveryTarget, catalogue.activeSubjects, labelNames, t('items.subject.unknown')),
    returnFocusRef, openAddForm, cancelAddForm, create,
    cancelForPeople() { cancelAddForm(); addRef.current?.focus(); },
  };
}

/**
 * Returns focus to Add item after the form closes. The screen calls this after the label-retry and
 * completion-notice focus effects so that, as before the split, it wins when they settle together.
 */
export function useAddReturnFocus(adding: boolean, returnFocusRef: RefObject<boolean>, addRef: RefObject<HTMLButtonElement | null>) {
  useEffect(() => {
    if (!adding && returnFocusRef.current) { addRef.current?.focus(); returnFocusRef.current = false; }
  }, [adding, addRef, returnFocusRef]);
}

function applyOutcome(
  result: CreateOutcome,
  recovery: ReturnType<typeof usePendingCreate>,
  reloadCatalogue: () => void,
  subjectId: string,
): SubmitOutcome {
  if (result.kind === 'unauthenticated') { recovery.signedOut(); return { kind: 'handled' }; }
  if (result.kind === 'invalid') { recovery.invalid(subjectId); reloadCatalogue(); return result; }
  if (result.kind === 'error') { recovery.uncertain(); return { kind: 'handled' }; }
  recovery.rejected(result.kind === 'reused');
  return { kind: result.kind === 'reused' ? 'handled' : 'failed' };
}

async function collectMembers(householdId: string, isCurrent: () => boolean): Promise<Member[] | 'stale' | 'unauthenticated'> {
  const collected: Member[] = [];
  let cursor: string | undefined;
  for (let page = 0; page < MAX_MEMBER_PAGES; page++) {
    const result = await listMembers(householdId, cursor);
    if (!isCurrent()) return 'stale';
    if (result.kind === 'unauthenticated') return 'unauthenticated';
    if (result.kind !== 'ok') return [];
    collected.push(...result.items);
    if (!result.nextCursor) return collected;
    cursor = result.nextCursor;
  }
  return isCurrent() ? [] : 'stale';
}

function formValues(pending: PendingItemCreate): ItemFormValues {
  const { body } = pending;
  return {
    title: body.title,
    subjectId: body.subjectId,
    attentionOn: body.attentionOn ?? '',
    notes: body.notes ?? '',
    recurrence: body.recurrence ?? null,
    responsibleUserId: body.responsibleUserId ?? null,
    ...(body.historicalCompletedOn ? { historicalCompletedOn: body.historicalCompletedOn } : {}),
  };
}

function recoveryCatalogue(
  target: string | null,
  active: Subject[],
  labels: Map<string, string>,
  unknownName: string,
): Subject[] {
  const available = active.filter((subject) => !subject.archived && subject.id !== target);
  if (!target) return available;
  const base = { id: target, type: 'custom' as const, linkedUserId: null, createdAt: '', updatedAt: '' };
  const label = labels.get(target);
  const fallback: Subject = label !== undefined
    ? { ...base, name: label, archived: false }
    : { ...base, name: unknownName, archived: true };
  return [...available, active.find((subject) => subject.id === target) ?? fallback];
}

function newPendingCreate(values: ItemFormValues): PendingItemCreate {
  return {
    version: 1,
    key: idempotencyKey(),
    body: {
      title: values.title,
      subjectId: values.subjectId,
      ...(values.attentionOn ? { attentionOn: values.attentionOn } : {}),
      ...(values.notes ? { notes: values.notes } : {}),
      recurrence: values.recurrence,
      ...(values.responsibleUserId !== null ? { responsibleUserId: values.responsibleUserId } : {}),
      ...(values.recurrence && values.historicalCompletedOn ? { historicalCompletedOn: values.historicalCompletedOn } : {}),
    },
  };
}
