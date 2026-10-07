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
  if (error.code === 'invalid_characters') return 'subjects.error.nameCharacters';
  if (error.code === 'invalid_length') return 'subjects.error.nameLength';
  return 'subjects.error.nameInvalid';
}

function errorTarget(error: FormError): 'name' | 'type' | null {
  if (error.kind === 'empty') return 'name';
  if (error.kind === 'invalid') return error.field;
  return null;
}

export function SubjectForm({
  label,
  initial,
  submitLabel,
  pendingLabel,
  notice,
  yourEntry,
  onSubmit,
  onCancel,
}: {
  label: string;
  initial: SubjectValues;
  submitLabel: string;
  pendingLabel: string;
  notice?: string;
  /** What the user had typed when their save was refused, so it is not silently lost. */
  yourEntry?: string;
  onSubmit: (values: SubjectValues) => Promise<SubmitOutcome>;
  onCancel: () => void;
}) {
  const { t } = useI18n();
  const uid = useId();
  const nameId = `${uid}-name`;
  const errorId = `${uid}-error`;
  const formRef = useRef<HTMLFormElement>(null);
  const [name, setName] = useState(initial.name);
  const [type, setType] = useState<SubjectType>(initial.type);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<FormError | null>(null);
  const submitting = useRef(false);
  const noticeRef = useRef<HTMLDivElement>(null);
  const target = error ? errorTarget(error) : null;

  // A freshly opened form takes focus. After a conflict the notice comes first so it is announced.
  useEffect(() => {
    if (noticeRef.current) noticeRef.current.focus();
    else document.getElementById(nameId)?.focus();
  }, [nameId]);

  function fail(next: FormError) {
    setError(next);
    const field = errorTarget(next);
    // Focus right away: the input exists already and the alert text follows on the next render.
    if (field === 'name') document.getElementById(nameId)?.focus();
    if (field === 'type') formRef.current?.querySelector<HTMLInputElement>('input[type="radio"]:checked')?.focus();
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
    setError(null);
    let outcome: SubmitOutcome;
    try {
      outcome = await onSubmit({ name, type });
    } catch {
      outcome = { kind: 'failed' };
    }
    submitting.current = false;
    setPending(false);
    if (outcome.kind === 'invalid') fail(outcome);
    else if (outcome.kind === 'failed') fail({ kind: 'failed' });
  }

  return (
    <form
      ref={formRef}
      onSubmit={submit}
      aria-label={label}
      noValidate
      className="space-y-5 rounded-2xl border border-line bg-white/70 p-4 sm:p-6"
    >
      {notice ? (
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
          onChange={(e) => setName(e.target.value)}
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
                onChange={() => setType(option)}
                className="peer absolute inset-0 size-full cursor-pointer opacity-0"
              />
              <span
                className={cn(
                  'inline-flex min-h-11 items-center rounded-xl border border-line bg-white px-4 text-base text-ink transition-colors',
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
      <div id={errorId} role="alert" className="text-base text-danger">
        {error ? t(errorKey(error)) : null}
      </div>
      <div className="flex flex-wrap gap-2">
        <Button type="submit" disabled={pending}>
          {pending ? pendingLabel : submitLabel}
        </Button>
        <Button type="button" variant="quiet" onClick={onCancel} disabled={pending}>
          {t('subjects.cancel')}
        </Button>
      </div>
    </form>
  );
}
