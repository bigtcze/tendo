import type { RefObject } from 'react';
import { Button } from '../../components/ui/button';
import { Field } from '../../components/ui/field';
import { Input } from '../../components/ui/input';
import { Select } from '../../components/ui/select';
import { Switch as Toggle } from '../../components/ui/switch';
import { Textarea } from '../../components/ui/textarea';
import { useI18n, type MessageKey } from '../../i18n';
import { unitWord } from './repeatSummary';
import type { Member } from '../members/membersApi';
import { units, type FormField, type IntervalUnit } from './itemFormModel';
import type { Subject } from './itemsApi';
import { controlId, type ItemFormState } from './useItemFormState';

type FieldProps = { id: string; errorId: string; state: ItemFormState; disabled: boolean };
type FormFieldsProps = FieldProps & {
  mode: 'add' | 'edit';
  subjects: Subject[];
  members: Member[];
  historyDisabled: boolean;
  titleRef?: RefObject<HTMLInputElement | null>;
  onTitleRef?: (node: HTMLInputElement | null) => void;
};

/**
 * aria-invalid plus descriptions: the field's own hint (if any) always, and the
 * form error only for the field the current error points at.
 */
function invalidProps(state: ItemFormState, errorId: string, field: FormField, hintIds?: string) {
  const invalid = state.field === field;
  const describedBy = [hintIds, invalid ? errorId : undefined].filter(Boolean).join(' ') || undefined;
  return { 'aria-invalid': invalid, 'aria-describedby': describedBy };
}

/** Hairline-separated form sections: the everyday fields, then repeat, then who, then the optional notes. */
export function ItemFormFields(props: FormFieldsProps) {
  const { state, mode, members } = props;
  const showResponsible = members.length > 1 || state.draft.responsibleUserId !== null;
  return <div className="divide-y divide-line">
    <div className="space-y-5 pb-5">
      <TitleField {...props} onTitleRef={props.onTitleRef} />
      <SubjectField {...props} />
      <DateField {...props} />
    </div>
    <div className="py-5">
      <RepeatSwitch {...props} />
      {state.draft.repeat ? <RecurrencePanel {...props} withHistory={mode === 'add'} /> : null}
    </div>
    {showResponsible ? <div className="py-5"><ResponsibleField {...props} /></div> : null}
    <div className="pt-5"><NotesField {...props} /></div>
  </div>;
}

type TitleFieldProps = FieldProps & Pick<FormFieldsProps, 'titleRef' | 'onTitleRef'>;

function TitleField({ id, state, disabled, titleRef, onTitleRef }: TitleFieldProps) {
  const { t } = useI18n();
  return <Field id={controlId(id, 'title')} label={t('items.title')}>
    {(field) => <Input
      ref={onTitleRef ?? titleRef}
      autoFocus className="text-lg" disabled={disabled} value={state.draft.title}
      data-item-title="true" placeholder={t('items.title.placeholder')}
      onChange={(event) => state.update({ title: event.target.value })} {...field} {...invalidProps(state, `${id}-error`, 'title')}
    />}
  </Field>;
}

function SubjectField({ id, errorId, state, disabled, subjects }: FieldProps & { subjects: Subject[] }) {
  const { t } = useI18n();
  return <Field id={controlId(id, 'subjectId')} label={t('items.subject')}>
    {(field) => <Select
      disabled={disabled} value={state.draft.subjectId}
      onChange={(event) => state.update({ subjectId: event.target.value })}
      {...field} {...invalidProps(state, errorId, 'subjectId')}
    >
      {subjects.map((subject) => <option key={subject.id} value={subject.id}>{subject.name}</option>)}
    </Select>}
  </Field>;
}

function DateField({ id, errorId, state, disabled }: FieldProps) {
  const { t, locale } = useI18n();
  return <Field id={controlId(id, 'attentionOn')} label={t('items.attentionDate')} hint={t('items.attentionDate.hint')}>
    {(field) => <Input disabled={disabled} type="date" lang={locale} value={state.draft.attentionOn}
      onChange={(event) => state.update({ attentionOn: event.target.value })} {...field}
      {...invalidProps(state, errorId, 'attentionOn', field['aria-describedby'])} />}
  </Field>;
}

type SwitchProps = { checked: boolean; disabled: boolean; onChange: (checked: boolean) => void; label: string; describedBy?: string };

function Switch({ checked, disabled, onChange, label, describedBy }: SwitchProps) {
  return <label className="flex min-h-11 items-center gap-3 text-ink">
    <Toggle checked={checked} disabled={disabled} onCheckedChange={onChange} aria-label={label} aria-describedby={describedBy} />
    <span className="font-semibold">{label}</span>
  </label>;
}

function RepeatSwitch({ state, disabled, mode }: FieldProps & { mode: 'add' | 'edit' }) {
  const { t } = useI18n();
  return <div>
    <Switch disabled={disabled} checked={state.draft.repeat} onChange={(repeat) => state.update({ repeat })} label={t('items.repeat')} />
    {mode === 'edit' ? <p className="mt-1 text-meta text-muted">{t('itemDetail.repeatEditHint')}</p> : null}
  </div>;
}

