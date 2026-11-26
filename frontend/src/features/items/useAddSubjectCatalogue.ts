import { useEffect, useMemo, useRef, useState, type Dispatch, type RefObject, type SetStateAction } from 'react';
import { getSubject } from '../subjects/subjectsApi';
import { listActiveSubjects, type Subject } from './itemsApi';
import type { PendingItemCreate } from './pendingItemCreate';

const MAX_SUBJECT_PAGES = 20;

export type SubjectState = 'loading' | 'error' | 'ready';

type CatalogueOptions = {
  userId: string;
  householdId: string;
  mounted: RefObject<boolean>;
  addingRef: RefObject<boolean>;
  adding: boolean;
  pendingCreate: PendingItemCreate | undefined;
  labelNames: Map<string, string>;
  onSignedOut: () => void;
};

/** Active-subject catalogue for the add-item form, including recovery lookups and retry focus. */
export function useAddSubjectCatalogue(options: CatalogueOptions) {
  const { userId, householdId, mounted, addingRef, adding, pendingCreate, labelNames, onSignedOut } = options;
  const [subjects, setSubjects] = useState<Subject[]>([]);
  const [subjectLoading, setSubjectLoading] = useState(false);
  const [subjectScope, setSubjectScope] = useState(`${userId}:${householdId}`);
  const [subjectState, setSubjectState] = useState<SubjectState>('loading');
  const subjectGeneration = useRef(0);
  const catalogueLoaded = useRef(false);
  const { catalogueRetryRef, noSubjectsRef, subjectFieldRef, markRetryFocus } = useCatalogueRetryFocus(subjectState);
  const activeSubjects = useMemo(
    () => subjectScope === `${userId}:${householdId}` ? subjects : [],
    [subjectScope, userId, householdId, subjects],
  );
  const names = useMemo(
    () => new Map([...labelNames, ...activeSubjects.map((subject) => [subject.id, subject.name] as const)]),
    [labelNames, activeSubjects],
  );

  async function loadAddSubjects(token: number) {
    const scope = `${userId}:${householdId}`;
    const isCurrent = () => mounted.current && token === subjectGeneration.current && addingRef.current;
    if (!isCurrent()) return;
    setSubjects([]);
    setSubjectScope(scope);
    setSubjectState('loading');
    setSubjectLoading(true);
    const loaded = await collectSubjects(householdId, isCurrent);
    // Returning from the collector is another async boundary: re-check before acting on its outcome.
    if (loaded === 'stale' || !isCurrent()) return;
    if (loaded === 'unauthenticated') { setSubjectLoading(false); onSignedOut(); return; }
    if (loaded === 'error') { setSubjects([]); setSubjectLoading(false); setSubjectState('error'); return; }
    setSubjects(loaded);
    catalogueLoaded.current = true;
    setSubjectLoading(false);
    setSubjectState('ready');
  }

  /** Starts a catalogue load for a fresh form, or marks it ready when recovering a pending create. */
  function open(recovering: boolean) {
    subjectGeneration.current++;
    const token = subjectGeneration.current;
    if (recovering) { setSubjectState('ready'); setSubjectLoading(false); return; }
    setSubjects([]);
    setSubjectScope(`${userId}:${householdId}`);
    setSubjectState('loading');
    setSubjectLoading(true);
    void loadAddSubjects(token);
  }

  function retryAddSubjects() {
    if (subjectLoading) return;
    const token = ++subjectGeneration.current;
    setSubjectState('loading');
    setSubjectLoading(true);
    void loadAddSubjects(token);
  }

  function retryCatalogue() {
    markRetryFocus();
    retryAddSubjects();
  }

  function cancel() {
    subjectGeneration.current++;
    setSubjectLoading(false);
  }

  /** After a rejected recovery attempt, the editable draft needs the catalogue that was never loaded. */
  function loadIfMissing() {
    if (catalogueLoaded.current || subjectLoading) return;
    const token = ++subjectGeneration.current;
    void loadAddSubjects(token);
  }

  useRecoverySubjectLookup({
    householdId, mounted, addingRef, adding, pendingCreate, names, subjectGeneration, onSignedOut, setSubjects,
  });

  return {
    activeSubjects, names, subjectLoading, subjectState, catalogueRetryRef, noSubjectsRef, subjectFieldRef,
    open, retryCatalogue, cancel, loadIfMissing,
  };
}

