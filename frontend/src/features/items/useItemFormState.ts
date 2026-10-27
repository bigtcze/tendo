import { useEffect, useRef, useState, type FormEvent, type RefObject } from 'react';
import {
  baselineValues, errorField, initialDraft, localError, restoredDraft, submittedValues,
  type FormError, type FormField, type ItemDraft, type ItemFormOutcome, type ItemFormValues,
} from './itemFormModel';

type Options = {
  id: string;
  noticeRef: RefObject<HTMLDivElement | null>;
  subjects: { id: string }[];
  onSubmit: (values: ItemFormValues) => Promise<ItemFormOutcome>;
  busy: boolean;
  mode: 'add' | 'edit';
  initialValues?: ItemFormValues;
  notice?: string;
  locked: boolean;
  preserveDraftOnUnlock: boolean;
};

const controlSuffix: Record<FormField, string> = {
  title: 'title',
  subjectId: 'subject',
  responsibleUserId: 'responsible',
  attentionOn: 'date',
  recurrence: 'interval',
  notes: 'notes',
  historicalCompletedOn: 'historical',
};

export function controlId(id: string, field: FormField): string {
  return `${id}-${controlSuffix[field]}`;
}

export function useItemFormState({ id, noticeRef, subjects, onSubmit, busy, mode, initialValues, notice, locked, preserveDraftOnUnlock }: Options) {
  const [draft, setDraft] = useState<ItemDraft>(() => initialDraft(initialValues, subjects));
  const [notesOpen, setNotesOpen] = useState(Boolean(initialValues?.notes));
  const [historicalOpen, setHistoricalOpen] = useState(Boolean(initialValues?.historicalCompletedOn));
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<FormError | null>(null);
  const [showNotice, setShowNotice] = useState(Boolean(notice));
  const submitting = useRef(false);
  const saved = useRef<ItemFormValues>(baselineValues(draft, initialValues));

  const wasLocked = useRef(locked);
  useEffect(() => {
    // A lock (uncertain create, conflict) lifted without a definitive outcome puts the last submitted values back.
    if (wasLocked.current && !locked && preserveDraftOnUnlock) {
      setDraft((current) => restoredDraft(current, saved.current));
      setShowNotice(false);
    }
    wasLocked.current = locked;
  }, [locked, preserveDraftOnUnlock]);
  useEffect(() => {
    if (notice && noticeRef.current) noticeRef.current.focus();
    else document.getElementById(controlId(id, 'title'))?.focus();
  }, [notice, id, noticeRef]);

  const update = (patch: Partial<ItemDraft>) => setDraft((current) => ({ ...current, ...patch }));

  function fail(next: FormError) {
    setShowNotice(false);
    setError(next);
    const target = errorField(next);
    if (target === 'notes') setNotesOpen(true);
    if (target === 'historicalCompletedOn') setHistoricalOpen(true);
    if (target) setTimeout(() => document.getElementById(controlId(id, target))?.focus(), 0);
  }

  function settle(result: ItemFormOutcome) {
    if (result.kind === 'invalid') fail(result);
    else if (result.kind === 'failed') fail({ kind: 'failed' });
    else setShowNotice(result.kind === 'handled');
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (submitting.current || busy) return;
    const invalid = localError(draft);
    if (invalid) return fail(invalid);
    submitting.current = true;
    setPending(true);
    setError(null);
    let result: ItemFormOutcome;
    try {
      const submitted = locked && initialValues ? initialValues : submittedValues(draft, mode);
      saved.current = submitted;
      result = await onSubmit(submitted);
    } catch { result = { kind: 'failed' }; }
    submitting.current = false;
    setPending(false);
    settle(result);
  }

  return {
    draft, update, pending, error, field: errorField(error), showNotice, submit,
    notesOpen, toggleNotes: () => setNotesOpen((open) => !open),
    historicalOpen, toggleHistorical: () => setHistoricalOpen((open) => !open),
  };
}

export type ItemFormState = ReturnType<typeof useItemFormState>;
