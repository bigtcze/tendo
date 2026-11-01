import { Button } from '../../components/ui/button';
import { Input } from '../../components/ui/input';
import { Label } from '../../components/ui/label';
import { useI18n, type MessageKey } from '../../i18n';
import type { Member } from '../members/membersApi';
import { units, type FormField, type IntervalUnit } from './itemFormModel';
import type { Subject } from './itemsApi';
import { controlId, type ItemFormState } from './useItemFormState';

type FieldProps = { id: string; errorId: string; state: ItemFormState; disabled: boolean };
type FormFieldsProps = FieldProps & { mode: 'add' | 'edit'; subjects: Subject[]; members: Member[]; historyDisabled: boolean };

/** aria-invalid plus the error description for the field the current error points at. */
function invalidProps(state: ItemFormState, errorId: string, field: FormField) {
  return { 'aria-invalid': state.field === field, 'aria-describedby': state.field === field ? errorId : undefined };
}

export function ItemFormFields(props: FormFieldsProps) {
  const { state, mode, members } = props;
  const showResponsible = members.length > 1 || state.draft.responsibleUserId !== null;
  return <>
    <TitleField {...props} />
    <SubjectField {...props} />
    <DateField {...props} />
    <RepeatSwitch {...props} />
    {state.draft.repeat ? <RecurrencePanel {...props} withHistory={mode === 'add'} /> : null}
    {showResponsible ? <ResponsibleField {...props} /> : null}
    <NotesField {...props} />
  </>;
}

function TitleField({ id, errorId, state, disabled }: FieldProps) {
  const { t } = useI18n();
  return <div className="space-y-2">
    <Label htmlFor={controlId(id, 'title')}>{t('items.title')}</Label>
    <Input
      disabled={disabled} id={controlId(id, 'title')} value={state.draft.title}
      onChange={(event) => state.update({ title: event.target.value })} {...invalidProps(state, errorId, 'title')}
    />
  </div>;
}

function SubjectField({ id, errorId, state, disabled, subjects }: FieldProps & { subjects: Subject[] }) {
  const { t } = useI18n();
  return <div className="space-y-2">
    <Label htmlFor={controlId(id, 'subjectId')}>{t('items.subject')}</Label>
    <select
      disabled={disabled} id={controlId(id, 'subjectId')} className="min-h-11 w-full rounded-xl border border-field bg-white px-3 text-base"
      value={state.draft.subjectId} onChange={(event) => state.update({ subjectId: event.target.value })}
      {...invalidProps(state, errorId, 'subjectId')}
    >
      {subjects.map((subject) => <option key={subject.id} value={subject.id}>{subject.name}</option>)}
    </select>
  </div>;
}

function DateField({ id, errorId, state, disabled }: FieldProps) {
  const { t, locale } = useI18n();
  return <div className="space-y-2">
    <Label htmlFor={controlId(id, 'attentionOn')}>{t('items.attentionDate')}</Label>
    <Input
      disabled={disabled} id={controlId(id, 'attentionOn')} type="date" lang={locale} value={state.draft.attentionOn}
      onChange={(event) => state.update({ attentionOn: event.target.value })} {...invalidProps(state, errorId, 'attentionOn')}
    />
  </div>;
}

function Switch({ checked, disabled, onChange, label }: { checked: boolean; disabled: boolean; onChange: (checked: boolean) => void; label: string }) {
  return <label className="switch-row flex min-h-12 items-center justify-between gap-4">
    <span className="text-base font-medium">{label}</span>
    <span className="switch-control">
      <input
        disabled={disabled} type="checkbox" role="switch" checked={checked}
        onChange={(event) => onChange(event.target.checked)}
      />
      <span aria-hidden="true" />
    </span>
  </label>;
}

function RepeatSwitch({ state, disabled, mode }: FieldProps & { mode: 'add' | 'edit' }) {
  const { t } = useI18n();
  return <div>
    <Switch disabled={disabled} checked={state.draft.repeat} onChange={(repeat) => state.update({ repeat })} label={t('items.repeat')} />
    {mode === 'edit' ? <p className="text-sm text-muted">{t('itemDetail.repeatEditHint')}</p> : null}
  </div>;
}

