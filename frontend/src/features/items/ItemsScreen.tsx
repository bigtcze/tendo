import { useCallback, useEffect, useRef, useState } from 'react';
import { Button } from '../../components/ui/button';
import { useI18n, type MessageKey } from '../../i18n';
import { ItemForm } from './ItemForm';
import {
  completeItem,
  createItem,
  getItem,
  listActiveSubjects,
  listItems,
  undoCompletion,
  type Completion,
  type Item,
  type Recurrence,
  type Subject,
} from './itemsApi';

type Notice = { text: string; completion?: Completion; itemId?: string; retryKey?: string };
type SubjectState = 'loading' | 'error' | 'ready';

function formatDate(value: string, locale: string): string {
  const [year, month, day] = value.split('-').map(Number);
  return new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeZone: 'UTC' }).format(
    new Date(Date.UTC(year, month - 1, day)),
  );
}

function idempotencyKey(): string {
  const bytes = new Uint8Array(16);
  crypto.getRandomValues(bytes);
  return [...bytes].map((byte) => byte.toString(16).padStart(2, '0')).join('');
}

function pluralKey(unit: Recurrence['intervalUnit'], count: number, locale: string): MessageKey {
  const category = new Intl.PluralRules(locale).select(count);
  const supported: Record<string, readonly string[]> = {
    en: ['one', 'other'],
    cs: ['one', 'few', 'other'],
  };
  const key = supported[locale]?.includes(category) ? category : 'other';
  return `items.every.${unit}.${key}` as MessageKey;
}

function NoticeView({ notice, busy, onUndo, onRetry }: {
  notice: Notice | null;
  busy: boolean;
  onUndo: () => void;
  onRetry: () => void;
}) {
  const { t } = useI18n();
  if (!notice) return null;
  return (
    <div role="status" aria-live="polite" className="flex flex-wrap items-center justify-between gap-2 rounded-xl bg-sand px-4 py-2">
      <p>{notice.text}</p>
      {notice.completion?.id ? <Button type="button" variant="quiet" disabled={busy} onClick={onUndo}>{t('items.undo')}</Button> : null}
      {notice.retryKey ? <Button type="button" variant="quiet" disabled={busy} onClick={onRetry}>{t('items.retry')}</Button> : null}
    </div>
  );
}

function ItemRow({ item, subjectName, locale, busy, onDone }: {
  item: Item;
  subjectName: string;
  locale: string;
  busy: boolean;
  onDone: () => void;
}) {
  const { t } = useI18n();
  const recurrence = item.recurrence;
  return (
    <li className="flex flex-wrap items-center justify-between gap-3 p-4">
      <div className="min-w-0 flex-1 basis-48">
        <p className="break-words text-lg leading-snug">{item.title}</p>
        <p className="mt-1 text-sm text-muted">
          {subjectName}
          {item.attention === 'upcoming' && item.attentionOn
            ? ` · ${t('items.attentionOn', { date: formatDate(item.attentionOn, locale) })}`
            : ''}
        </p>
        {recurrence ? <p className="mt-1 text-sm text-muted">
          {t(pluralKey(recurrence.intervalUnit, recurrence.intervalValue, locale), { count: String(recurrence.intervalValue) })}
        </p> : null}
      </div>
      <Button type="button" data-action="done" disabled={busy} onClick={onDone}>{t('items.done')}</Button>
    </li>
  );
}

const groups: { key: MessageKey; items: (item: Item) => boolean; quiet?: boolean }[] = [
  { key: 'items.group.needs', items: (item) => item.attention === 'needs_attention' && item.workflowState === 'open' },
  { key: 'items.group.inProgress', items: (item) => item.workflowState === 'in_progress' },
  { key: 'items.group.waiting', items: (item) => item.workflowState === 'waiting' },
  { key: 'items.group.upcoming', items: (item) => item.attention === 'upcoming' && item.workflowState === 'open', quiet: true },
  { key: 'items.group.paused', items: (item) => item.workflowState === 'paused', quiet: true },
];

