import { useId, useRef } from 'react';
import { Button } from '../../components/ui/button';
import { useI18n } from '../../i18n';
import { errorKey, type SubmitOutcome, type SubjectValues } from './subjectFormModel';
import { SubjectFormFields } from './SubjectFormFields';
import { useSubjectFormState } from './useSubjectFormState';

export type { SubmitOutcome, SubjectValues } from './subjectFormModel';
export { typeLabelKey } from './subjectFormModel';

export function SubjectForm({
  label, initial, members = [], membersLoaded = false, submitLabel, pendingLabel, notice, yourEntry,
  locked = false, retryLabel, onSubmit, onCancel,
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
  const noticeRef = useRef<HTMLDivElement>(null);
  const state = useSubjectFormState({ initial, nameId, linkedId, notice, locked, onSubmit, formRef, noticeRef });

  return (
    <form ref={formRef} onSubmit={state.submit} aria-label={label} noValidate
      className="space-y-5 rounded-2xl border border-line bg-white/70 p-4 sm:p-6">
      {notice && state.showNotice ? (
        <div ref={noticeRef} tabIndex={-1} role="status" className="space-y-1 rounded-xl bg-sand px-4 py-3 text-ink outline-none">
          <p>{notice}</p>
          {yourEntry ? <p className="break-words text-sm text-muted">{yourEntry}</p> : null}
        </div>
      ) : null}
      <SubjectFormFields
        uid={uid} nameId={nameId} linkedId={linkedId} errorId={errorId} target={state.target} name={state.name} changeName={state.changeName}
        type={state.type} changeType={state.changeType} linkedUserId={state.linkedUserId} changeLinkedUserId={state.changeLinkedUserId}
        locked={locked} members={members} membersLoaded={membersLoaded} initial={initial}
      />
      <div id={errorId} role="alert" className="text-base text-danger">
        {state.error ? t(errorKey(state.error)) : null}
      </div>
      <div className="flex flex-wrap gap-2">
        {locked ? (
          <Button type="submit" disabled={state.pending} className="w-full whitespace-normal text-center sm:w-auto">
            {state.pending && state.pendingAttempt ? pendingLabel : retryLabel ?? submitLabel}
          </Button>
        ) : (
          <Button type="submit" disabled={state.pending}>
            {state.pending ? pendingLabel : submitLabel}
          </Button>
        )}
        <Button type="button" variant="quiet" onClick={onCancel} disabled={state.pending}>
          {t('subjects.cancel')}
        </Button>
      </div>
    </form>
  );
}
