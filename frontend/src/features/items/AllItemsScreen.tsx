import { useCallback, useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { AccountActions } from '../../app/AccountActions';
import { Heading } from '../../app/Heading';
import { Layout } from '../../app/Layout';
import { NavLink } from '../../app/NavLink';
import { Button } from '../../components/ui/button';
import { useI18n, type MessageKey } from '../../i18n';
import { listMembers, type Member } from '../members/membersApi';
import { repeatSummary } from './repeatSummary';
import {
  getFilteredItemsSummary,
  listActiveSubjects,
  listAllItems,
  type AllItemsFilters,
  type AllItemsStatus,
  type Item,
  type ItemSummary,
  type Subject,
} from './itemsApi';

type Context = AllItemsFilters & { status: AllItemsStatus; cursor?: string };
type FacetState = 'loading' | 'ready' | 'failed';
const statuses: AllItemsStatus[] = ['active', 'needs', 'inProgress', 'waiting', 'upcoming', 'paused', 'done', 'archived'];
const statusKeys: Record<AllItemsStatus, MessageKey> = {
  active: 'allItems.status.active', needs: 'items.group.needs', inProgress: 'items.group.inProgress',
  waiting: 'items.group.waiting', upcoming: 'items.group.upcoming', paused: 'items.group.paused',
  done: 'itemDetail.state.done', archived: 'allItems.status.archived',
};

function buildQuery(context: Context): string {
  const query = new URLSearchParams();
  if (context.q) query.set('q', context.q);
  if (context.status !== 'active') query.set('status', context.status);
  if (context.subjectId) query.set('subject', context.subjectId);
  if (context.responsibleUserId) query.set('responsible', context.responsibleUserId);
  if (context.recurring !== undefined) query.set('recurring', String(context.recurring));
  if (context.cursor) query.set('cursor', context.cursor);
  return query.toString();
}

function readContext(): Context {
  const query = new URLSearchParams(window.location.search);
  const rawStatus = query.get('status');
  const rawRecurring = query.get('recurring');
  const context: Context = {
    q: query.get('q')?.trim() || undefined,
    status: statuses.includes(rawStatus as AllItemsStatus) ? rawStatus as AllItemsStatus : 'active',
    subjectId: query.get('subject') || undefined,
    responsibleUserId: query.get('responsible') || undefined,
    recurring: rawRecurring === 'true' ? true : rawRecurring === 'false' ? false : undefined,
    cursor: query.get('cursor') || undefined,
  };
  const normalized = buildQuery(context);
  const location = `/items${normalized ? `?${normalized}` : ''}`;
  if (`${window.location.pathname}${window.location.search}` !== location) {
    window.history.replaceState({ ...(window.history.state ?? {}), allItemsCursors: [] }, '', location);
  }
  return context;
}

function cursorStack(): string[] {
  const state = window.history.state?.allItemsCursors;
  return Array.isArray(state) ? state.filter((cursor: unknown): cursor is string => typeof cursor === 'string') : [];
}

function formatDate(value: string, locale: string): string {
  const [year, month, day] = value.split('-').map(Number);
  return new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeZone: 'UTC' })
    .format(new Date(Date.UTC(year, month - 1, day)));
}

function countKey(count: number, locale: string): MessageKey {
  const category = new Intl.PluralRules(locale).select(count);
  const supported: Record<string, string[]> = { en: ['one', 'other'], cs: ['one', 'few', 'other'] };
  return `items.group.count.${supported[locale]?.includes(category) ? category : 'other'}` as MessageKey;
}

function countFor(status: AllItemsStatus, summary: ItemSummary): number {
  switch (status) {
    case 'active': return summary.needsAttention + summary.inProgress + summary.waiting + summary.upcoming + summary.paused;
    case 'needs': return summary.needsAttention;
    case 'inProgress': return summary.inProgress;
    case 'waiting': return summary.waiting;
    case 'upcoming': return summary.upcoming;
    case 'paused': return summary.paused;
    case 'done': return summary.done;
    case 'archived': return summary.archived;
  }
}

async function loadPagedSubjects(householdId: string, archived: boolean): Promise<Subject[]> {
  const subjects: Subject[] = [];
  let cursor: string | undefined;
  for (let page = 0; page < 20; page++) {
    const result = await listActiveSubjects(householdId, archived, cursor);
    if (result.kind !== 'ok') throw result;
    subjects.push(...result.subjects);
    if (!result.nextCursor) return subjects;
    cursor = result.nextCursor;
  }
  throw new Error('Subject page limit reached');
}

