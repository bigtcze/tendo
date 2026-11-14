import { useRef, useState } from 'react';
import { Button } from '../../components/ui/button';
import { Dialog } from '../../components/ui/dialog';
import { SubjectForm, type SubjectValues, type SubmitOutcome } from './SubjectForm';
import type { MessageKey } from '../../i18n';

/**
 * The screen's one primary action and its dialog. It sits in the header band (like Home's Add item) but keeps
 * the existing `data-focus="add"` contract, so cancelling the form returns focus to the same control.
 */
export function SubjectAddAction({ adding, createUncertain, createLocked, pendingInitial, addDisabled, t,
  onAdd, onCancelAdd, onAddSubmit }: {
  adding: boolean; createUncertain: boolean; createLocked: boolean; pendingInitial: SubjectValues;
  addDisabled: boolean; t: (key: MessageKey, values?: Record<string, string>) => string;
  onAdd: () => void; onCancelAdd: () => void;
  onAddSubmit: (values: SubjectValues) => Promise<SubmitOutcome>;
}) {
  const addRef = useRef<HTMLButtonElement>(null);
  const addNameRef = useRef<HTMLInputElement | null>(null);
  const [createPending, setCreatePending] = useState(false);
  return <>
    <Button ref={addRef} type="button" data-focus="add" disabled={addDisabled} onClick={onAdd}>{t('subjects.add')}</Button>
    <Dialog open={adding} onOpenChange={(open) => { if (!open) onCancelAdd(); }} title={t('subjects.add.formLabel')}
      closeLabel={t('subjects.cancel')} busy={createPending} sheet triggerRef={addRef} initialFocusRef={addNameRef}>
      {adding ? <SubjectForm key={String(adding)} label={t('subjects.add.formLabel')} initial={pendingInitial}
        submitLabel={t('subjects.add.submit')} pendingLabel={t('subjects.add.submitting')}
        locked={createLocked} retryLabel={t('subjects.create.checkAgain')}
        notice={createUncertain ? t('subjects.notice.uncertain') : undefined} initialFocusRef={addNameRef} onSubmit={onAddSubmit} onCancel={onCancelAdd}
        onPendingChange={setCreatePending} /> : null}
    </Dialog>
  </>;
}
