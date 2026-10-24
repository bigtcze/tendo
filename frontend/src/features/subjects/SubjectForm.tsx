import { useEffect, useId, useRef, useState, type FormEvent } from 'react';
import { Button } from '../../components/ui/button';
import { Input } from '../../components/ui/input';
import { Label } from '../../components/ui/label';
import { cn } from '../../lib/cn';
import { useI18n, type MessageKey } from '../../i18n';
import { subjectTypes, type Invalid, type SubjectType } from './subjectsApi';

export interface SubjectValues {
  name: string;
  type: SubjectType;
  linkedUserId: string | null;
}

export type SubmitOutcome =
  | { kind: 'done' }
  /** The parent already dealt with the result (for example a conflict), so the form shows no error. */
  | { kind: 'handled' }
  | { kind: 'failed' }
  | Invalid;

type FormError = { kind: 'empty' } | Invalid | { kind: 'failed' };

const typeLabels: Record<SubjectType, MessageKey> = {
  person: 'subjects.type.person',
  home: 'subjects.type.home',
  vehicle: 'subjects.type.vehicle',
  pet: 'subjects.type.pet',
  custom: 'subjects.type.custom',
};

export function typeLabelKey(type: SubjectType): MessageKey {
  return typeLabels[type];
}

function errorKey(error: FormError): MessageKey {
  if (error.kind === 'empty') return 'subjects.error.nameEmpty';
  if (error.kind === 'failed') return 'subjects.error.save';
  if (error.field === 'type') return 'subjects.error.type';
  if (error.field === 'linkedUserId') return error.code === 'requires_person' ? 'subjects.error.linkRequiresPerson' : 'subjects.error.linkInvalid';
  if (error.code === 'invalid_characters') return 'subjects.error.nameCharacters';
  if (error.code === 'invalid_length') return 'subjects.error.nameLength';
  return 'subjects.error.nameInvalid';
}

function errorTarget(error: FormError): 'name' | 'type' | 'linkedUserId' | null {
  if (error.kind === 'empty') return 'name';
  if (error.kind === 'invalid') return error.field;
  return null;
}

