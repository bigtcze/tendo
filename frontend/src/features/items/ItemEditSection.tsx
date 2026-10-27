import type { Translate } from '../../i18n';
import type { Member } from '../members/membersApi';
import type { Subject } from './itemsApi';
import { ItemForm, type ItemFormValues } from './ItemForm';
import { ItemEditConflictSummary } from './ItemEditConflictSummary';
import { formValues, type Editing } from './itemEditModel';
import type { SaveOutcome } from './itemEditSave';

export function ItemEditSection({ editing, subjects, members, busy, locale, t, onSubmit, onCancel }: {
  editing: Editing; subjects: Subject[]; members: Member[]; busy: boolean; locale: string; t: Translate;
  onSubmit: (values: ItemFormValues) => Promise<SaveOutcome>; onCancel: () => void;
}) {
  const conflict = editing.conflict && editing.latest
    ? <ItemEditConflictSummary latest={editing.latest} mine={editing.mine} subjects={subjects} members={members} locale={locale} t={t} />
    : null;
  return <div className="mt-6 space-y-4">
    {conflict}
    <ItemForm
      key={`${editing.baseline.id}:${editing.etag}`} mode="edit"
      initialValues={editing.conflict ? editing.mine : formValues(editing.baseline)}
      members={members} subjects={subjects} busy={busy}
      notice={editing.conflict ? t('itemDetail.edit.conflictHint') : undefined}
      onSubmit={onSubmit} onCancel={onCancel}
    />
  </div>;
}
