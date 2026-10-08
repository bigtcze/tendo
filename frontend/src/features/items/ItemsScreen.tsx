import { useCallback, useEffect, useRef, useState } from 'react';
import { Button } from '../../components/ui/button';
import { NavLink } from '../../app/NavLink';
import { useI18n, type MessageKey } from '../../i18n';
import { ItemForm } from './ItemForm';
import { clearPendingCompletion, getPendingCompletion, setPendingCompletion } from './pendingCompletions';
import { completeItem, createItem, getItem, listActiveSubjects, listItems, undoCompletion, type Completion, type Item, type Recurrence, type Subject } from './itemsApi';

type Notice = { text: string; completion?: Completion; itemId?: string; retryKey?: string; retryKind?: 'complete' | 'undo'; undoAvailable?: boolean };
type SubjectState = 'loading' | 'error' | 'ready';
const MAX_SUBJECT_PAGES = 20;

function formatDate(value: string, locale: string) {
  const [year, month, day] = value.split('-').map(Number);
  return new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeZone: 'UTC' }).format(new Date(Date.UTC(year, month - 1, day)));
}
function idempotencyKey() {
  const bytes = new Uint8Array(16);
  crypto.getRandomValues(bytes);
  return [...bytes].map((byte) => byte.toString(16).padStart(2, '0')).join('');
}
function pluralKey(unit: Recurrence['intervalUnit'], count: number, locale: string): MessageKey {
  const category = new Intl.PluralRules(locale).select(count);
  const supported: Record<string, readonly string[]> = { en: ['one', 'other'], cs: ['one', 'few', 'other'] };
  return `items.every.${unit}.${supported[locale]?.includes(category) ? category : 'other'}` as MessageKey;
}

function NoticeView({ notice, busy, onUndo, onRetry, onRetryFocus }: { notice: Notice | null; busy: boolean; onUndo: () => void; onRetry: () => void; onRetryFocus: () => void }) {
  const { t } = useI18n();
  if (!notice) return null;
  return <div role="status" aria-live="polite" className="flex flex-wrap items-center justify-between gap-2 rounded-xl bg-sand px-4 py-2">
    <p>{notice.text}</p>
    {notice.undoAvailable && notice.completion?.id ? <Button type="button" variant="quiet" disabled={busy} onClick={onUndo}>{t('items.undo')}</Button> : null}
    {notice.retryKey && notice.retryKind ? <Button type="button" data-action="completion-trigger" variant="quiet" disabled={busy} onFocus={onRetryFocus} onClick={onRetry}>{t('items.retry')}</Button> : null}
  </div>;
}

function ItemRow({ item, subjectName, locale, busy, onDone, onOpenItem, rowRef }: { item: Item; subjectName: string; locale: string; busy: boolean; onDone: () => void; onOpenItem: (itemId: string) => void; rowRef?: (node: HTMLLIElement | null) => void }) {
  const { t } = useI18n();
  return <li ref={rowRef} data-item-id={item.id} className="flex flex-wrap items-center justify-between gap-3 p-4">
    <div className="min-w-0 flex-1 basis-48">
      <NavLink href={`/items/${encodeURIComponent(item.id)}`} onNavigate={() => onOpenItem(item.id)} className="inline-flex min-h-11 min-w-0 max-w-full break-words text-left text-lg leading-snug underline decoration-line underline-offset-4 hover:decoration-accent focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"><span aria-hidden="true">{item.title}</span><span className="sr-only">{t('itemDetail.open', { title: item.title })}</span></NavLink>
      <p className="mt-1 text-sm text-muted">{subjectName}{item.attention === 'upcoming' && item.attentionOn ? ` · ${t('items.attentionOn', { date: formatDate(item.attentionOn, locale) })}` : ''}</p>
      {item.recurrence ? <p className="mt-1 text-sm text-muted">{t(pluralKey(item.recurrence.intervalUnit, item.recurrence.intervalValue, locale), { count: String(item.recurrence.intervalValue) })}</p> : null}
    </div>
    <Button type="button" data-action="done" disabled={busy} onClick={onDone}>{t('items.done')}</Button>
  </li>;
}

const groups: { key: MessageKey; matches: (item: Item) => boolean; quiet?: boolean }[] = [
  { key: 'items.group.needs', matches: (item) => item.attention === 'needs_attention' && item.workflowState === 'open' },
  { key: 'items.group.inProgress', matches: (item) => item.workflowState === 'in_progress' },
  { key: 'items.group.waiting', matches: (item) => item.workflowState === 'waiting' },
  { key: 'items.group.upcoming', matches: (item) => item.attention === 'upcoming' && item.workflowState === 'open', quiet: true },
  { key: 'items.group.paused', matches: (item) => item.workflowState === 'paused', quiet: true },
];