async function loadPagedMembers(householdId: string): Promise<Member[]> {
  const members: Member[] = [];
  let cursor: string | undefined;
  for (let page = 0; page < 20; page++) {
    const result = await listMembers(householdId, cursor);
    if (result.kind !== 'ok') throw result;
    members.push(...result.items);
    if (!result.nextCursor) return members;
    cursor = result.nextCursor;
  }
  throw new Error('Member page limit reached');
}

export function AllItemsScreen({ householdId, login, userId, onBack, onSignedOut, onOpenItem }: {
  householdId: string;
  login: string;
  userId: string;
  onBack: () => void;
  onSignedOut: () => void;
  onOpenItem: (itemId: string, returnTo: string) => void;
}) {
  const { t, locale } = useI18n();
  const [context, setContext] = useState(readContext);
  const [search, setSearch] = useState(context.q ?? '');
  const [status, setStatus] = useState(context.status);
  const [subjectId, setSubjectId] = useState(context.subjectId ?? '');
  const [responsible, setResponsible] = useState(context.responsibleUserId ?? '');
  const [recurring, setRecurring] = useState(context.recurring === undefined ? '' : String(context.recurring));
  const [subjects, setSubjects] = useState<Subject[]>([]);
  const [members, setMembers] = useState<Member[]>([]);
  const [subjectState, setSubjectState] = useState<FacetState>('loading');
  const [memberState, setMemberState] = useState<FacetState>('loading');
  const [items, setItems] = useState<Item[]>([]);
  const [summary, setSummary] = useState<ItemSummary | null>(null);
  const [nextCursor, setNextCursor] = useState<string | null>(null);
  const [loadingPage, setLoading] = useState(true);
  // Query whose rows are currently in `items`; rows for any other context are stale and must not render.
  const [loadedQuery, setLoadedQuery] = useState<string | null>(null);
  const loading = loadingPage;
  const rowsCurrent = loadedQuery === buildQuery(context);
  const [error, setError] = useState(false);
  const [searchError, setSearchError] = useState(false);
  const [notice, setNotice] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const generation = useRef(0);
  const mounted = useRef(false);
  const focusResults = useRef(false);
  const skipNextContextLoad = useRef(false);
  const resultRef = useRef<HTMLElement>(null);
  useEffect(() => {
    mounted.current = true;
    const generationRef = generation;
    return () => { mounted.current = false; generationRef.current++; };
  }, []);

  useEffect(() => {
    let cancelled = false;
    void loadPagedSubjects(householdId, false)
      .then((active) => loadPagedSubjects(householdId, true).then((archived) => [...active, ...archived.filter((subject) => !active.some((entry) => entry.id === subject.id))]))
      .then((allSubjects) => {
        if (cancelled) return;
        setSubjects(allSubjects);
        setSubjectState('ready');
      })
      .catch((failure: { kind?: string }) => {
        if (cancelled) return;
        if (failure.kind === 'unauthenticated') onSignedOut();
        if (failure.kind === 'noHousehold') setError(true);
        setSubjectState('failed');
      });
    void loadPagedMembers(householdId)
      .then((allMembers) => {
        if (cancelled) return;
        setMembers(allMembers);
        setMemberState('ready');
      })
      .catch((failure: { kind?: string }) => {
        if (cancelled) return;
        if (failure.kind === 'unauthenticated') onSignedOut();
        if (failure.kind === 'noHousehold') setError(true);
        setMemberState('failed');
      });
    return () => { cancelled = true; };
  }, [householdId, onSignedOut]);

  const loadPage = useCallback(async (nextContext: Context) => {
    // Context transitions also bump the generation synchronously; each load still owns a fresh token.
    const token = ++generation.current;
    setLoading(true);
    setError(false);
    setSearchError(false);

    async function run(pageContext: Context, allowRestart: boolean, requestToken = token): Promise<void> {
      const current = () => mounted.current && requestToken === generation.current;
      const activeFilters: AllItemsFilters = {
        ...(pageContext.q ? { q: pageContext.q } : {}),
        ...(pageContext.subjectId ? { subjectId: pageContext.subjectId } : {}),
        ...(pageContext.responsibleUserId ? { responsibleUserId: pageContext.responsibleUserId } : {}),
        ...(pageContext.recurring !== undefined ? { recurring: pageContext.recurring } : {}),
      };
      const summaryResult = await getFilteredItemsSummary(householdId, activeFilters);
      if (!current()) return;
      if (summaryResult.kind === 'unauthenticated') { onSignedOut(); return; }
      if (summaryResult.kind !== 'ok') { setError(true); setLoading(false); setLoadedQuery(buildQuery(pageContext)); return; }
      const nextSummary = summaryResult.summary;

      const pageResult = await listAllItems(householdId, pageContext.status, activeFilters, nextSummary.evaluatedOn, pageContext.cursor);
      if (!current()) return;
      if (pageResult.kind === 'unauthenticated') { onSignedOut(); return; }
      if (pageResult.kind === 'invalidQuery') { setSearchError(true); setLoading(false); setLoadedQuery(buildQuery(pageContext)); return; }
      if (pageResult.kind === 'stale') {
        if (allowRestart) {
          if (!current()) return;
          generation.current++;
          setNotice(true);
          const firstPage = { ...pageContext, cursor: undefined };
          const query = buildQuery(firstPage);
          window.history.replaceState({ ...(window.history.state ?? {}), allItemsCursors: [] }, '', `/items${query ? `?${query}` : ''}`);
          const restartToken = generation.current;
          skipNextContextLoad.current = true;
          setContext(firstPage);
          await run(firstPage, false, restartToken);
          return;
        }
        setError(true);
        setLoading(false);
        setLoadedQuery(buildQuery(pageContext));
        return;
      }
      if (pageResult.kind !== 'ok') { setError(true); setLoading(false); setLoadedQuery(buildQuery(pageContext)); return; }
      setSummary(nextSummary);
      setItems(pageResult.items);
      setNextCursor(pageResult.nextCursor);
      setLoadedQuery(buildQuery(pageContext));
      setError(false);
      setLoading(false);
    }

    await run(nextContext, true);
  }, [householdId, onSignedOut]);

  useEffect(() => {
    if (skipNextContextLoad.current) {
      skipNextContextLoad.current = false;
      return;
    }
    const timer = window.setTimeout(() => { void loadPage(context); }, 0);
    return () => window.clearTimeout(timer);
  }, [loadPage, context, attempt]);
  useEffect(() => {
    if (loading || error || !focusResults.current) return;
    const active = document.activeElement;
    const pagingControl = active instanceof HTMLElement && active.matches('[data-all-items-page]');
    if (pagingControl || active === document.body || active === null) resultRef.current?.focus();
    focusResults.current = false;
  }, [loading, error, items]);
  useEffect(() => {
    const sync = () => {
      if (window.location.pathname !== '/items') return;
      generation.current++;
      const next = readContext();
      setContext(next);
      setSearch(next.q ?? '');
      setStatus(next.status);
      setSubjectId(next.subjectId ?? '');
      setResponsible(next.responsibleUserId ?? '');
      setRecurring(next.recurring === undefined ? '' : String(next.recurring));
      setNotice(false);
    };
    window.addEventListener('popstate', sync);
    return () => window.removeEventListener('popstate', sync);
  }, []);

  function update(next: Partial<Context>) {
    generation.current++;
    const value: Context = { ...context, ...next, cursor: undefined };
    const query = buildQuery(value);
    window.history.replaceState({ ...(window.history.state ?? {}), allItemsCursors: [] }, '', `/items${query ? `?${query}` : ''}`);
    setNotice(false);
    setContext(value);
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    const q = search.trim();
    if (q.length > 200) { setSearchError(true); return; }
    setSearchError(false);
    update({ q: q || undefined });
  }

  const subjectNames = new Map(subjects.map((subject) => [subject.id, subject.name]));
  const labelForItem = (item: Item) => t(item.archived ? 'allItems.status.archived'
    : item.done ? 'itemDetail.state.done'
      : item.workflowState === 'paused' ? 'items.group.paused'
        : item.workflowState === 'in_progress' ? 'items.group.inProgress'
          : item.workflowState === 'waiting' ? 'items.group.waiting'
            : item.attention === 'upcoming' ? 'items.group.upcoming' : 'items.group.needs');
  // Same wording as Home rows: only an open item can be due on or overdue since its date.
  const attentionKeyForItem = (item: Item): MessageKey => item.archived || item.done || item.workflowState !== 'open' ? 'items.rowAttentionDate'
    : item.attention === 'upcoming' ? 'items.attentionOn' : 'items.needsSince';
  const stack = cursorStack();
  const pageNumber = context.cursor ? stack.length + 1 : 1;
  // After a reload on a later page the earlier cursors are gone, so the page position is unknown.
  const pageKnown = !context.cursor || stack[0] === '';
  const selectedCount = summary ? countFor(context.status, summary) : 0;
  const anyFilters = Boolean(context.q || context.subjectId || context.responsibleUserId || context.recurring !== undefined);
  function goToCursor(cursor: string | undefined, nextStack: string[]) {
    generation.current++;
    const next = { ...context, cursor };
    const query = buildQuery(next);
    window.history.replaceState({ ...(window.history.state ?? {}), allItemsCursors: nextStack }, '', `/items${query ? `?${query}` : ''}`);
    setNotice(false);
    focusResults.current = true;
    setContext(next);
  }

  return <Layout actions={<AccountActions login={login} onSignedOut={onSignedOut} />}>
    <section className="settle space-y-5">
      <Heading className="font-display text-h1 leading-tight">{t('allItems.title')}</Heading>
      <form role="search" aria-label={t('allItems.search')} onSubmit={submit} className="flex flex-col items-stretch gap-3 sm:flex-row sm:items-end">
        <div className="min-w-0 flex-1">
          <label htmlFor="all-items-search" className="block text-sm font-medium">{t('allItems.search')}</label>
          <input id="all-items-search" type="search" value={search}
            onChange={(event) => { setSearch(event.target.value); setSearchError(false); }}
            aria-invalid={searchError} aria-describedby={searchError ? 'all-items-search-error' : undefined}
            className="mt-1 min-h-11 w-full rounded-xl border border-field bg-white px-3 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent" />
          {searchError ? <p id="all-items-search-error" className="mt-1 text-small text-danger">{t('allItems.searchTooLong')}</p> : null}
        </div>
        <Button type="submit">{t('allItems.searchButton')}</Button>
      </form>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <label className="text-sm">{t('allItems.status.label')}
          <select value={status} onChange={(event) => { const value = event.target.value as AllItemsStatus; setStatus(value); update({ status: value }); }} className="mt-1 block min-h-11 w-full min-w-0 rounded-xl border border-field bg-white px-3 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent">
            {statuses.map((id) => <option key={id} value={id}>{t(statusKeys[id])}{summary ? ` (${countFor(id, summary)})` : ''}</option>)}
          </select>
        </label>
        {subjectState === 'ready' ? <label className="text-sm">{t('items.subject')}
          <select value={subjectId} onChange={(event) => { setSubjectId(event.target.value); update({ subjectId: event.target.value || undefined }); }} className="mt-1 block min-h-11 w-full min-w-0 rounded-xl border border-field bg-white px-3 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent">
            <option value="">{t('allItems.any')}</option>
            {subjects.map((subject) => <option key={subject.id} value={subject.id}>{subject.name}{subject.archived ? ` ${t('allItems.archivedSuffix')}` : ''}</option>)}
            {subjectId && !subjectNames.has(subjectId) ? <option value={subjectId}>{t('allItems.unknown')}</option> : null}
          </select>
        </label> : null}
        {memberState === 'ready' ? <label className="text-sm">{t('items.responsible.label')}
          <select value={responsible} onChange={(event) => { setResponsible(event.target.value); update({ responsibleUserId: event.target.value || undefined }); }} className="mt-1 block min-h-11 w-full min-w-0 rounded-xl border border-field bg-white px-3 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent">
            <option value="">{t('allItems.anyone')}</option><option value="unassigned">{t('items.responsible.none')}</option>
            {members.map((member) => <option key={member.userId} value={member.userId}>{member.login}{member.userId === userId ? ` (${t('allItems.you')})` : ''}</option>)}
            {responsible && responsible !== 'unassigned' && !members.some((member) => member.userId === responsible) ? <option value={responsible}>{t('allItems.unknown')}</option> : null}
          </select>
        </label> : null}
        <label className="text-sm">{t('allItems.repeat')}
          <select value={recurring} onChange={(event) => { setRecurring(event.target.value); update({ recurring: event.target.value === '' ? undefined : event.target.value === 'true' }); }} className="mt-1 block min-h-11 w-full min-w-0 rounded-xl border border-field bg-white px-3 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent">
            <option value="">{t('allItems.any')}</option><option value="true">{t('allItems.repeating')}</option><option value="false">{t('allItems.oneOff')}</option>
          </select>
        </label>
      </div>
      {notice ? <p role="status" aria-live="polite" className="rounded-xl bg-sand px-4 py-2">{t('allItems.stale')}</p> : null}
      {loading ? <p role="status" className="text-muted">{t('items.loading')}</p> : null}
      {error ? <div><p className="text-muted">{t('app.error.body')}</p><Button type="button" variant="quiet" onClick={() => setAttempt((value) => value + 1)}>{t('app.error.retry')}</Button></div> : null}
      {!loading && !error && rowsCurrent && summary && summary.total === 0 && !anyFilters
        ? <div className="rounded-3xl border border-line bg-white/70 p-6 sm:p-8"><p className="font-display text-xl">{t('allItems.none')}</p><NavLink href="/" onNavigate={onBack} className="mt-3 inline-flex min-h-11 items-center rounded-lg text-accent underline underline-offset-4 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent">{t('items.add')}</NavLink></div> : null}
      {!loading && !error && rowsCurrent && summary && !items.length && (summary.total > 0 || anyFilters)
        ? <div className="rounded-3xl border border-line bg-white/70 p-6 sm:p-8"><p className="font-display text-xl">{t('allItems.noMatch')}</p><Button type="button" variant="quiet" className="mt-2 -ml-3" onClick={() => {
          setSearch(''); setStatus('active'); setSubjectId(''); setResponsible(''); setRecurring(''); setSearchError(false);
          update({ q: undefined, status: 'active', subjectId: undefined, responsibleUserId: undefined, recurring: undefined });
        }}>{t('allItems.clear')}</Button></div> : null}
      {!loading && !error && rowsCurrent && items.length > 0 ? <section ref={resultRef} tabIndex={-1} aria-label={t('allItems.results')} className="outline-none">
        <h2 className="font-display text-xl leading-snug">{pageKnown ? t('allItems.range', {
          from: String((pageNumber - 1) * 25 + 1), to: String((pageNumber - 1) * 25 + items.length), total: String(selectedCount),
        }) : t(countKey(selectedCount, locale), { count: String(selectedCount) })}</h2>
        <ul className="mt-3 divide-y divide-line border-y border-line bg-white/70">
          {items.map((item) => <li key={item.id} data-item-id={item.id} className="p-4">
            <NavLink href={`/items/${encodeURIComponent(item.id)}`} onNavigate={() => onOpenItem(item.id, `/items${window.location.search}`)} className="inline-flex min-h-11 max-w-full [overflow-wrap:anywhere] text-lg leading-snug underline decoration-line underline-offset-4 hover:decoration-accent focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent">{item.title}</NavLink>
            <p className="mt-1 text-sm text-muted">{subjectNames.get(item.subjectId) ?? t('items.subject.unknown')}</p>
            <p className="text-sm font-medium text-accent-strong">{labelForItem(item)}</p>
            {item.attentionOn ? <p className="text-sm text-muted">{t(attentionKeyForItem(item), { date: formatDate(item.attentionOn, locale) })}</p> : null}
            {item.recurrence ? <p className="text-sm text-muted">{repeatSummary(t, item.recurrence, locale)}</p> : null}
            {item.responsibleUserId ? <p className="text-sm text-muted">{t('items.responsible.read', { name: members.find((member) => member.userId === item.responsibleUserId)?.login ?? t('items.responsible.unknown') })}</p> : null}
          </li>)}
        </ul>
        <div className="mt-3 flex flex-wrap items-center justify-between gap-2">
          {context.cursor ? <Button data-all-items-page type="button" variant="quiet" onClick={() => {
            const prior = [...cursorStack()];
            const previous = prior.pop();
            goToCursor(previous || undefined, prior);
          }}>{stack.length ? t('allItems.previous') : t('allItems.first')}</Button> : null}
          {nextCursor ? <Button data-all-items-page type="button" variant="quiet" onClick={() => {
            // '' marks the first page (no cursor) so Previous and the range line stay truthful from page 2 on.
            const prior = [...cursorStack(), context.cursor ?? ''];
            goToCursor(nextCursor, prior);
          }}>{t('allItems.next')}</Button> : null}
        </div>
      </section> : null}
    </section>
  </Layout>;
}
