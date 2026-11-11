import { useId, useRef, type RefObject } from 'react';
import { Button } from '../../components/ui/button';
import { useI18n } from '../../i18n';
import type { Member } from '../members/membersApi';
import { ItemFormFields } from './ItemFormFields';
import type { ItemFormOutcome, ItemFormValues } from './itemFormModel';
import { errorMessage } from './itemFormModel';
import type { Subject } from './itemsApi';
import { useItemFormState } from './useItemFormState';

export type { ItemFormValues } from './itemFormModel';

type Mode = 'add' | 'edit';
type ItemFormProps = {
  subjects: Subject[];
  members: Member[];
  onSubmit: (values: ItemFormValues) => Promise<ItemFormOutcome>;
  onCancel: () => void;
  busy?: boolean;
  mode?: Mode;
  initialValues?: ItemFormValues;
  notice?: string;
  locked?: boolean;
  retryLabel?: string;
  submittingLabel?: string;
  preserveDraftOnUnlock?: boolean;
  initialFocusRef?: RefObject<HTMLElement | null>;
};

export function ItemForm(props: ItemFormProps) {
  const { subjects, members, onCancel, busy = false, mode = 'add', notice, locked = false, initialFocusRef } = props;
  const { t } = useI18n();
  const id = useId();
  const noticeRef = useRef<HTMLDivElement>(null);
  const initialTitleRef = useRef<HTMLInputElement>(null);
  const state = useItemFormState({
    id, subjects, onSubmit: props.onSubmit, busy, mode, initialValues: props.initialValues, notice, locked,
    preserveDraftOnUnlock: props.preserveDraftOnUnlock ?? false,
  });
  const errorId = `${id}-error`;
  const pendingOrLocked = state.pending || locked;
  return <form
    aria-label={t(mode === 'edit' ? 'itemDetail.edit.formLabel' : 'items.add.formLabel')} noValidate onSubmit={state.submit}
    className="space-y-5"
  >
    <ItemFormFields
      id={id} errorId={errorId} state={state} mode={mode} subjects={subjects} members={members}
      titleRef={initialTitleRef}
      onTitleRef={(node) => { if (node && !notice && initialFocusRef) initialFocusRef.current = node; }}
      disabled={pendingOrLocked || (mode === 'edit' && busy)} historyDisabled={pendingOrLocked}
    />
    {notice && state.showNotice
      ? <div ref={(node) => {
        noticeRef.current = node;
        if (node && state.showNotice && initialFocusRef) initialFocusRef.current = node;
      }} tabIndex={-1} role="status" className="rounded-xl border border-line bg-sand px-3 py-2 text-sm text-muted outline-none">{notice}</div>
      : null}
    {state.error ? <div id={errorId} role="alert" className="rounded-xl border border-danger bg-danger-bg px-4 py-3 text-danger">{errorMessage(state.error, t)}</div> : null}
    <FormActions {...props} busy={busy} mode={mode} locked={locked} pending={state.pending} onCancel={onCancel} />
  </form>;
}

type ActionProps = Pick<ItemFormProps, 'retryLabel' | 'submittingLabel' | 'onCancel'> & { busy: boolean; mode: Mode; locked: boolean; pending: boolean };

function FormActions({ busy, mode, locked, pending, onCancel, retryLabel, submittingLabel }: ActionProps) {
  const { t } = useI18n();
  const idleLabel = locked ? (retryLabel ?? t('items.add.submit')) : t(mode === 'edit' ? 'itemDetail.save' : 'items.add.submit');
  const label = pending ? (submittingLabel ?? t(mode === 'edit' ? 'itemDetail.saving' : 'items.add.submitting')) : idleLabel;
  return <div className="flex flex-wrap items-center gap-2 border-t border-line pt-5">
    <Button type="submit" disabled={pending || busy}>{label}</Button>
    <Button type="button" variant="quiet" onClick={onCancel} disabled={pending || (mode === 'edit' && busy)}>{t('items.cancel')}</Button>
  </div>;
}
