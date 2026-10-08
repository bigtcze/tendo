import { useEffect, useId, useRef, useState, type FormEvent } from 'react';
import { Button } from '../../components/ui/button';
import { Input } from '../../components/ui/input';
import { Label } from '../../components/ui/label';
import { useI18n, type MessageKey } from '../../i18n';
import type { Invalid, Recurrence, Subject } from './itemsApi';

export type ItemFormValues = { title: string; subjectId: string; attentionOn: string; notes: string; recurrence: Recurrence | null };
type Values = ItemFormValues;
type Outcome = { kind: 'done' | 'handled' | 'failed' } | Invalid;
type FormError = Invalid | { kind: 'empty' } | { kind: 'failed' };
const units = ['day', 'week', 'month', 'year'] as const;

function errorMessage(error: FormError | null, t: (key: MessageKey) => string): string {
  if (!error) return '';
  if (error.kind === 'empty') return t('items.error.titleEmpty');
  if (error.kind === 'failed') return t('items.error.save');
  if (error.field === 'title') return error.code === 'invalid_length' ? t('items.error.titleLength') : t('items.error.titleInvalid');
  if (error.field === 'subjectId') return t('items.error.subject');
  if (error.field === 'attentionOn') return t('items.error.date');
  if (error.field === 'notes') return t('items.error.notes');
  return t('items.error.interval');
}

