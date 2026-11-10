import { useEffect, useId, useRef, type RefObject } from 'react';
import { Button } from '../../components/ui/button';
import { useI18n } from '../../i18n';
import { errorKey, type SubmitOutcome, type SubjectValues } from './subjectFormModel';
import { SubjectFormFields } from './SubjectFormFields';
import { useSubjectFormState } from './useSubjectFormState';

export type { SubmitOutcome, SubjectValues } from './subjectFormModel';
export { typeLabelKey } from './subjectFormModel';

export function SubjectForm({
  label, initial, submitLabel, pendingLabel, notice, yourEntry, archiveLabel, initialFocusRef,
  locked = false, memberPerson = false, retryLabel, onSubmit, onCancel, onArchive, onPendingChange,
}: {
  label: string;
  initial: SubjectValues;
  submitLabel: string;
  pendingLabel: string;
  notice?: string;
  /** What the user had typed when their save was refused, so it is not silently lost. */
  yourEntry?: string;
  archiveLabel?: string;
  initialFocusRef?: RefObject<HTMLInputElement | null>;
  locked?: boolean;
  /** A household member's person: the name stays editable, the type and account link are fixed. */
  memberPerson?: boolean;
  retryLabel?: string;
  onSubmit: (values: SubjectValues) => Promise<SubmitOutcome>;
  onCancel: () => void;
  onArchive?: () => void;
  onPendingChange?: (pending: boolean) => void;
}) {
  const { t } = useI18n();
  const uid = useId();
  const nameId = `${uid}-name`;
  const errorId = `${uid}-error`;
  const formRef = useRef<HTMLFormElement>(null);
  const noticeRef = useRef<HTMLDivElement>(null);
  const state = useSubjectFormState({ initial, nameId, notice, locked, onSubmit, formRef, noticeRef });
  useEffect(() => { onPendingChange?.(state.pending); }, [onPendingChange, state.pending]);

  return (
    <form ref={formRef} onSubmit={state.submit} aria-label={label} noValidate
      className="space-y-5">
      {notice && state.showNotice ? (
        <div ref={noticeRef} tabIndex={-1} role="status" className="space-y-1 rounded-xl border border-line bg-sand px-4 py-3 text-ink outline-none">
          <p>{notice}</p>
          {yourEntry ? <p className="break-words text-small text-muted">{yourEntry}</p> : null}
        </div>
      ) : null}
      <SubjectFormFields
        initialFocusRef={initialFocusRef}
        uid={uid} nameId={nameId} errorId={errorId} target={state.target} name={state.name} changeName={state.changeName}
        type={state.type} changeType={state.changeType} locked={locked} memberPerson={memberPerson}
      />
      <div id={errorId} role="alert" className={state.error ? 'rounded-xl border border-danger bg-danger-bg px-4 py-3 text-danger' : undefined}>
        {state.error ? t(errorKey(state.error)) : null}
      </div>
      <SubjectFormActions pending={state.pending} pendingAttempt={state.pendingAttempt} locked={locked}
        submitLabel={submitLabel} pendingLabel={pendingLabel} retryLabel={retryLabel} cancelLabel={t('subjects.cancel')}
        archiveLabel={archiveLabel} onArchive={onArchive} onCancel={onCancel} />
    </form>
  );
}

/** Save/Cancel, plus a quiet Archive on the edit form only; Archive sits apart so it is never mistaken for Save. */
function SubjectFormActions({ pending, pendingAttempt, locked, submitLabel, pendingLabel, retryLabel, cancelLabel,
  archiveLabel, onArchive, onCancel }: {
  pending: boolean; pendingAttempt: boolean; locked: boolean; submitLabel: string; pendingLabel: string; retryLabel?: string;
  cancelLabel: string; archiveLabel?: string; onArchive?: () => void; onCancel: () => void;
}) {
  const submitText = locked
    ? (pending && pendingAttempt ? pendingLabel : retryLabel ?? submitLabel)
    : (pending ? pendingLabel : submitLabel);
  return <div className="flex flex-wrap items-center gap-2 border-t border-line pt-5">
    {onArchive && archiveLabel ? <div className="mr-auto">
      <Button type="button" variant="quiet" onClick={onArchive} disabled={pending || locked}>
        {archiveLabel}
      </Button>
    </div> : null}
    <span className="flex-1" />
    <Button type="submit" disabled={pending}
      className={locked ? 'w-full whitespace-normal text-center sm:w-auto' : undefined}>{submitText}</Button>
    <Button type="button" variant="quiet" onClick={onCancel} disabled={pending}>{cancelLabel}</Button>
  </div>;
}
