import { useEffect, useId, useRef, type RefObject } from 'react';
import { Button } from '../../components/ui/button';
import { useI18n } from '../../i18n';
import { errorKey, type SubmitOutcome, type SubjectValues } from './subjectFormModel';
import { SubjectFormFields } from './SubjectFormFields';
import { useSubjectFormState } from './useSubjectFormState';

export type { SubmitOutcome, SubjectValues } from './subjectFormModel';
export { typeLabelKey } from './subjectFormModel';

export function SubjectForm({
  label, initial, members = [], membersLoaded = false, submitLabel, pendingLabel, notice, yourEntry, archiveLabel, initialFocusRef,
  locked = false, retryLabel, onSubmit, onCancel, onArchive, onPendingChange,
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
  archiveLabel?: string;
  initialFocusRef?: RefObject<HTMLInputElement | null>;
  locked?: boolean;
  retryLabel?: string;
  onSubmit: (values: SubjectValues) => Promise<SubmitOutcome>;
  onCancel: () => void;
  onArchive?: () => void;
  onPendingChange?: (pending: boolean) => void;
}) {
  const { t } = useI18n();
  const uid = useId();
  const nameId = `${uid}-name`;
  const linkedId = `${uid}-linked-account`;
  const errorId = `${uid}-error`;
  const formRef = useRef<HTMLFormElement>(null);
  const noticeRef = useRef<HTMLDivElement>(null);
  const state = useSubjectFormState({ initial, nameId, linkedId, notice, locked, onSubmit, formRef, noticeRef });
  useEffect(() => { onPendingChange?.(state.pending); }, [onPendingChange, state.pending]);

  return (
    <form ref={formRef} onSubmit={state.submit} aria-label={label} noValidate
      className="space-y-5 p-1">
      {notice && state.showNotice ? (
        <div ref={noticeRef} tabIndex={-1} role="status" className="space-y-1 rounded-xl bg-sand px-4 py-3 text-ink outline-none">
          <p>{notice}</p>
          {yourEntry ? <p className="break-words text-sm text-muted">{yourEntry}</p> : null}
        </div>
      ) : null}
      <SubjectFormFields
        initialFocusRef={initialFocusRef}
        uid={uid} nameId={nameId} linkedId={linkedId} errorId={errorId} target={state.target} name={state.name} changeName={state.changeName}
        type={state.type} changeType={state.changeType} linkedUserId={state.linkedUserId} changeLinkedUserId={state.changeLinkedUserId}
        locked={locked} members={members} membersLoaded={membersLoaded} initial={initial}
      />
      <div id={errorId} role="alert" className="text-base text-danger">
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
  return <div className="flex flex-wrap gap-2">
    {onArchive && archiveLabel ? <Button type="button" variant="quiet" onClick={onArchive} disabled={pending || locked}>
      {archiveLabel}
    </Button> : null}
    <span className="flex-1" />
    <Button type="submit" disabled={pending}
      className={locked ? 'w-full whitespace-normal text-center sm:w-auto' : undefined}>{submitText}</Button>
    <Button type="button" variant="quiet" onClick={onCancel} disabled={pending}>{cancelLabel}</Button>
  </div>;
}