export function SubjectForm({
  label,
  initial,
  members = [],
  membersLoaded = false,
  submitLabel,
  pendingLabel,
  notice,
  yourEntry,
  locked = false,
  retryLabel,
  onSubmit,
  onCancel,
}: {
  label: string;
  initial: SubjectValues;
  members?: { userId: string; login: string }[];
  membersLoaded?: boolean;
  submitLabel: string;
  pendingLabel: string;
  notice?: string;
  /** What the user had typed when their save was refused, so it is not silently lost. */
  yourEntry?: string;
  locked?: boolean;
  retryLabel?: string;
  onSubmit: (values: SubjectValues) => Promise<SubmitOutcome>;
  onCancel: () => void;
}) {
  const { t } = useI18n();
  const uid = useId();
  const nameId = `${uid}-name`;
  const linkedId = `${uid}-linked-account`;
  const errorId = `${uid}-error`;
  const formRef = useRef<HTMLFormElement>(null);
  const [name, setName] = useState(initial.name);
  const [type, setType] = useState<SubjectType>(initial.type);
  const [linkedUserId, setLinkedUserId] = useState<string | null>(initial.linkedUserId);
  const [pending, setPending] = useState(false);
  const [pendingAttempt, setPendingAttempt] = useState(false);
  const [error, setError] = useState<FormError | null>(null);
  const [showNotice, setShowNotice] = useState(Boolean(notice));
  const submitting = useRef(false);
  const noticeRef = useRef<HTMLDivElement>(null);
  const target = error ? errorTarget(error) : null;

  // A freshly opened form takes focus. After a conflict the notice comes first so it is announced.
  useEffect(() => {
    if (noticeRef.current) noticeRef.current.focus();
    else document.getElementById(nameId)?.focus();
  }, [nameId, notice]);

  function fail(next: FormError) {
    setShowNotice(false);
    setError(next);
    const field = errorTarget(next);
    // Focus right away: the input exists already and the alert text follows on the next render.
    if (field === 'name') document.getElementById(nameId)?.focus();
    if (field === 'type') formRef.current?.querySelector<HTMLInputElement>('input[type="radio"]:checked')?.focus();
    if (field === 'linkedUserId') document.getElementById(linkedId)?.focus();
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (submitting.current) return;
    if (!name.trim()) {
      fail({ kind: 'empty' });
      return;
    }
    submitting.current = true;
    setPending(true);
    setPendingAttempt(locked);
    setError(null);
    let outcome: SubmitOutcome;
    try {
      outcome = await onSubmit({ name, type, linkedUserId: type === 'person' ? linkedUserId : null });
    } catch {
      outcome = { kind: 'failed' };
    }
    submitting.current = false;
    setPending(false);
    if (outcome.kind === 'invalid') fail(outcome);
    else if (outcome.kind === 'failed') fail({ kind: 'failed' });
    else if (outcome.kind === 'done') setShowNotice(false);
    else setShowNotice(true);
    setPendingAttempt(false);
  }

  return (
    <form
      ref={formRef}
      onSubmit={submit}
      aria-label={label}
      noValidate
      className="space-y-5 rounded-2xl border border-line bg-white/70 p-4 sm:p-6"
    >
      {notice && showNotice ? (
        <div ref={noticeRef} tabIndex={-1} role="status" className="space-y-1 rounded-xl bg-sand px-4 py-3 text-ink outline-none">
          <p>{notice}</p>
          {yourEntry ? <p className="break-words text-sm text-muted">{yourEntry}</p> : null}
        </div>
      ) : null}
      <div className="space-y-2">
        <Label htmlFor={nameId}>{t('subjects.name')}</Label>
        <Input
          id={nameId}
          name="name"
          type="text"
          autoComplete="off"
          required
          value={name}
          onChange={(e) => { setShowNotice(false); setName(e.target.value); }}
          disabled={locked}
          aria-invalid={target === 'name'}
          aria-describedby={target === 'name' ? errorId : undefined}
        />
      </div>
      <fieldset
        role="radiogroup"
        className="min-w-0 space-y-2"
        aria-invalid={target === 'type'}
        aria-describedby={target === 'type' ? errorId : undefined}
      >
        <legend className="text-sm font-medium text-ink">{t('subjects.type.legend')}</legend>
        <div className="flex flex-wrap gap-2 pt-1">
          {subjectTypes.map((option) => (
            <label key={option} className="relative">
              <input
                type="radio"
                name={`${uid}-type`}
                value={option}
                checked={type === option}
                onChange={() => { setShowNotice(false); setType(option); }}
                disabled={locked}
                className="peer absolute inset-0 size-full cursor-pointer opacity-0"
              />
              <span
                className={cn(
                  'inline-flex min-h-11 items-center rounded-xl border border-field bg-white px-4 text-base text-ink transition-colors',
                  'peer-hover:bg-sand peer-checked:border-accent peer-checked:bg-accent peer-checked:text-white',
                  'peer-focus-visible:outline-2 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-accent',
                )}
              >
                {t(typeLabels[option])}
              </span>
            </label>
          ))}
        </div>
      </fieldset>
      {type === 'person' && membersLoaded && (members.length > 0 || linkedUserId !== null) ? (
        <div className="space-y-2">
          <Label htmlFor={linkedId} className="text-muted">{t('subjects.link.label')}</Label>
          <select
            id={linkedId}
            value={linkedUserId ?? ''}
            onChange={(event) => { setShowNotice(false); setLinkedUserId(event.target.value || null); }}
            disabled={locked}
            aria-invalid={target === 'linkedUserId'}
            aria-describedby={target === 'linkedUserId' ? errorId : `${linkedId}-hint`}
            className="min-h-11 w-full rounded-xl border border-field bg-white px-3 text-ink focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
          >
            <option value="">{t('subjects.link.none')}</option>
            {linkedUserId && !members.some((member) => member.userId === linkedUserId) ? (
              <option value={linkedUserId}>{t('subjects.link.unknown')}</option>
            ) : null}
            {members.map((member) => <option key={member.userId} value={member.userId}>{member.login}</option>)}
          </select>
          <p id={`${linkedId}-hint`} className="text-sm text-muted">{t('subjects.link.hint')}</p>
        </div>
      ) : null}
      {type !== 'person' && initial.linkedUserId && linkedUserId ? <p className="text-sm text-muted">{t('subjects.link.clearedByType')}</p> : null}
      <div id={errorId} role="alert" className="text-base text-danger">
        {error ? t(errorKey(error)) : null}
      </div>
      <div className="flex flex-wrap gap-2">
        {locked ? (
          <Button type="submit" disabled={pending} className="w-full whitespace-normal text-center sm:w-auto">
            {pending && pendingAttempt ? pendingLabel : retryLabel ?? submitLabel}
          </Button>
        ) : (
          <Button type="submit" disabled={pending}>
            {pending ? pendingLabel : submitLabel}
          </Button>
        )}
        <Button type="button" variant="quiet" onClick={onCancel} disabled={pending}>
          {t('subjects.cancel')}
        </Button>
      </div>
    </form>
  );
}
