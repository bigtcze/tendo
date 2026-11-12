import type { RefObject } from 'react';
import { Button } from '../../components/ui/button';
import { useI18n } from '../../i18n';
import type { Member } from '../members/membersApi';
import { ItemForm, type ItemFormValues } from './ItemForm';
import type { Subject } from './itemsApi';
import type { SubjectState, SubmitOutcome } from './useAddItem';

type CatalogueStatusProps = {
  adding: boolean;
  subjectLoading: boolean;
  subjectState: SubjectState;
  editingRecovery: boolean;
  retryRef: RefObject<HTMLButtonElement | null>;
  onRetry: () => void;
  onCancel: () => void;
};

/** Catalogue loading and error states shown above the home overview while the add form opens. */
export function AddItemCatalogueStatus(props: CatalogueStatusProps) {
  const { adding, subjectLoading, subjectState, editingRecovery, retryRef, onRetry, onCancel } = props;
  const { t } = useI18n();
  return <>
    {adding && subjectLoading && !editingRecovery
      ? <p role="status" className="text-muted">{t('items.subjects.loading')}</p>
      : null}
    {adding && subjectState === 'error' && !editingRecovery ? <div className="space-y-3 rounded-xl border border-line bg-sand p-4">
      <p className="text-ink">{t('items.subjects.error')}</p>
      <div className="flex flex-wrap gap-2">
        <Button ref={retryRef} type="button" variant="secondary" disabled={subjectLoading} onClick={onRetry}>
          {t('app.error.retry')}
        </Button>
        <Button type="button" variant="quiet" onClick={onCancel}>{t('items.cancel')}</Button>
      </div>
    </div> : null}
  </>;
}

type AddItemPanelProps = {
  adding: boolean;
  busy: boolean;
  subjectLoading: boolean;
  subjectState: SubjectState;
  recovering: boolean;
  recoveryEditable: boolean;
  createUncertain: boolean;
  selectable: Subject[];
  recoverySubjects: Subject[];
  members: Member[];
  initialValues?: ItemFormValues;
  noSubjectsRef: RefObject<HTMLButtonElement | null>;
  initialFocusRef: RefObject<HTMLElement | null>;
  onSubmit: (values: ItemFormValues) => Promise<SubmitOutcome>;
  onCancel: () => void;
  onOpenPeople: () => void;
};

/** Add-item form, empty-catalogue prompt and loading cancel action. */
export function AddItemPanel(props: AddItemPanelProps) {
  const { adding, busy, subjectLoading, subjectState, recovering, recoveryEditable, createUncertain } = props;
  const { selectable, recoverySubjects, members, initialValues, noSubjectsRef, initialFocusRef, onSubmit, onCancel, onOpenPeople } = props;
  const { t } = useI18n();
  if (!adding) return null;
  const showForm = recovering || recoveryEditable || (subjectState === 'ready' && selectable.length > 0);
  const showEmpty = !recovering && !recoveryEditable && subjectState === 'ready' && selectable.length === 0;
  return <>
    {showForm ? <ItemForm
      busy={busy}
      members={members}
      subjects={recovering || recoveryEditable ? recoverySubjects : selectable}
      initialValues={initialValues}
      initialFocusRef={initialFocusRef}
      locked={recovering}
      preserveDraftOnUnlock={recoveryEditable}
      retryLabel={t('items.create.checkAgain')}
      submittingLabel={t('items.add.submitting')}
      notice={createUncertain ? t('items.create.uncertain') : undefined}
      onSubmit={onSubmit}
      onCancel={onCancel}
    /> : null}
    {showEmpty ? <div className="rounded-xl border border-line bg-sand p-5">
      <p className="text-ink">{t('items.noSubjects')}</p>
      <div className="mt-3 flex flex-wrap gap-2">
        <Button ref={noSubjectsRef} type="button" variant="secondary" onClick={onOpenPeople}>
          {t('home.people')}
        </Button>
        <Button type="button" variant="quiet" onClick={onCancel}>{t('items.cancel')}</Button>
      </div>
    </div> : null}
    {subjectLoading && !recovering
      ? <Button type="button" variant="quiet" className="mt-3" onClick={onCancel}>{t('items.cancel')}</Button>
      : null}
  </>;
}