export function ItemForm({ subjects, onSubmit, onCancel, busy = false, mode = 'add', initialValues, notice }: { subjects: Subject[]; onSubmit: (values: Values) => Promise<Outcome>; onCancel: () => void; busy?: boolean; mode?: 'add' | 'edit'; initialValues?: Values; notice?: string }) {
  const { t, locale } = useI18n();
  const id = useId();
  const titleRef = useRef<HTMLInputElement>(null);
  const notesRef = useRef<HTMLTextAreaElement>(null);
  const submitting = useRef(false);
  const [title, setTitle] = useState(initialValues?.title ?? '');
  const [subjectId, setSubjectId] = useState(initialValues?.subjectId ?? subjects[0]?.id ?? '');
  const [attentionOn, setAttentionOn] = useState(initialValues?.attentionOn ?? '');
  const [notes, setNotes] = useState(initialValues?.notes ?? '');
  const [notesOpen, setNotesOpen] = useState(Boolean(initialValues?.notes));
  const [repeat, setRepeat] = useState(Boolean(initialValues?.recurrence));
  const [fluid, setFluid] = useState(initialValues?.recurrence?.mode === 'after_completion');
  const [interval, setInterval] = useState(String(initialValues?.recurrence?.intervalValue ?? 1));
  const [unit, setUnit] = useState<(typeof units)[number]>(initialValues?.recurrence?.intervalUnit ?? 'year');
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<FormError | null>(null);
  const errorId = `${id}-error`;
  const field = error && 'field' in error ? error.field : error?.kind === 'empty' ? 'title' : null;
  useEffect(() => { titleRef.current?.focus(); }, []);

  function fail(next: FormError) {
    setError(next);
    const target = 'field' in next ? next.field : next.kind === 'empty' ? 'title' : null;
    if (target === 'notes') setNotesOpen(true);
    setTimeout(() => {
      if (target === 'title') titleRef.current?.focus();
      if (target === 'subjectId') document.getElementById(`${id}-subject`)?.focus();
      if (target === 'attentionOn') document.getElementById(`${id}-date`)?.focus();
      if (target === 'recurrence') document.getElementById(`${id}-interval`)?.focus();
      if (target === 'notes') notesRef.current?.focus();
    }, 0);
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (submitting.current || busy) return;
    if (!title.trim()) return fail({ kind: 'empty' });
    if (!subjectId) return fail({ kind: 'invalid', field: 'subjectId', code: 'invalid_reference' });
    if (repeat && (!Number.isInteger(Number(interval)) || Number(interval) < 1 || Number(interval) > 999)) return fail({ kind: 'invalid', field: 'recurrence', code: 'invalid_interval' });
    submitting.current = true;
    setPending(true);
    setError(null);
    let result: Outcome;
    try {
      result = await onSubmit({ title, subjectId, attentionOn, notes, recurrence: repeat ? { intervalValue: Number(interval), intervalUnit: unit, mode: fluid ? 'after_completion' : 'fixed' } : null });
    } catch { result = { kind: 'failed' }; }
    submitting.current = false;
    setPending(false);
    if (result.kind === 'invalid') fail(result);
    else if (result.kind === 'failed') fail({ kind: 'failed' });
  }

  return <form aria-label={t(mode === 'edit' ? 'itemDetail.edit.formLabel' : 'items.add.formLabel')} noValidate onSubmit={submit} className="space-y-5 rounded-2xl border border-line bg-white/70 p-4 sm:p-6">
    <div className="space-y-2"><Label htmlFor={`${id}-title`}>{t('items.title')}</Label><Input disabled={pending || (mode === 'edit' && busy)} ref={titleRef} id={`${id}-title`} value={title} onChange={(event) => setTitle(event.target.value)} aria-invalid={field === 'title'} aria-describedby={field === 'title' ? errorId : undefined} /></div>
    <div className="space-y-2"><Label htmlFor={`${id}-subject`}>{t('items.subject')}</Label><select disabled={pending || (mode === 'edit' && busy)} id={`${id}-subject`} className="min-h-11 w-full rounded-xl border border-line bg-white px-3 text-base" value={subjectId} onChange={(event) => setSubjectId(event.target.value)} aria-invalid={field === 'subjectId'} aria-describedby={field === 'subjectId' ? errorId : undefined}>{subjects.map((subject) => <option key={subject.id} value={subject.id}>{subject.name}</option>)}</select></div>
    <div className="space-y-2"><Label htmlFor={`${id}-date`}>{t('items.attentionDate')}</Label><Input disabled={pending || (mode === 'edit' && busy)} id={`${id}-date`} type="date" lang={locale} value={attentionOn} onChange={(event) => setAttentionOn(event.target.value)} aria-invalid={field === 'attentionOn'} aria-describedby={field === 'attentionOn' ? errorId : undefined} /></div>
    <div><label className="flex min-h-11 items-center gap-3"><input disabled={pending || (mode === 'edit' && busy)} type="checkbox" role="switch" checked={repeat} onChange={(event) => setRepeat(event.target.checked)} className="size-5 accent-accent" />{t('items.repeat')}</label>{mode === 'edit' ? <p className="text-sm text-muted">{t('itemDetail.repeatEditHint')}</p> : null}</div>
    {repeat ? <div className="space-y-4 rounded-xl bg-sand/70 p-4">
      <fieldset className="space-y-2"><legend id={`${id}-interval-label`} className="text-sm font-medium">{t('items.interval')}</legend><div className="flex gap-3">
        <Input disabled={pending || (mode === 'edit' && busy)} id={`${id}-interval`} aria-labelledby={`${id}-interval-label`} className="w-24" type="number" min="1" max="999" value={interval} onChange={(event) => setInterval(event.target.value)} aria-invalid={field === 'recurrence'} aria-describedby={field === 'recurrence' ? errorId : undefined} />
        <select disabled={pending || (mode === 'edit' && busy)} aria-label={t('items.intervalUnit')} className="min-h-11 min-w-0 flex-1 rounded-xl border border-line bg-white px-3" value={unit} onChange={(event) => setUnit(event.target.value as typeof unit)}>{units.map((value) => <option key={value} value={value}>{t(`items.unit.${value}` as MessageKey)}</option>)}</select>
      </div></fieldset>
      <label className="flex min-h-11 items-center gap-3"><input disabled={pending || (mode === 'edit' && busy)} type="checkbox" role="switch" checked={fluid} onChange={(event) => setFluid(event.target.checked)} className="size-5 accent-accent" />{t('items.fluid')}</label>
      <p className="text-sm text-muted">{t(fluid ? 'items.fluid.on' : 'items.fluid.off')}</p>
    </div> : null}
    <div>
      <Button type="button" variant="quiet" size="small" disabled={pending || (mode === 'edit' && busy)} aria-expanded={notesOpen} onClick={() => setNotesOpen((open) => !open)}>{t(notesOpen ? 'items.notes.hide' : 'items.notes.show')}</Button>
      {notesOpen ? <div className="mt-2 space-y-2"><Label htmlFor={`${id}-notes`}>{t('items.notes')}</Label><textarea disabled={pending || (mode === 'edit' && busy)} ref={notesRef} id={`${id}-notes`} value={notes} onChange={(event) => setNotes(event.target.value)} maxLength={4000} aria-invalid={field === 'notes'} aria-describedby={field === 'notes' ? errorId : undefined} className="min-h-24 w-full rounded-xl border border-line bg-white p-3" /></div> : null}
    </div>
    {notice ? <p role="status" className="rounded-xl bg-sand px-3 py-2 text-sm text-muted">{notice}</p> : null}
    <div id={errorId} role="alert" className="text-danger">{errorMessage(error, t)}</div>
    <div className="flex flex-wrap gap-2"><Button type="submit" disabled={pending || busy}>{pending ? t(mode === 'edit' ? 'itemDetail.saving' : 'items.add.submitting') : t(mode === 'edit' ? 'itemDetail.save' : 'items.add.submit')}</Button><Button type="button" variant="quiet" onClick={onCancel} disabled={pending || (mode === 'edit' && busy)}>{t('items.cancel')}</Button></div>
  </form>;
}
