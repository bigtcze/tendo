import { useCallback, useEffect, useRef, useState } from 'react';
import { AccountActions } from '../../app/AccountActions';
import { Heading } from '../../app/Heading';
import { Layout } from '../../app/Layout';
import { NavLink } from '../../app/NavLink';
import { Button } from '../../components/ui/button';
import { useI18n, type MessageKey } from '../../i18n';
import { getSubject } from '../subjects/subjectsApi';
import { listCompletions, getItem, changeItem, patchItem, listActiveSubjects, type Completion, type Item, type Recurrence, type Subject, type ItemField } from './itemsApi';
import { ItemForm, type ItemFormValues } from './ItemForm';

const MAX_HISTORY_PAGES = 1000;
const MAX_SUBJECT_PAGES = 20;
type Editing = { etag: string; baseline: Item; conflict: boolean; latest?: Item; mine?: ItemFormValues };
function recurrenceEqual(a: Recurrence | null, b: Recurrence | null) {
  return a === null || b === null ? a === b : a.intervalValue === b.intervalValue && a.intervalUnit === b.intervalUnit && a.mode === b.mode;
}
function normalizeNotes(value: string | null | undefined): string | null { return value?.trim() ? value : null; }
function sameValue(field: keyof ItemFormValues, a: ItemFormValues[keyof ItemFormValues], b: ItemFormValues[keyof ItemFormValues]) {
  if (field === 'recurrence') return recurrenceEqual(a as Recurrence | null, b as Recurrence | null);
  if (field === 'notes') return normalizeNotes(a as string) === normalizeNotes(b as string);
  return a === b;
}
function formValues(item: Item): ItemFormValues { return { title: item.title, subjectId: item.subjectId, attentionOn: item.attentionOn ?? '', notes: item.notes ?? '', recurrence: item.recurrence }; }
function formatDate(value: string, locale: string) {
  const [year, month, day] = value.split('-').map(Number);
  return new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeZone: 'UTC' }).format(new Date(Date.UTC(year, month - 1, day)));
}
function pluralKey(unit: Recurrence['intervalUnit'], count: number, locale: string): MessageKey {
  const category = new Intl.PluralRules(locale).select(count);
  const supported: Record<string, readonly string[]> = { en: ['one', 'other'], cs: ['one', 'few', 'other'] };
  return `items.every.${unit}.${supported[locale]?.includes(category) ? category : 'other'}` as MessageKey;
}
type LoadState = 'loading' | 'error' | 'gone' | 'ready';
export function ItemDetailScreen({ householdId, itemId, login, onBack, onSignedOut }: { householdId: string; itemId: string; login: string; onBack: () => void; onSignedOut: () => void }) {
  const { t, locale } = useI18n();
  const [state, setState] = useState<LoadState>('loading');
  const [item, setItem] = useState<Item | null>(null);
  const [subjectName, setSubjectName] = useState('');
  const [subjectOverride, setSubjectOverride] = useState<string | null>(null);
  const [history, setHistory] = useState<Completion[]>([]);
  const [historyState, setHistoryState] = useState<'loading' | 'ready' | 'error'>('loading');
  const [historyPartial, setHistoryPartial] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState<MessageKey | null>(null);
  const [undoArchive, setUndoArchive] = useState(false);
  const [editing, setEditing] = useState<Editing | null>(null);
  const [editSubjects, setEditSubjects] = useState<Subject[]>([]);
  const [editLoading, setEditLoading] = useState(false);
  const editButtonRef = useRef<HTMLButtonElement>(null);
  const returnEditFocus = useRef(false);
  const active = useRef(false);
  const generation = useRef(0);
  const busyRef = useRef(false);
  const busyOwner = useRef<symbol | null>(null);
  const focusRef = useRef<HTMLParagraphElement>(null);
  const focusAfterMutation = useRef(false);

  useEffect(() => {
    active.current = true;
    return () => { active.current = false; };
  }, []);
  const current = (token: number) => active.current && generation.current === token;

  const loadHistory = useCallback(async (token: number) => {
    setHistoryState('loading'); setHistoryPartial(false);
    const all: Completion[] = [];
    let cursor: string | undefined;
    for (let page = 0; page < MAX_HISTORY_PAGES; page++) {
      const result = await listCompletions(householdId, itemId, cursor);
      if (!current(token)) return;
      if (result.kind === 'unauthenticated') { onSignedOut(); return; }
      if (result.kind !== 'ok') { setHistoryState('error'); return; }
      all.push(...result.completions);
      if (!result.nextCursor) { setHistory(all.reverse()); setHistoryState('ready'); return; }
      cursor = result.nextCursor;
    }
    setHistory(all.reverse()); setHistoryPartial(true); setHistoryState('ready');
  }, [householdId, itemId, onSignedOut]);

  const load = useCallback(async (token: number, preserveArchiveNotice = false) => {
    if (!/^[0-9a-f-]{36}$/i.test(itemId)) { if (current(token)) setState('gone'); return; }
    setState('loading'); setHistory([]); setHistoryState('loading'); setEditing(null); setEditLoading(false);
    if (!preserveArchiveNotice) { setNotice(null); setUndoArchive(false); }
    const result = await getItem(householdId, itemId);
    if (!current(token)) return;
    if (result.kind === 'unauthenticated') { onSignedOut(); return; }
    if (result.kind === 'notFound') { setState('gone'); return; }
    if (result.kind !== 'ok') { setState('error'); return; }
    setItem(result.item); setState('ready'); setSubjectOverride(null); setSubjectName('');
    const subject = await getSubject(householdId, result.item.subjectId);
    if (!current(token)) return;
    if (subject.kind === 'unauthenticated') { onSignedOut(); return; }
    setSubjectName(subject.kind === 'ok' ? subject.subject.name : '');
    void loadHistory(token);
  }, [householdId, itemId, loadHistory, onSignedOut]);

  useEffect(() => {
    const token = ++generation.current;
    void Promise.resolve().then(() => load(token));
    return () => { if (generation.current === token) generation.current = token + 1; };
  }, [load, attempt]);
  useEffect(() => { if (focusAfterMutation.current && notice) { focusRef.current?.focus(); focusAfterMutation.current = false; } }, [notice]);
  useEffect(() => { if (notice === 'itemDetail.updated') { focusRef.current?.focus(); returnEditFocus.current = false; } }, [notice]);
  useEffect(() => { if (!editing && !editLoading && !busy && returnEditFocus.current) { editButtonRef.current?.focus(); returnEditFocus.current = false; } }, [editing, editLoading, busy]);

  async function beginEdit() {
    if (busyRef.current || editLoading) return;
    const owner = Symbol('edit-load');
    busyOwner.current = owner; busyRef.current = true; setBusy(true);
    const token = generation.current;
    setNotice(null); setEditLoading(true);
    try {
      const result = await getItem(householdId, itemId);
      if (!current(token)) return;
      if (result.kind === 'unauthenticated') { onSignedOut(); return; }
      if (result.kind === 'notFound') { setState('gone'); return; }
      if (result.kind !== 'ok') { setNotice('itemDetail.actionFailed'); return; }
      const subjects: Subject[] = [];
      let cursor: string | undefined;
      for (let page = 0; page < MAX_SUBJECT_PAGES; page++) {
        const listed = await listActiveSubjects(householdId, false, cursor);
        if (!current(token)) return;
        if (listed.kind === 'unauthenticated') { onSignedOut(); return; }
        if (listed.kind !== 'ok') { setNotice('items.subjects.error'); return; }
        subjects.push(...listed.subjects);
        if (!listed.nextCursor) break;
        cursor = listed.nextCursor;
      }
      if (!subjects.some((subject) => subject.id === result.item.subjectId)) {
        const selected = await getSubject(householdId, result.item.subjectId);
        if (!current(token)) return;
        if (selected.kind === 'unauthenticated') { onSignedOut(); return; }
        if (selected.kind === 'ok') subjects.push(selected.subject);
        else if (selected.kind === 'notFound') { setState('gone'); return; }
        else { setNotice('itemDetail.actionFailed'); return; }
      }
      const selected = subjects.find((subject) => subject.id === result.item.subjectId);
      setItem(result.item); setEditSubjects(subjects); setEditing({ etag: result.etag, baseline: result.item, conflict: false });
      if (selected) { setSubjectName(selected.name); setSubjectOverride(selected.name); }
    } finally {
      if (busyOwner.current === owner) { busyOwner.current = null; busyRef.current = false; setBusy(false); }
      if (current(token)) setEditLoading(false);
    }
  }

  async function saveEdit(values: ItemFormValues) {
    if (!editing || !item) return { kind: 'handled' as const };
    const original = editing.baseline;
    const recurrence: Recurrence | null = values.recurrence;
    const body = {
      ...(values.title !== original.title ? { title: values.title } : {}),
      ...(values.subjectId !== original.subjectId ? { subjectId: values.subjectId } : {}),
      ...(values.attentionOn !== (original.attentionOn ?? '') ? { attentionOn: values.attentionOn || null } : {}),
      ...(normalizeNotes(values.notes) !== normalizeNotes(original.notes) ? { notes: normalizeNotes(values.notes) } : {}),
      ...(!recurrenceEqual(recurrence, original.recurrence) ? { recurrence } : {}),
    };
    if (!Object.keys(body).length) { returnEditFocus.current = true; setEditing(null); return { kind: 'done' as const }; }
    if (busyRef.current) return { kind: 'handled' as const };
    const owner = Symbol('edit-save'); busyOwner.current = owner;
    busyRef.current = true; setBusy(true);
    const token = generation.current;
    try {
      const result = await patchItem(householdId, itemId, editing.etag, body);
      if (!current(token)) return { kind: 'handled' as const };
      if (result.kind === 'unauthenticated') { onSignedOut(); return { kind: 'handled' as const }; }
      if (result.kind === 'notFound') { setState('gone'); setEditing(null); return { kind: 'handled' as const }; }
      if (result.kind === 'changed') {
        const latest = await getItem(householdId, itemId);
        if (!current(token)) return { kind: 'handled' as const };
        if (latest.kind === 'unauthenticated') { onSignedOut(); return { kind: 'handled' as const }; }
        if (latest.kind === 'notFound') { setState('gone'); setEditing(null); return { kind: 'handled' as const }; }
        if (latest.kind === 'ok') {
          const before = formValues(original); const theirs = formValues(latest.item);
          const rebased = Object.fromEntries((Object.keys(before) as (keyof ItemFormValues)[]).map((field) => [field, sameValue(field, values[field], before[field]) ? theirs[field] : values[field]])) as ItemFormValues;
          const latestSubject = await getSubject(householdId, latest.item.subjectId);
          if (!current(token)) return { kind: 'handled' as const };
          if (latestSubject.kind === 'unauthenticated') { onSignedOut(); return { kind: 'handled' as const }; }
          const cachedSubject = editSubjects.find((subject) => subject.id === latest.item.subjectId);
          const safeSubjectName = cachedSubject?.name ?? t('items.subject.unknown');
          setSubjectName(safeSubjectName); setSubjectOverride(safeSubjectName);
          if (latestSubject.kind === 'ok') { setSubjectName(latestSubject.subject.name); setSubjectOverride(latestSubject.subject.name); }
          setItem(latest.item); setEditing({ etag: latest.etag, baseline: latest.item, conflict: true, latest: latest.item, mine: rebased });
          setEditSubjects((current) => latestSubject.kind === 'ok' && !current.some((subject) => subject.id === latestSubject.subject.id) ? [...current, latestSubject.subject] : current);
          return { kind: 'handled' as const };
        }
        return { kind: 'failed' as const };
      }
      if (result.kind === 'invalid') {
        const allowed = ['title', 'subjectId', 'attentionOn', 'notes', 'recurrence'];
        return allowed.includes(result.field) ? { kind: 'invalid' as const, field: result.field as ItemField, code: result.code } : { kind: 'failed' as const };
      }
      if (result.kind === 'ok') {
        const cachedSubject = editSubjects.find((subject) => subject.id === result.item.subjectId);
        const safeSubjectName = cachedSubject?.name ?? t('items.subject.unknown');
        setSubjectName(safeSubjectName); setSubjectOverride(safeSubjectName);
        setItem(result.item); setEditing(null); setNotice('itemDetail.updated'); focusAfterMutation.current = true;
        const savedSubject = await getSubject(householdId, result.item.subjectId);
        if (!current(token)) return { kind: 'handled' as const };
        if (savedSubject.kind === 'unauthenticated') { onSignedOut(); return { kind: 'handled' as const }; }
        if (savedSubject.kind === 'ok') { setSubjectName(savedSubject.subject.name); setSubjectOverride(savedSubject.subject.name); }
        return { kind: 'done' as const };
      }
      return { kind: 'failed' as const };
    } finally { if (busyOwner.current === owner) { busyOwner.current = null; busyRef.current = false; setBusy(false); } }
  }

  async function mutate(body: { workflowState?: 'open' | 'in_progress' | 'waiting' | 'paused'; archived?: boolean }, archiveAction = false) {
    if (busyRef.current) return;
    const owner = Symbol('workflow-mutation'); busyOwner.current = owner;
    busyRef.current = true; setBusy(true);
    if (!(archiveAction && undoArchive)) setNotice(null);
    const token = generation.current;
    try {
      const result = await changeItem(householdId, itemId, body);
      if (!current(token)) return;
      if (result.kind === 'unauthenticated') { onSignedOut(); return; }
      if (result.kind === 'notFound') { setState('gone'); return; }
      if (result.kind === 'changed') {
        const next = ++generation.current;
        await load(next, archiveAction && undoArchive);
        if (current(next)) { setNotice('itemDetail.changed'); focusAfterMutation.current = true; }
        return;
      }
      if (result.kind === 'ok') {
        setItem(result.item);
        if (archiveAction) { setNotice(body.archived ? 'itemDetail.archivedNotice' : 'itemDetail.restored'); setUndoArchive(Boolean(body.archived)); }
        else setNotice('itemDetail.updated');
        focusAfterMutation.current = true;
        return;
      }
      setNotice(result.kind === 'invalid' ? 'itemDetail.invalid' : 'itemDetail.actionFailed');
      focusAfterMutation.current = true;
    } finally {
      if (busyOwner.current === owner) { busyOwner.current = null; busyRef.current = false; setBusy(false); }
    }
  }

  const backLink = <NavLink href="/" onNavigate={onBack} className="-ml-3 inline-flex min-h-11 items-center rounded-xl px-3 text-muted hover:bg-sand hover:text-ink focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent">{t('subjects.backHome')}</NavLink>;
  let status: MessageKey = 'items.group.needs';
  if (item?.done) status = 'itemDetail.state.done';
  else if (item?.workflowState === 'in_progress') status = 'itemDetail.state.progress';
  else if (item?.workflowState === 'waiting') status = 'itemDetail.state.waiting';
  else if (item?.workflowState === 'paused') status = 'itemDetail.state.paused';
  else if (item?.attention === 'upcoming') status = 'items.group.upcoming';

  return <Layout actions={<AccountActions login={login} onSignedOut={onSignedOut} />}><section className="settle">
    {backLink}
    {state === 'loading' ? <p role="status" className="mt-6 text-muted">{t('items.loading')}</p> : null}
    {state === 'error' ? <div className="mt-6 space-y-3"><p className="text-muted">{t('app.error.body')}</p><Button type="button" onClick={() => setAttempt((n) => n + 1)}>{t('app.error.retry')}</Button></div> : null}
    {state === 'gone' ? <div className="mt-6 space-y-3"><Heading className="font-display text-3xl">{t('itemDetail.gone')}</Heading><p className="text-muted">{t('itemDetail.goneBody')}</p></div> : null}
    {state === 'ready' && item ? <>
      <Heading className="mt-2 min-w-0 break-words font-display text-3xl leading-tight sm:text-4xl">{item.title}</Heading>
      <p className="mt-2 text-muted">{subjectOverride ?? (subjectName || t('items.subject.unknown'))}</p>
      {item.archived ? <p className="mt-5 rounded-xl bg-sand px-4 py-3">{t('itemDetail.archived')}</p> : null}
      {notice ? <div className="mt-4 rounded-xl bg-sand px-4 py-3"><p ref={focusRef} tabIndex={-1} role="status" aria-live="polite" className="outline-none">{t(notice)}</p>{undoArchive ? <Button type="button" variant="quiet" className="mt-1" disabled={busy} onClick={() => void mutate({ archived: false }, true)}>{t('subjects.undo')}</Button> : null}</div> : null}
      {!editing ? <div className="mt-6 space-y-5 rounded-2xl border border-line bg-white/70 p-5 sm:p-7">
        <p className="font-medium">{t(status)}</p>
        {item.attentionOn ? <p className="text-muted">{t(item.attention === 'upcoming' ? 'itemDetail.nextAttention' : 'itemDetail.attentionDate', { date: formatDate(item.attentionOn, locale) })}</p> : null}
        {item.lastCompletedOn ? <p className="text-muted">{t('itemDetail.lastDone', { date: formatDate(item.lastCompletedOn, locale) })}</p> : null}
        {item.recurrence ? <div><p>{t(pluralKey(item.recurrence.intervalUnit, item.recurrence.intervalValue, locale), { count: String(item.recurrence.intervalValue) })}</p><p className="mt-1 text-sm text-muted">{t(item.recurrence.mode === 'after_completion' ? 'itemDetail.repeat.fluid' : 'itemDetail.repeat.fixed')}</p></div> : null}
        {item.notes ? <div className="border-t border-line pt-4"><h2 className="text-sm font-medium text-muted">{t('items.notes')}</h2><p className="mt-2 whitespace-pre-wrap break-words">{item.notes}</p></div> : null}
      </div> : null}
      {editing ? <div className="mt-6 space-y-4">
        {editing.conflict && editing.latest ? <div className="space-y-1 rounded-xl border border-line bg-sand/70 px-4 py-3 text-sm"><p className="font-medium">{t('itemDetail.edit.conflict')}</p>{editing.latest.title !== editing.mine?.title ? <p>{t('itemDetail.edit.latest.title', { value: editing.latest.title })}</p> : null}{editing.latest.subjectId !== editing.mine?.subjectId ? <p>{t('itemDetail.edit.latest.subject', { value: editSubjects.find((entry) => entry.id === editing.latest?.subjectId)?.name ?? t('items.subject.unknown') })}</p> : null}{(editing.latest.attentionOn ?? '') !== editing.mine?.attentionOn ? <p>{t('itemDetail.edit.latest.date', { value: editing.latest.attentionOn ? formatDate(editing.latest.attentionOn, locale) : t('itemDetail.edit.noDate') })}</p> : null}{(editing.latest.notes ?? '') !== editing.mine?.notes ? <p className="whitespace-pre-wrap break-words">{t('itemDetail.edit.latest.notes', { value: editing.latest.notes ?? t('itemDetail.edit.noNotes') })}</p> : null}{!recurrenceEqual(editing.latest.recurrence, editing.mine?.recurrence ?? null) ? <p>{t('itemDetail.edit.latest.repeat', { value: editing.latest.recurrence ? `${t(pluralKey(editing.latest.recurrence.intervalUnit, editing.latest.recurrence.intervalValue, locale), { count: String(editing.latest.recurrence.intervalValue) })} · ${t(editing.latest.recurrence.mode === 'after_completion' ? 'itemDetail.repeat.fluid' : 'itemDetail.repeat.fixed')}` : t('itemDetail.edit.noRepeat') })}</p> : null}</div> : null}
        <ItemForm key={`${editing.baseline.id}:${editing.etag}`} mode="edit" initialValues={editing.conflict ? editing.mine : { title: editing.baseline.title, subjectId: editing.baseline.subjectId, attentionOn: editing.baseline.attentionOn ?? '', notes: editing.baseline.notes ?? '', recurrence: editing.baseline.recurrence }} subjects={editSubjects} busy={busy} notice={editing.conflict ? t('itemDetail.edit.conflictHint') : undefined} onSubmit={saveEdit} onCancel={() => { returnEditFocus.current = true; setEditing(null); }} />
      </div> : null}
      {!item.done && !item.archived && !editing ? <Button ref={editButtonRef} type="button" variant="quiet" className="mt-5" disabled={busy || editLoading} onClick={() => void beginEdit()}>{t(editLoading ? 'itemDetail.edit.loading' : 'itemDetail.edit.button')}</Button> : null}
      {!item.done && !item.archived && !editing ? <div className="mt-5 flex flex-wrap gap-2">
        {(item.workflowState === 'open' || item.workflowState === 'waiting') ? <Button type="button" disabled={busy} onClick={() => void mutate({ workflowState: 'in_progress' })}>{t(item.workflowState === 'waiting' ? 'itemDetail.resumeWork' : 'itemDetail.start')}</Button> : null}
        {item.workflowState !== 'paused' ? <><Button type="button" variant="quiet" disabled={busy} onClick={() => void mutate({ workflowState: 'waiting' })}>{t('itemDetail.waitingAction')}</Button><Button type="button" variant="quiet" disabled={busy} onClick={() => void mutate({ workflowState: 'paused' })}>{t('itemDetail.pause')}</Button></> : <Button type="button" disabled={busy} onClick={() => void mutate({ workflowState: 'open' })}>{t('itemDetail.resume')}</Button>}
      </div> : null}
      {!editing && (!item.archived ? <div className="mt-5 border-t border-line pt-4"><Button type="button" variant="quiet" disabled={busy} onClick={() => void mutate({ archived: true }, true)}>{t('subjects.archive')}</Button></div> : <Button type="button" variant="quiet" className="mt-5" disabled={busy} onClick={() => void mutate({ archived: false }, true)}>{t('subjects.restore')}</Button>)}
      <section className="mt-8 border-t border-line pt-6"><h2 className="font-display text-2xl">{t('itemDetail.history')}</h2>{historyState === 'loading' ? <p role="status" className="mt-3 text-sm text-muted">{t('itemDetail.historyLoading')}</p> : historyState === 'error' ? <div className="mt-3 space-y-2"><p className="text-sm text-muted">{t('itemDetail.historyError')}</p><Button type="button" variant="quiet" onClick={() => void loadHistory(generation.current)}>{t('app.error.retry')}</Button></div> : history.length ? <>{historyPartial ? <p className="mt-3 text-sm text-muted">{t('itemDetail.historyPartial')}</p> : null}<ol className="mt-3 space-y-2">{history.map((completion) => <li key={completion.id} className={`rounded-xl bg-white/50 px-4 py-3 text-sm ${completion.undoneAt ? 'text-muted opacity-65' : ''}`}><p>{t(completion.undoneAt ? 'itemDetail.historyUndone' : 'itemDetail.historyDone', { date: formatDate(completion.completedOn, locale) })}</p>{completion.cycleAttentionOn ? <p className="mt-1 text-muted">{t('itemDetail.historyPlanned', { date: formatDate(completion.cycleAttentionOn, locale) })}</p> : null}{completion.nextAttentionOn ? <p className="text-muted">{t('itemDetail.historyNext', { date: formatDate(completion.nextAttentionOn, locale) })}</p> : null}</li>)}</ol></> : <p className="mt-3 text-sm text-muted">{t('itemDetail.historyEmpty')}</p>}</section>
    </> : null}
  </section></Layout>;
}