function IntervalFieldset({ id, errorId, state, disabled }: FieldProps) {
  const { t } = useI18n();
  const labelId = `${controlId(id, 'recurrence')}-label`;
  return <fieldset className="space-y-2">
    <legend id={labelId} className="text-sm font-medium">{t('items.interval')}</legend>
    <div className="flex gap-3">
      <Input
        disabled={disabled} id={controlId(id, 'recurrence')} aria-labelledby={labelId} className="w-24"
        type="number" min="1" max="999" value={state.draft.interval}
        onChange={(event) => state.update({ interval: event.target.value })} {...invalidProps(state, errorId, 'recurrence')}
      />
      <select
        disabled={disabled} aria-label={t('items.intervalUnit')} className="min-h-11 min-w-0 flex-1 rounded-xl border border-field bg-white px-3"
        value={state.draft.unit} onChange={(event) => state.update({ unit: event.target.value as IntervalUnit })}
      >
        {units.map((value) => <option key={value} value={value}>{t(`items.unit.${value}` as MessageKey)}</option>)}
      </select>
    </div>
  </fieldset>;
}

function RecurrencePanel(props: FieldProps & { withHistory: boolean; historyDisabled: boolean }) {
  const { state, disabled, withHistory } = props;
  const { t } = useI18n();
  return <div className="space-y-4 rounded-xl bg-sand/70 p-4">
    <IntervalFieldset {...props} />
    <Switch disabled={disabled} checked={state.draft.fluid} onChange={(fluid) => state.update({ fluid })} label={t('items.fluid')} />
    <p className="text-base text-muted"><span>{t(state.draft.fluid ? 'items.fluid.on' : 'items.fluid.off')}</span><span className="example-copy"> {t(state.draft.fluid ? 'items.fluid.exampleOn' : 'items.fluid.exampleOff')}</span></p>
    {withHistory ? <HistoricalCompletion {...props} /> : null}
  </div>;
}

function HistoricalCompletion({ id, errorId, state, historyDisabled }: FieldProps & { historyDisabled: boolean }) {
  const { t, locale } = useI18n();
  return <div className="border-t border-line/70 pt-2">
    <Button
      type="button" variant="quiet" size="small" disabled={historyDisabled}
      aria-expanded={state.historicalOpen} onClick={state.toggleHistorical}
    >
      {t(state.historicalOpen ? 'items.historical.hide' : 'items.historical.show')}
    </Button>
    {state.historicalOpen ? <div className="mt-2 space-y-2">
      <Label htmlFor={controlId(id, 'historicalCompletedOn')}>{t('items.historical.label')}</Label>
      <Input
        disabled={historyDisabled} id={controlId(id, 'historicalCompletedOn')} type="date" lang={locale}
        value={state.draft.historicalCompletedOn} onChange={(event) => state.update({ historicalCompletedOn: event.target.value })}
        {...invalidProps(state, errorId, 'historicalCompletedOn')}
      />
      <p className="text-sm text-muted">{t('items.historical.hint')}</p>
    </div> : null}
  </div>;
}

function ResponsibleField({ id, errorId, state, disabled, members }: FieldProps & { members: Member[] }) {
  const { t } = useI18n();
  const selected = state.draft.responsibleUserId;
  const unlisted = selected !== null && selected !== '' && !members.some((member) => member.userId === selected);
  return <div className="space-y-1.5">
    <Label className="text-sm text-muted" htmlFor={controlId(id, 'responsibleUserId')}>{t('items.responsible.label')}</Label>
    <select
      disabled={disabled} id={controlId(id, 'responsibleUserId')} className="min-h-11 w-full max-w-sm rounded-xl border border-field bg-sand/50 px-3 text-sm"
      value={selected ?? ''} onChange={(event) => state.update({ responsibleUserId: event.target.value || null })}
      {...invalidProps(state, errorId, 'responsibleUserId')}
    >
      <option value="">{t('items.responsible.none')}</option>
      {members.map((member) => <option key={member.userId} value={member.userId}>{member.login}</option>)}
      {unlisted ? <option value={selected}>{t('items.responsible.unknown')}</option> : null}
    </select>
  </div>;
}

function NotesField({ id, errorId, state, disabled }: FieldProps) {
  const { t } = useI18n();
  return <div>
    <Button type="button" variant="quiet" size="small" disabled={disabled} aria-expanded={state.notesOpen} onClick={state.toggleNotes}>
      {t(state.notesOpen ? 'items.notes.hide' : 'items.notes.show')}
    </Button>
    {state.notesOpen ? <div className="mt-2 space-y-2">
      <Label htmlFor={controlId(id, 'notes')}>{t('items.notes')}</Label>
      <textarea
        disabled={disabled} id={controlId(id, 'notes')} value={state.draft.notes}
        onChange={(event) => state.update({ notes: event.target.value })} className="min-h-24 w-full rounded-xl border border-field bg-white p-3"
        {...invalidProps(state, errorId, 'notes')}
      />
    </div> : null}
  </div>;
}
