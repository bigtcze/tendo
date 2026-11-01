import { useEffect, useRef, type ReactNode, type RefObject } from 'react';
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

/** Catalogue loading and error states shown inside the add dialog while the form opens. */
export function AddItemCatalogueStatus(props: CatalogueStatusProps) {
  const { adding, subjectLoading, subjectState, editingRecovery, retryRef, onRetry, onCancel } = props;
  const { t } = useI18n();
  return <>
    {adding && subjectLoading && !editingRecovery
      ? <p role="status" className="text-muted">{t('items.subjects.loading')}</p>
      : null}
    {adding && subjectState === 'error' && !editingRecovery ? <div className="space-y-3">
      <p className="text-muted">{t('items.subjects.error')}</p>
      <Button ref={retryRef} type="button" variant="quiet" disabled={subjectLoading} onClick={onRetry}>
        {t('app.error.retry')}
      </Button>
      <Button type="button" variant="quiet" onClick={onCancel}>{t('items.cancel')}</Button>
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
  onSubmit: (values: ItemFormValues) => Promise<SubmitOutcome>;
  onCancel: () => void;
  onOpenPeople: () => void;
  /** Catalogue loading/error content; rendered inside the dialog so it is never covered by the backdrop. */
  catalogueStatus?: ReactNode;
};

/** Add-item form, empty-catalogue prompt and loading cancel action. */
export function AddItemPanel(props: AddItemPanelProps) {
  const { adding, busy, subjectLoading, subjectState, recovering, recoveryEditable, createUncertain } = props;
  const { selectable, recoverySubjects, members, initialValues, noSubjectsRef, onSubmit, onCancel, onOpenPeople, catalogueStatus } = props;
  const { t } = useI18n();
  const dialogRef = useRef<HTMLDivElement>(null);
  const previousFocus = useRef<HTMLElement | null>(null);
  const wasOpen = useRef(false);
  useEffect(() => {
    if (!adding) { if (wasOpen.current) { wasOpen.current = false; previousFocus.current?.focus(); } return; }
    if (wasOpen.current) return;
    wasOpen.current = true;
    previousFocus.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const dialog = dialogRef.current;
    const first = dialog?.querySelector<HTMLElement>('input:not([disabled]), select:not([disabled]), button:not([disabled])');
    first?.focus();
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') { event.preventDefault(); onCancel(); return; }
      if (event.key !== 'Tab' || !dialog) return;
      const controls = [...dialog.querySelectorAll<HTMLElement>('button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [href], [tabindex]:not([tabindex="-1"])')];
      if (!controls.length) return;
      const index = controls.indexOf(document.activeElement as HTMLElement);
      if (event.shiftKey && index <= 0) { event.preventDefault(); controls.at(-1)?.focus(); }
      else if (!event.shiftKey && index === controls.length - 1) { event.preventDefault(); controls[0]?.focus(); }
    }
    document.addEventListener('keydown', onKeyDown);
    return () => { document.removeEventListener('keydown', onKeyDown); };
  }, [adding, onCancel]);
  if (!adding) return null;
  const showForm = recovering || recoveryEditable || (subjectState === 'ready' && selectable.length > 0);
  const showEmpty = !recovering && !recoveryEditable && subjectState === 'ready' && selectable.length === 0;
  return <div className="add-sheet-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) onCancel(); }}>
    <div ref={dialogRef} role="dialog" aria-modal="true" aria-label={t('items.add.formLabel')} className="add-sheet" onMouseDown={(event) => event.stopPropagation()}>
    {catalogueStatus}
    {showForm ? <ItemForm
      busy={busy}
      members={members}
      subjects={recovering || recoveryEditable ? recoverySubjects : selectable}
      initialValues={initialValues}
      locked={recovering}
      preserveDraftOnUnlock={recoveryEditable}
      retryLabel={t('items.create.checkAgain')}
      submittingLabel={t('items.add.submitting')}
      notice={createUncertain ? t('items.create.uncertain') : undefined}
      onSubmit={onSubmit}
      onCancel={onCancel}
    /> : null}
    {showEmpty ? <div className="rounded-2xl border border-line bg-white/70 p-5">
      <p>{t('items.noSubjects')}</p>
      <Button ref={noSubjectsRef} type="button" variant="quiet" className="mt-3" onClick={onOpenPeople}>
        {t('home.people')}
      </Button>
      <Button type="button" variant="quiet" className="mt-3" onClick={onCancel}>{t('items.cancel')}</Button>
    </div> : null}
    {subjectLoading && !recovering
      ? <Button type="button" variant="quiet" className="mt-3" onClick={onCancel}>{t('items.cancel')}</Button>
      : null}
    </div>
  </div>;
}