function IntervalFieldset({ id, errorId, state, disabled }: FieldProps) {
  const { t, locale } = useI18n();
  const labelId = `${controlId(id, 'recurrence')}-label`;
  const intervalId = controlId(id, 'recurrence');
  return <fieldset className="space-y-2">
    <legend id={labelId} className="text-sm font-semibold">{t('items.interval')}</legend>
    <div className="flex gap-3">
      <Input
        disabled={disabled} id={intervalId} aria-labelledby={labelId} className="w-24"
        type="number" min="1" max="999" value={state.draft.interval}
        onChange={(event) => state.update({ interval: event.target.value })} {...invalidProps(state, errorId, 'recurrence')}
      />
      <Select
        disabled={disabled} aria-label={t('items.intervalUnit')} className="min-w-0 flex-1"
        value={state.draft.unit}
        onChange={(event) => state.update({ unit: event.target.value as IntervalUnit })}
      >
        {units.map((value) => <option key={value} value={value}>{unitWord(t, locale, Number(state.draft.interval), value)}</option>)}
      </Select>
    </div>
  </fieldset>;
}

function intervalLabel(
  t: (key: MessageKey, params?: Record<string, string>) => string,
  locale: string,
  count: string,
  unit: IntervalUnit,
) {
  return `${count} ${unitWord(t, locale, Number(count), unit)}`;
}

function RecurrencePanel(props: FieldProps & { withHistory: boolean; historyDisabled: boolean }) {
  const { id, state, disabled, withHistory } = props;
  const { t, locale } = useI18n();
  const exampleId = `${id}-repeat-example`;
  return <div className="mt-4 space-y-4 rounded-xl border border-line bg-sand p-4">
    <IntervalFieldset {...props} />
    <div className="border-t border-line pt-4">
      <Switch disabled={disabled} checked={state.draft.fluid} onChange={(fluid) => state.update({ fluid })}
        label={t('items.repeat.afterCompletion')} describedBy={exampleId} />
      <p id={exampleId} className="mt-1 text-meta text-muted">
        {t(state.draft.fluid ? 'items.repeatExample.after' : 'items.repeatExample.fixed', {
          interval: intervalLabel(t, locale, state.draft.interval, state.draft.unit),
        })}
      </p>
    </div>
    {withHistory ? <HistoricalCompletion {...props} /> : null}
  </div>;
}

function HistoricalCompletion({ id, errorId, state, historyDisabled }: FieldProps & { historyDisabled: boolean }) {
  const { t, locale } = useI18n();
  return <div className="border-t border-line pt-3">
    <Button
      type="button" variant="quiet" size="small" disabled={historyDisabled}
      aria-expanded={state.historicalOpen} onClick={state.toggleHistorical}
    >
      {t(state.historicalOpen ? 'items.historical.hide' : 'items.historical.show')}
    </Button>
    {state.historicalOpen ? <div className="mt-2">
      <Field id={controlId(id, 'historicalCompletedOn')} label={t('items.historical.label')}
        hint={t('items.historical.hint')}>
        {(field) => <Input disabled={historyDisabled} type="date" lang={locale}
          value={state.draft.historicalCompletedOn}
          onChange={(event) => state.update({ historicalCompletedOn: event.target.value })}
          {...field} {...invalidProps(state, errorId, 'historicalCompletedOn', field['aria-describedby'])} />}
      </Field>
    </div> : null}
  </div>;
}

function ResponsibleField({ id, errorId, state, disabled, members }: FieldProps & { members: Member[] }) {
  const { t } = useI18n();
  const selected = state.draft.responsibleUserId;
  const unlisted = selected !== null && selected !== '' && !members.some((member) => member.userId === selected);
  return <Field id={controlId(id, 'responsibleUserId')} label={t('items.responsible.label')}
>
    {(field) => <Select
      disabled={disabled}
      value={selected ?? ''}
      onChange={(event) => state.update({ responsibleUserId: event.target.value || null })}
      {...field} {...invalidProps(state, errorId, 'responsibleUserId')}
    >
      <option value="">{t('items.responsible.none')}</option>
      {members.map((member) => <option key={member.userId} value={member.userId}>{member.login}</option>)}
      {unlisted ? <option value={selected}>{t('items.responsible.unknown')}</option> : null}
    </Select>}
  </Field>;
}

function NotesField({ id, errorId, state, disabled }: FieldProps) {
  const { t } = useI18n();
  return <div>
    <Button type="button" variant="quiet" size="small" disabled={disabled} aria-expanded={state.notesOpen} onClick={state.toggleNotes}
      className="-ml-3 min-h-11">
      {t(state.notesOpen ? 'items.notes.hide' : 'items.notes.show')}
    </Button>
    {state.notesOpen ? <div className="mt-2">
      <Field id={controlId(id, 'notes')} label={t('items.notes')}>
        {(field) => <Textarea disabled={disabled} value={state.draft.notes}
          onChange={(event) => state.update({ notes: event.target.value })} {...field} {...invalidProps(state, errorId, 'notes')} />}
      </Field>
    </div> : null}
  </div>;
}