async function collectSubjects(
  householdId: string,
  isCurrent: () => boolean,
): Promise<Subject[] | 'stale' | 'unauthenticated' | 'error'> {
  const collected: Subject[] = [];
  let cursor: string | undefined;
  for (let page = 0; page < MAX_SUBJECT_PAGES; page++) {
    const result = await listActiveSubjects(householdId, false, cursor);
    if (!isCurrent()) return 'stale';
    if (result.kind === 'unauthenticated') return 'unauthenticated';
    if (result.kind !== 'ok') return 'error';
    collected.push(...result.subjects);
    if (!result.nextCursor) return collected;
    cursor = result.nextCursor;
  }
  return isCurrent() ? 'error' : 'stale';
}

type RecoveryLookupOptions = {
  householdId: string;
  mounted: RefObject<boolean>;
  addingRef: RefObject<boolean>;
  adding: boolean;
  pendingCreate: PendingItemCreate | undefined;
  names: Map<string, string>;
  subjectGeneration: RefObject<number>;
  onSignedOut: () => void;
  setSubjects: Dispatch<SetStateAction<Subject[]>>;
};

/** Fetches the pending create's subject when it is not in the catalogue, so recovery can show its name. */
function useRecoverySubjectLookup(options: RecoveryLookupOptions) {
  const { householdId, mounted, addingRef, adding, pendingCreate, names, subjectGeneration, onSignedOut, setSubjects } = options;
  const recoveryLookup = useRef(new Set<string>());
  useEffect(() => {
    if (!pendingCreate || !adding) return;
    const id = pendingCreate.body.subjectId;
    if (names.has(id) || recoveryLookup.current.has(id)) return;
    const token = subjectGeneration.current;
    const lookup = { active: true };
    const lookupSet = recoveryLookup.current;
    lookupSet.add(id);
    void getSubject(householdId, id).then((result) => {
      if (!lookup.active || !mounted.current || token !== subjectGeneration.current || !addingRef.current) return;
      if (result.kind === 'unauthenticated') { onSignedOut(); return; }
      if (result.kind === 'ok') setSubjects((current) => [...current.filter((entry) => entry.id !== id), result.subject]);
    });
    return () => { lookup.active = false; lookupSet.delete(id); };
  }, [adding, pendingCreate, names, householdId, onSignedOut, mounted, addingRef, subjectGeneration, setSubjects]);
}

/** After a catalogue retry the user started, focus follows the outcome unless the user moved it. */
function useCatalogueRetryFocus(subjectState: SubjectState) {
  const catalogueRetryRef = useRef<HTMLButtonElement>(null);
  const noSubjectsRef = useRef<HTMLButtonElement>(null);
  const subjectFieldRef = useRef<HTMLSelectElement>(null);
  const catalogueRetryHadFocus = useRef(false);
  const formShownAtRetry = useRef(false);
  useEffect(() => {
    if (subjectState === 'loading') return;
    const active = document.activeElement;
    if (catalogueRetryHadFocus.current && (active === catalogueRetryRef.current || active === document.body)) {
      // A fresh form mounts with the catalogue and focuses its title itself. A recovery form was already mounted:
      // an editable draft focuses the For field its 422 flagged, a locked one (For frozen) its Check again action.
      // An empty catalogue focuses its People and things action.
      if (subjectState === 'error') catalogueRetryRef.current?.focus();
      else if (formShownAtRetry.current) focusRecoveryForm(subjectFieldRef.current);
      else noSubjectsRef.current?.focus();
    }
    catalogueRetryHadFocus.current = false;
    formShownAtRetry.current = false;
  }, [subjectState]);
  function markRetryFocus() {
    catalogueRetryHadFocus.current = document.activeElement === catalogueRetryRef.current;
    formShownAtRetry.current = Boolean(subjectFieldRef.current?.isConnected);
  }
  return { catalogueRetryRef, noSubjectsRef, subjectFieldRef, markRetryFocus };
}

function focusRecoveryForm(subjectField: HTMLSelectElement | null) {
  if (!subjectField?.isConnected) return;
  if (!subjectField.disabled) { subjectField.focus(); return; }
  subjectField.form?.querySelector<HTMLButtonElement>('button[type="submit"]:not(:disabled)')?.focus();
}