export function ItemsScreen({ householdId, onOpenPeople, onSignedOut }: { householdId: string; onOpenPeople: () => void; onSignedOut: () => void }) {
  const { t, locale } = useI18n();
  const [items, setItems] = useState<Item[]>([]);
  const [subjects, setSubjects] = useState<Subject[]>([]);
  const [subjectState, setSubjectState] = useState<SubjectState>('loading');
  const [subjectRequestComplete, setSubjectRequestComplete] = useState(false);
  const [subjectReload, setSubjectReload] = useState(0);
  const [cursor, setCursor] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [itemsRequestComplete, setItemsRequestComplete] = useState(false);
  const [error, setError] = useState(false);
  const [adding, setAdding] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [notice, setNotice] = useState<Notice | null>(null);
  const [busy, setBusy] = useState(false);
  const generation = useRef(0);
  const actionBusy = useRef(false);
  const focusedRow = useRef(false);
  const noticeRef = useRef<HTMLDivElement>(null);
  const addRef = useRef<HTMLButtonElement>(null);
  const returnFocus = useRef(false);
  const names = new Map(subjects.map((subject) => [subject.id, subject.name]));

  const refresh = useCallback(async () => {
    const current = ++generation.current;
    setLoading(true);
    const result = await listItems(householdId);
    if (current !== generation.current) return;
    if (result.kind === 'unauthenticated') onSignedOut();
    else if (result.kind === 'ok') {
      setItems(result.items);
      setCursor(result.nextCursor);
      setError(false);
    } else setError(true);
    setLoading(false);
  }, [householdId, onSignedOut]);

  useEffect(() => {
    let cancelled = false;
    void listItems(householdId).then((result) => {
      if (cancelled) return;
      if (result.kind === 'unauthenticated') onSignedOut();
      else if (result.kind === 'ok') { setItems(result.items); setCursor(result.nextCursor); setLoading(false);  setItemsRequestComplete(true); }
      else { setError(true); setLoading(false);  setItemsRequestComplete(true); }
    });
    return () => { cancelled = true; };
  }, [householdId, onSignedOut]);

  useEffect(() => {
    let cancelled = false;
    void listActiveSubjects(householdId).then((active) => {
      if (cancelled) return;
      if (active.kind === 'unauthenticated') { onSignedOut(); return; }
      if (active.kind !== 'ok') { setSubjectState('error'); setSubjectRequestComplete(true); return; }
      setSubjects(active.subjects);
      setSubjectState('ready');
      setSubjectRequestComplete(true);
    });
    return () => { cancelled = true; };
  }, [householdId, onSignedOut, subjectReload]);

  useEffect(() => {
    if (notice && focusedRow.current) {
      noticeRef.current?.focus();
      focusedRow.current = false;
    }
  }, [notice]);
  useEffect(() => {
    if (!adding && returnFocus.current) {
      addRef.current?.focus();
      returnFocus.current = false;
    }
  }, [adding]);

  async function create(values: { title: string; subjectId: string; attentionOn: string; notes: string; recurrence: Recurrence | null }) {
    const result = await createItem(householdId, {
      title: values.title,
      subjectId: values.subjectId,
      ...(values.attentionOn ? { attentionOn: values.attentionOn } : {}),
      ...(values.notes ? { notes: values.notes } : {}),
      recurrence: values.recurrence,
    });
    if (result.kind === 'unauthenticated') { onSignedOut(); return { kind: 'handled' as const }; }
    if (result.kind === 'invalid') return result;
    if (result.kind === 'ok') {
      setItems((current) => [...current, result.item]);
      setAdding(false);
      setNotice({ text: t('items.notice.added', { title: result.item.title }) });
      return { kind: 'done' as const };
    }
    return { kind: 'failed' as const };
  }

  async function showMore() {
    if (!cursor || loadingMore) return;
    const current = generation.current;
    setLoadingMore(true);
    const result = await listItems(householdId, cursor);
    setLoadingMore(false);
    if (current !== generation.current) return;
    if (result.kind === 'unauthenticated') onSignedOut();
    else if (result.kind === 'ok') {
      setItems((existing) => [...existing, ...result.items.filter((item) => !existing.some((old) => old.id === item.id))]);
      setCursor(result.nextCursor);
    } else setError(true);
  }

  async function finish(item: Item, retryKey?: string) {
    if (actionBusy.current) return;
    actionBusy.current = true;
    setBusy(true);
    const key = retryKey ?? idempotencyKey();
    const current = await getItem(householdId, item.id);
    if (current.kind === 'unauthenticated') { onSignedOut(); }
    else if (current.kind !== 'ok' || !current.etag) {
      await refresh();
      setNotice({ text: t(current.kind === 'notFound' ? 'items.notice.gone' : current.kind === 'changed' ? 'items.notice.changed' : 'items.error.action') });
    } else {
      const result = await completeItem(householdId, item.id, current.etag, key);
      if (result.kind === 'unauthenticated') onSignedOut();
      else if (result.kind === 'ok') {
        // The row (and its Done button) may disappear after refresh; keep keyboard users anchored on the notice.
        focusedRow.current = document.activeElement instanceof HTMLElement && document.activeElement.dataset.action === 'done';
        await refresh();
        const date = result.completion.nextAttentionOn;
        setNotice({
          text: item.recurrence && date
            ? t('items.notice.doneNext', { title: item.title, date: formatDate(date, locale) })
            : t('items.notice.done', { title: item.title }),
          completion: result.completion,
          itemId: item.id,
        });
      } else if (result.kind === 'notFound') { await refresh(); setNotice({ text: t('items.notice.gone') }); }
      else if (result.kind === 'changed' || result.kind === 'conflict') {
        await refresh();
        setNotice({ text: t(result.kind === 'changed' ? 'items.notice.changed' : 'items.notice.conflict') });
      } else setNotice({ text: t('items.notice.retry'), itemId: item.id, retryKey: key });
    }
    actionBusy.current = false;
    setBusy(false);
  }

  async function undo() {
    if (!notice?.completion || !notice.itemId || actionBusy.current) return;
    actionBusy.current = true;
    setBusy(true);
    const current = await getItem(householdId, notice.itemId);
    if (current.kind === 'unauthenticated') onSignedOut();
    else if (current.kind !== 'ok' || !current.etag) {
      await refresh();
      setNotice({ text: t(current.kind === 'notFound' ? 'items.notice.gone' : 'items.notice.changed') });
    } else {
      const result = await undoCompletion(householdId, notice.itemId, notice.completion.id, current.etag);
      if (result.kind === 'unauthenticated') onSignedOut();
      else if (result.kind === 'ok') { setNotice(null); await refresh(); }
      else {
        await refresh();
        setNotice({
          text: t(result.kind === 'notFound' ? 'items.notice.gone' : result.kind === 'changed' || result.kind === 'conflict' ? 'items.notice.changed' : 'items.notice.retry'),
          completion: notice.completion,
          itemId: notice.itemId,
        });
      }
    }
    actionBusy.current = false;
    setBusy(false);
  }

  const hasItems = items.length > 0;
  return (
    <div className="space-y-6">
      <div ref={noticeRef} tabIndex={-1} className="outline-none">
        {notice ? <NoticeView notice={notice} busy={busy} onUndo={() => void undo()} onRetry={() => {
          const item = items.find((entry) => entry.id === notice?.itemId);
          if (item && notice?.retryKey) void finish(item, notice.retryKey);
        }} /> : null}
      </div>
      {error ? <div className="space-y-3"><p className="text-muted">{t('app.error.body')}</p><Button type="button" variant="quiet" onClick={() => void refresh()}>{t('app.error.retry')}</Button></div> : null}
      {loading ? <p role="status" className="text-muted">{t('items.loading')}</p> : null}
      {subjectState === 'error' ? <div className="space-y-3"><p className="text-muted">{t('items.subjects.error')}</p><Button type="button" variant="quiet" onClick={() => { setSubjectState('loading'); setSubjectRequestComplete(false); setSubjectReload((attempt) => attempt + 1); }}>{t('app.error.retry')}</Button></div> : null}
      {!loading && !error && !hasItems && !adding ? <div className="rounded-3xl border border-line bg-white/70 p-6 sm:p-10"><h2 className="font-display text-2xl">{t('home.empty.title')}</h2><p className="mt-3 text-muted">{t('home.empty.body')}</p></div> : null}
      {itemsRequestComplete && !adding ? <Button ref={addRef} data-focus="add-item" type="button" onClick={() => { setNotice(null); setAdding(true); }} className="w-full sm:w-auto">{t('items.add')}</Button> : null}
      {adding && subjectRequestComplete && subjectState === 'ready' && subjects.length > 0 ? <ItemForm subjects={subjects.filter((subject) => !subject.archived)} onSubmit={create} onCancel={() => { returnFocus.current = true; setAdding(false); }} /> : null}
      {adding && subjectRequestComplete && subjectState === 'ready' && subjects.filter((subject) => !subject.archived).length === 0 ? <div className="rounded-2xl border border-line bg-white/70 p-5"><p>{t('items.noSubjects')}</p><Button type="button" variant="quiet" className="mt-3" onClick={onOpenPeople}>{t('home.people')}</Button><Button type="button" variant="quiet" className="mt-3" onClick={() => setAdding(false)}>{t('subjects.cancel')}</Button></div> : null}
      {!loading ? groups.map((group) => {
        const groupedItems = items.filter(group.items);
        if (!groupedItems.length) return null;
        return <section key={group.key} className={group.quiet ? 'border-t border-line pt-5' : ''}>
          <h2 className={`font-display ${group.quiet ? 'text-xl text-muted' : 'text-2xl'}`}>{t(group.key)}</h2>
          <ul className="mt-3 divide-y divide-line rounded-2xl border border-line bg-white/70">{groupedItems.map((item) => <ItemRow key={item.id} item={item} subjectName={names.get(item.subjectId) ?? t('items.subject.unknown')} locale={locale} busy={busy} onDone={() => void finish(item)} />)}</ul>
        </section>;
      }) : null}
      {cursor && !loading ? <Button type="button" variant="quiet" disabled={loadingMore} onClick={() => void showMore()}>{loadingMore ? t('items.moreLoading') : t('subjects.more')}</Button> : null}
    </div>
  );
}