export function ItemsScreen({ userId, householdId, onOpenPeople, onOpenItem, onSignedOut }: { userId: string; householdId: string; onOpenPeople: () => void; onOpenItem: (itemId: string) => void; onSignedOut: () => void }) {
  const { t, locale } = useI18n();
  const [items, setItems] = useState<Item[]>([]);
  const [subjects, setSubjects] = useState<Subject[]>([]);
  const subjectLoadBusy = useRef(false);
  const [subjectState, setSubjectState] = useState<SubjectState>('loading');
  const [subjectRetry, setSubjectRetry] = useState(0);
  const [subjectLoadingArchived, setSubjectLoadingArchived] = useState(false);
  const [cursor, setCursor] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [itemsRequestComplete, setItemsRequestComplete] = useState(false);
  const [error, setError] = useState(false);
  const [adding, setAdding] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [notice, setNotice] = useState<Notice | null>(null);
  const [busy, setBusy] = useState(false);
  const generation = useRef(0);
  const mutationBusy = useRef(false);
  const noticeRef = useRef<HTMLDivElement>(null);
  const addRef = useRef<HTMLButtonElement>(null);
  const returnFocus = useRef(false);
  const focusedRow = useRef(false);
  const retryHadFocus = useRef(false);
  const sectionHeadingRef = useRef<HTMLHeadingElement>(null);
  const names = new Map(subjects.map((subject) => [subject.id, subject.name]));

  const refresh = useCallback(async () => {
    const started = ++generation.current;
    setLoading(true);
    const result = await listItems(householdId);
    if (started !== generation.current) return;
    if (result.kind === 'unauthenticated') onSignedOut();
    else if (result.kind === 'ok') { setItems(result.items); setCursor(result.nextCursor); setError(false); }
    else setError(true);
    setLoading(false);
  }, [householdId, onSignedOut]);

  useEffect(() => {
    let cancelled = false;
    const started = ++generation.current;
    void listItems(householdId).then((result) => {
      if (cancelled || started !== generation.current) return;
      if (result.kind === 'unauthenticated') onSignedOut();
      else if (result.kind === 'ok') { setItems(result.items); setCursor(result.nextCursor); setLoading(false); setItemsRequestComplete(true); }
      else { setError(true); setLoading(false); setItemsRequestComplete(true); }
    });
    return () => { cancelled = true; };
  }, [householdId, onSignedOut]);

  useEffect(() => {
    let cancelled = false;
    const load = async (archived: boolean) => {
      const collected: Subject[] = [];
      let cursor: string | undefined;
      for (let page = 0; page < MAX_SUBJECT_PAGES; page++) {
        const result = await listActiveSubjects(householdId, archived, cursor);
        if (result.kind !== 'ok') return result;
        collected.push(...result.subjects);
        if (!result.nextCursor) return { kind: 'ok' as const, subjects: collected };
        cursor = result.nextCursor;
      }
      return { kind: 'error' as const };
    };
    subjectLoadBusy.current = true;
    void load(false).then(async (active) => {
      if (cancelled) return;
      if (active.kind === 'unauthenticated') { subjectLoadBusy.current = false; onSignedOut(); return; }
      if (active.kind !== 'ok') { subjectLoadBusy.current = false; setSubjectState('error'); return; }
      setSubjects(active.subjects);
      subjectLoadBusy.current = false;
      setSubjectState('ready');
      setSubjectLoadingArchived(true);
      subjectLoadBusy.current = true;
      const archived = await load(true);
      if (cancelled) { subjectLoadBusy.current = false; setSubjectLoadingArchived(false); return; }
      subjectLoadBusy.current = false;
      setSubjectLoadingArchived(false);
      if (archived.kind === 'unauthenticated') { onSignedOut(); return; }
      if (archived.kind === 'ok') {
        const seen = new Set(active.subjects.map((subject) => subject.id));
        setSubjects((current) => [...current, ...archived.subjects.filter((subject) => !seen.has(subject.id))]);
      }
    });
    return () => { cancelled = true; };
  }, [householdId, onSignedOut, subjectRetry]);

  useEffect(() => {
    if (notice && (focusedRow.current || retryHadFocus.current)) {
      noticeRef.current?.focus();
      focusedRow.current = false;
      retryHadFocus.current = false;
    }
  }, [notice]);
  useEffect(() => {
    if (!adding && returnFocus.current) { addRef.current?.focus(); returnFocus.current = false; }
  }, [adding]);

  function beginMutation() {
    if (mutationBusy.current) return false;
    mutationBusy.current = true;
    setBusy(true);
    return true;
  }
  function endMutation() { mutationBusy.current = false; setBusy(false); }

  async function create(values: { title: string; subjectId: string; attentionOn: string; notes: string; recurrence: Recurrence | null }) {
    if (!beginMutation()) return { kind: 'handled' as const };
    const hadFocus = document.activeElement === addRef.current;
    try {
      const result = await createItem(householdId, { title: values.title, subjectId: values.subjectId, ...(values.attentionOn ? { attentionOn: values.attentionOn } : {}), ...(values.notes ? { notes: values.notes } : {}), recurrence: values.recurrence });
      if (result.kind === 'unauthenticated') { onSignedOut(); return { kind: 'handled' as const }; }
      if (result.kind === 'invalid') return result;
      if (result.kind !== 'ok') return { kind: 'failed' as const };
      generation.current++;
      returnFocus.current = true;
      setAdding(false);
      await refresh();
      setNotice({ text: t('items.notice.added', { title: result.item.title }) });
      if (hadFocus) requestAnimationFrame(() => addRef.current?.focus());
      return { kind: 'done' as const };
    } finally { endMutation(); }
  }

  async function showMore() {
    if (!cursor || loadingMore) return;
    const started = generation.current;
    setLoadingMore(true);
    const result = await listItems(householdId, cursor);
    setLoadingMore(false);
    if (started !== generation.current) return;
    if (result.kind === 'unauthenticated') onSignedOut();
    else if (result.kind === 'ok') { setItems((old) => [...old, ...result.items.filter((item) => !old.some((known) => known.id === item.id))]); setCursor(result.nextCursor); }
    else setError(true);
  }

  async function finish(itemId: string) {
    const focusedControl = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const focused = focusedControl?.dataset.action === 'done' || focusedControl?.dataset.action === 'completion-trigger';
    if (!beginMutation()) return;
    const attemptKey = getPendingCompletion(userId, householdId, itemId) ?? idempotencyKey();
    setPendingCompletion(userId, householdId, itemId, attemptKey);
    try {
      const current = await getItem(householdId, itemId);
      if (current.kind === 'unauthenticated') { onSignedOut(); return; }
      if (current.kind === 'notFound') {
        clearPendingCompletion(userId, householdId, itemId);
        await refresh();
        setNotice({ text: t('items.notice.gone') });
        return;
      }
      if (current.kind !== 'ok' || !current.etag) {
        await refresh();
        setNotice({ text: t(current.kind === 'changed' ? 'items.notice.changed' : 'items.error.action'), itemId, retryKey: attemptKey, retryKind: 'complete' });
        return;
      }
      const result = await completeItem(householdId, itemId, current.etag, attemptKey);
      if (result.kind === 'unauthenticated') { onSignedOut(); return; }
      if (result.kind === 'ok') {
        if (result.completion.undoneAt) {
          clearPendingCompletion(userId, householdId, itemId);
          await refresh();
          setNotice({ text: t('items.notice.alreadyUndone') });
          focusedRow.current = focused;
          requestAnimationFrame(() => noticeRef.current?.focus());
          return;
        }
        clearPendingCompletion(userId, householdId, itemId);
        focusedRow.current = focused;
        await refresh();
        const title = current.item.title;
        const date = result.completion.nextAttentionOn;
        setNotice({ text: current.item.recurrence && date ? t('items.notice.doneNext', { title, date: formatDate(date, locale) }) : t('items.notice.done', { title }), completion: result.completion, itemId, undoAvailable: true });
      } else if (result.kind === 'notFound') { clearPendingCompletion(userId, householdId, itemId); await refresh(); setNotice({ text: t('items.notice.gone') }); }
      else if (result.kind === 'conflict') {
        clearPendingCompletion(userId, householdId, itemId);
        await refresh();
        setNotice({ text: t(result.code === 'item_archived' ? 'items.notice.archived' : result.code === 'item_done' ? 'items.notice.alreadyDone' : 'items.notice.conflict') });
      } else if (result.kind === 'changed') { await refresh(); setNotice({ text: t('items.notice.changed') }); }
      else if (result.kind === 'invalid') {
        clearPendingCompletion(userId, householdId, itemId);
        setNotice({ text: t(result.code === 'idempotency_key_reused' ? 'items.notice.keyUsed' : result.code === 'future_date' ? 'items.notice.futureDate' : result.code === 'date_overflow' ? 'items.notice.dateOverflow' : 'items.notice.validation') });
      } else setNotice({ text: t('items.notice.retry'), itemId, retryKey: attemptKey, retryKind: 'complete' });
    } finally { endMutation(); }
  }

  async function undo() {
    if (!notice?.completion || !notice.itemId) return;
    if (!beginMutation()) return;
    const previous = notice;
    const itemId = notice.itemId;
    const completion = notice.completion;
    try {
      const current = await getItem(householdId, itemId);
      if (current.kind === 'unauthenticated') { onSignedOut(); return; }
      if (current.kind === 'notFound') { await refresh(); setNotice({ text: t('items.notice.gone') }); return; }
      if (current.kind !== 'ok' || !current.etag) {
        await refresh();
        setNotice({ ...previous, text: t('items.notice.changed'), undoAvailable: true });
        return;
      }
      const result = await undoCompletion(householdId, itemId, completion.id, current.etag);
      if (result.kind === 'unauthenticated') { onSignedOut(); return; }
      if (result.kind === 'ok') {
        setNotice(null);
        await refresh();
        requestAnimationFrame(() => {
          const done = document.querySelector<HTMLButtonElement>(`[data-item-id="${itemId}"] [data-action="done"]`);
          if (done) done.focus();
          else if (sectionHeadingRef.current) sectionHeadingRef.current.focus();
          else if (noticeRef.current) noticeRef.current.focus();
          else addRef.current?.focus();
        });
      } else if (result.kind === 'notFound') { await refresh(); setNotice({ text: t('items.notice.gone') }); }
      else if (result.kind === 'conflict') {
        await refresh();
        const key = result.code === 'completion_not_latest' ? 'items.notice.notLatest' : 'items.notice.archived';
        setNotice({ text: t(key) });
      } else if (result.kind === 'changed') { await refresh(); setNotice({ ...previous, text: t('items.notice.changed'), retryKind: 'undo', undoAvailable: true }); }
      else setNotice({ ...previous, text: t('items.notice.retry'), retryKind: 'undo', undoAvailable: true });
    } finally { endMutation(); }
  }

  async function retryNotice() {
    if (!notice) return;
    if (notice.retryKind === 'complete' && notice.itemId) await finish(notice.itemId);
    else if (notice.retryKind === 'undo') await undo();
  }

  return <div className="space-y-6">
    <div ref={noticeRef} tabIndex={-1} className="outline-none">{notice ? <NoticeView notice={notice} busy={busy} onUndo={() => void undo()} onRetry={() => void retryNotice()} onRetryFocus={() => { retryHadFocus.current = true; }} /> : null}</div>
    {error ? <div className="space-y-3"><p className="text-muted">{t('app.error.body')}</p><Button type="button" variant="quiet" onClick={() => void refresh()}>{t('app.error.retry')}</Button></div> : null}
    {loading ? <p role="status" className="text-muted">{t('items.loading')}</p> : null}
    {subjectState === 'error' && !subjectLoadingArchived ? <div className="space-y-3"><p className="text-muted">{t('items.subjects.error')}</p><Button type="button" variant="quiet" onClick={() => { subjectLoadBusy.current = true; setSubjectState('loading'); setSubjectRetry((value) => value + 1); }}>{t('app.error.retry')}</Button></div> : null}
    {!loading && !error && items.length === 0 && !adding ? <div className="rounded-3xl border border-line bg-white/70 p-6 sm:p-10"><h2 className="font-display text-2xl">{t('home.empty.title')}</h2><p className="mt-3 text-muted">{t('home.empty.body')}</p></div> : null}
    {itemsRequestComplete && !adding ? <Button ref={addRef} data-action="completion-trigger" type="button" onClick={() => { setAdding(true); }} className="w-full sm:w-auto">{t('items.add')}</Button> : null}
    {adding && subjectState === 'ready' && subjects.some((subject) => !subject.archived) ? <ItemForm busy={busy} subjects={subjects.filter((subject) => !subject.archived)} onSubmit={create} onCancel={() => { returnFocus.current = true; setAdding(false); }} /> : null}
    {adding && subjectState === 'ready' && !subjects.some((subject) => !subject.archived) ? <div className="rounded-2xl border border-line bg-white/70 p-5"><p>{t('items.noSubjects')}</p><Button type="button" variant="quiet" className="mt-3" onClick={onOpenPeople}>{t('home.people')}</Button><Button type="button" variant="quiet" className="mt-3" onClick={() => setAdding(false)}>{t('items.cancel')}</Button></div> : null}
    {!loading ? groups.map((group) => {
      const grouped = items.filter(group.matches);
      if (!grouped.length) return null;
      return <section key={group.key} className={group.quiet ? 'border-t border-line pt-5' : ''}><h2 ref={sectionHeadingRef} tabIndex={-1} className={`font-display ${group.quiet ? 'text-xl text-muted' : 'text-2xl'}`}>{t(group.key)}</h2><ul className="mt-3 divide-y divide-line rounded-2xl border border-line bg-white/70">{grouped.map((item) => <ItemRow key={item.id} item={item} subjectName={names.get(item.subjectId) ?? t('items.subject.unknown')} locale={locale} busy={busy} onDone={() => void finish(item.id)} onOpenItem={onOpenItem} />)}</ul></section>;
    }) : null}
    {cursor && !loading ? <Button type="button" variant="quiet" disabled={loadingMore} onClick={() => void showMore()}>{loadingMore ? t('items.moreLoading') : t('subjects.more')}</Button> : null}
  </div>;
}
