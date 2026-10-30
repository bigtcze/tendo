import { useEffect, useRef, useState, type FormEvent, type RefObject } from 'react';
import {
  errorTarget, hasEmptyName, submittedValues,
  type FormError, type SubmitOutcome, type SubjectValues,
} from './subjectFormModel';
import type { SubjectType } from './subjectsApi';

type Options = {
  initial: SubjectValues;
  nameId: string;
  linkedId: string;
  notice?: string;
  locked: boolean;
  onSubmit: (values: SubjectValues) => Promise<SubmitOutcome>;
  formRef: RefObject<HTMLFormElement | null>;
  noticeRef: RefObject<HTMLDivElement | null>;
};

export function useSubjectFormState({ initial, nameId, linkedId, notice, locked, onSubmit, formRef, noticeRef }: Options) {
  const [name, setName] = useState(initial.name);
  const [type, setType] = useState<SubjectType>(initial.type);
  const [linkedUserId, setLinkedUserId] = useState<string | null>(initial.linkedUserId);
  const [pending, setPending] = useState(false);
  const [pendingAttempt, setPendingAttempt] = useState(false);
  const [error, setError] = useState<FormError | null>(null);
  const [showNotice, setShowNotice] = useState(Boolean(notice));
  const submitting = useRef(false);
  const target = error ? errorTarget(error) : null;

  useEffect(() => {
    if (noticeRef.current) noticeRef.current.focus();
    else document.getElementById(nameId)?.focus();
  }, [nameId, notice, noticeRef]);

  function fail(next: FormError) {
    setShowNotice(false);
    setError(next);
    const field = errorTarget(next);
    if (field === 'name') document.getElementById(nameId)?.focus();
    if (field === 'type') formRef.current?.querySelector<HTMLInputElement>('input[type="radio"]:checked')?.focus();
    if (field === 'linkedUserId') document.getElementById(linkedId)?.focus();
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (submitting.current) return;
    if (hasEmptyName(name)) {
      fail({ kind: 'empty' });
      return;
    }
    submitting.current = true;
    setPending(true);
    setPendingAttempt(locked);
    setError(null);
    let outcome: SubmitOutcome;
    try {
      outcome = await onSubmit(submittedValues(name, type, linkedUserId));
    } catch {
      outcome = { kind: 'failed' };
    }
    submitting.current = false;
    setPending(false);
    settle(outcome);
  }

  function settle(outcome: SubmitOutcome) {
    if (outcome.kind === 'invalid') fail(outcome);
    else if (outcome.kind === 'failed') fail({ kind: 'failed' });
    else if (outcome.kind === 'done') setShowNotice(false);
    else setShowNotice(true);
    setPendingAttempt(false);
  }

  function changeName(nextName: string) {
    setShowNotice(false);
    setName(nextName);
  }

  function changeType(nextType: SubjectType) {
    setShowNotice(false);
    setType(nextType);
  }

  function changeLinkedUserId(nextLinkedUserId: string | null) {
    setShowNotice(false);
    setLinkedUserId(nextLinkedUserId);
  }

  return {
    name, changeName, type, changeType, linkedUserId, changeLinkedUserId,
    pending, pendingAttempt, error, target, showNotice, submit,
  };
}
