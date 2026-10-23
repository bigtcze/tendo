import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Button } from '../../components/ui/button';
import { NavLink } from '../../app/NavLink';
import { useI18n, type MessageKey } from '../../i18n';
import { ItemForm, type ItemFormValues } from './ItemForm';
import { listMembers, type Member } from '../members/membersApi';
import { clearPendingCompletion, getPendingCompletion, setPendingCompletion } from './pendingCompletions';
import { clearPendingItemCreate, getPendingItemCreate, setPendingItemCreate, type PendingItemCreate } from './pendingItemCreate';
import { completeItem, createItem, getItem, getItemsSummary, listActiveSubjects, listItemGroup, undoCompletion, type Completion, type Item, type ItemGroup, type ItemSummary, type Subject } from './itemsApi';
import { repeatSummary } from './repeatSummary';
import { useSubjectLabels } from './useSubjectLabels';
import { getSubject } from '../subjects/subjectsApi';

type Notice = { text: string; completion?: Completion; itemId?: string; retryKey?: string; retryKind?: 'complete' | 'undo'; undoAvailable?: boolean };
type SubjectState = 'loading' | 'error' | 'ready';
const MAX_SUBJECT_PAGES = 20;
const MAX_MEMBER_PAGES = 20;
export const HOME_GROUP_ROW_CAP = 100;

function formatDate(value: string, locale: string) {
  const [year, month, day] = value.split('-').map(Number);
  return new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeZone: 'UTC' }).format(new Date(Date.UTC(year, month - 1, day)));
}
function idempotencyKey() {
  const bytes = new Uint8Array(16);
  crypto.getRandomValues(bytes);
  return [...bytes].map((byte) => byte.toString(16).padStart(2, '0')).join('');
}
function countPluralKey(count: number, locale: string): MessageKey {
  const category = new Intl.PluralRules(locale).select(count);
  const supported: Record<string, readonly string[]> = { en: ['one', 'other'], cs: ['one', 'few', 'other'] };
  return `items.group.count.${supported[locale]?.includes(category) ? category : 'other'}` as MessageKey;
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

function ItemRow({ item, group, subjectName, locale, userId, members, membersLoaded, busy, onDone, onOpenItem, rowRef }: { item: Item; group: ItemGroup; subjectName: string; locale: string; userId: string; members: Member[]; membersLoaded: boolean; busy: boolean; onDone: () => void; onOpenItem: (itemId: string) => void; rowRef?: (node: HTMLLIElement | null) => void }) {
  const { t } = useI18n();
  const responsible = item.responsibleUserId ? members.find((member) => member.userId === item.responsibleUserId) : undefined;
  const attentionLine = item.attentionOn
    ? t(group === 'upcoming' ? 'items.attentionOn' : group === 'needs' ? 'items.needsSince' : 'items.rowAttentionDate', { date: formatDate(item.attentionOn, locale) })
    : null;
  const historyLine = [
    item.lastCompletedOn ? t('itemDetail.lastDone', { date: formatDate(item.lastCompletedOn, locale) }) : null,
    item.responsibleUserId && membersLoaded ? responsible ? t(responsible.userId === userId ? 'items.responsible.you' : 'items.responsible.read', { name: responsible.login }) : t('items.responsible.unknown') : null,
  ].filter(Boolean).join(' · ');
  return <li ref={rowRef} data-item-id={item.id} className="flex flex-wrap items-center justify-between gap-3 p-4">
    <div className="min-w-0 flex-1 basis-48">
      <NavLink href={`/items/${encodeURIComponent(item.id)}`} onNavigate={() => onOpenItem(item.id)} className="inline-flex min-h-11 min-w-0 max-w-full [overflow-wrap:anywhere] text-left text-lg leading-snug underline decoration-line underline-offset-4 hover:decoration-accent focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"><span aria-hidden="true">{item.title}</span><span className="sr-only">{t('itemDetail.open', { title: item.title })}</span></NavLink>
      <p className="mt-1 break-words text-sm text-muted">{subjectName}{attentionLine ? ` · ${attentionLine}` : ''}</p>
      {historyLine ? <p className="mt-1 break-words text-sm text-muted">{historyLine}</p> : null}
      {item.recurrence ? <p className="mt-1 break-words text-sm text-muted">{repeatSummary(t, item.recurrence, locale)}</p> : null}
    </div>
    <Button type="button" variant="secondary" data-action="done" aria-label={t('items.done.label', { title: item.title })} disabled={busy} onClick={onDone}>{t('items.done')}</Button>
  </li>;
}

const groups: { id: ItemGroup; key: MessageKey; count: GroupCount; quiet?: boolean }[] = [
  { id: 'needs', key: 'items.group.needs', count: 'needsAttention' },
  { id: 'inProgress', key: 'items.group.inProgress', count: 'inProgress' },
  { id: 'waiting', key: 'items.group.waiting', count: 'waiting' },
  { id: 'upcoming', key: 'items.group.upcoming', count: 'upcoming', quiet: true },
  { id: 'paused', key: 'items.group.paused', count: 'paused', quiet: true },
];
type GroupCount = 'needsAttention' | 'inProgress' | 'waiting' | 'upcoming' | 'paused';
type GroupData = { items: Item[]; cursor: string | null; loadingMore: boolean; overflow: boolean };

export function ItemsScreen({ userId, householdId, onOpenPeople, onOpenItem, onOpenAllItems, onSignedOut }: { userId: string; householdId: string; onOpenPeople: () => void; onOpenItem: (itemId: string) => void; onOpenAllItems: (status: ItemGroup) => void; onSignedOut: () => void }) {
  const { t, locale } = useI18n();
  const emptyGroups = (): Record<ItemGroup, GroupData> => ({ needs: { items: [], cursor: null, loadingMore: false, overflow: false }, inProgress: { items: [], cursor: null, loadingMore: false, overflow: false }, waiting: { items: [], cursor: null, loadingMore: false, overflow: false }, upcoming: { items: [], cursor: null, loadingMore: false, overflow: false }, paused: { items: [], cursor: null, loadingMore: false, overflow: false } });
  const [groupData, setGroupData] = useState<Record<ItemGroup, GroupData>>(emptyGroups);
  const [summary, setSummary] = useState<ItemSummary | null>(null);
  const dataHousehold = useRef<string | null>(null);
  const [subjects, setSubjects] = useState<Subject[]>([]);
  const [subjectLoading, setSubjectLoading] = useState(false);
  const [subjectScope, setSubjectScope] = useState(`${userId}:${householdId}`);
  const [members, setMembers] = useState<Member[]>([]);
  const [homeMembersState, setHomeMembersState] = useState<{ householdId: string; members: Member[] } | null>(null);
  const [subjectState, setSubjectState] = useState<SubjectState>('loading');
  const subjectGeneration = useRef(0);
  const catalogueLoaded = useRef(false);
  const recoveryLookup = useRef(new Set<string>());
  const [loading, setLoading] = useState(true);
  const [itemsRequestComplete, setItemsRequestComplete] = useState(false);
  const [error, setError] = useState(false);
  const [pendingCreate, setPendingCreate] = useState<PendingItemCreate | undefined>(() => getPendingItemCreate(userId, householdId));
  const [createUncertain, setCreateUncertain] = useState(() => Boolean(getPendingItemCreate(userId, householdId)));
  const [recoveryEditable, setRecoveryEditable] = useState(false);
  const [recoverySubjectId, setRecoverySubjectId] = useState<string | null>(null);
  const [adding, setAdding] = useState(() => Boolean(getPendingItemCreate(userId, householdId)));
  const addingRef = useRef(Boolean(getPendingItemCreate(userId, householdId)));
  const [notice, setNotice] = useState<Notice | null>(null);
  const [createNotice, setCreateNotice] = useState<'uncertain' | 'reused' | null>(() => getPendingItemCreate(userId, householdId) ? 'uncertain' : null);
  const [busy, setBusy] = useState(false);
  const generation = useRef(0);
  const memberLoadGeneration = useRef(0);
  const homeMemberGeneration = useRef(0);
  const homeMembers = homeMembersState?.householdId === householdId ? homeMembersState.members : [];
  const homeMembersLoaded = homeMembersState?.householdId === householdId;
  const mounted = useRef(false);
  const mutationBusy = useRef(false);
  const noticeRef = useRef<HTMLDivElement>(null);
  const addRef = useRef<HTMLButtonElement>(null);
  const returnFocus = useRef(false);
  const focusedRow = useRef(false);
  const retryHadFocus = useRef(false);
  const labelRetryRef = useRef<HTMLButtonElement>(null);
  const catalogueRetryRef = useRef<HTMLButtonElement>(null);
  const noSubjectsRef = useRef<HTMLButtonElement>(null);
  const catalogueRetryHadFocus = useRef(false);
  const [labelRetryPending, setLabelRetryPending] = useState(false);
  const sectionHeadingRefs = useRef(new Map<ItemGroup, HTMLHeadingElement>());
  const showMoreButtons = useRef(new Map<ItemGroup, HTMLButtonElement>());
  const showMoreInFlight = useRef(new Set<ItemGroup>());
  const focusedMoreGroup = useRef<ItemGroup | null>(null);
  const itemGroup = (itemId: string): ItemGroup | null => {
    for (const group of groups) if (groupData[group.id].items.some((item) => item.id === itemId)) return group.id;
    return null;
  }
  const activeSubjects = useMemo(() => subjectScope === `${userId}:${householdId}` ? subjects : [], [subjectScope, userId, householdId, subjects]);
  // Overview links only target sections that actually render (summary count and loaded rows), so every link has a heading to focus.
  const overviewGroups = summary ? groups.filter((group) => summary[group.count] > 0 && groupData[group.id].items.length > 0) : [];
  const visibleSubjectIDs = useMemo(() => [...new Set(Object.values(groupData).flatMap((group) => group.items.map((item) => item.subjectId)))], [groupData]);
  const labels = useSubjectLabels(userId, householdId, visibleSubjectIDs, onSignedOut);
  const labelSubjects = useMemo(() => [...labels.names].map(([id, name]) => ({ id, name, type: 'custom' as const, linkedUserId: null, archived: false, createdAt: '', updatedAt: '' })), [labels.names]);
  const names = useMemo(() => new Map([...labels.names, ...activeSubjects.map((subject) => [subject.id, subject.name] as const)]), [labels.names, activeSubjects]);
  const retryDraft = recoveryEditable && !createUncertain && pendingCreate ? {
    title: pendingCreate.body.title,
    subjectId: pendingCreate.body.subjectId,
    attentionOn: pendingCreate.body.attentionOn ?? '',
    notes: pendingCreate.body.notes ?? '',
    recurrence: pendingCreate.body.recurrence ?? null,
    responsibleUserId: pendingCreate.body.responsibleUserId ?? null,
    ...(pendingCreate.body.historicalCompletedOn ? { historicalCompletedOn: pendingCreate.body.historicalCompletedOn } : {}),
  } : undefined;
  const recoverySubjects = pendingCreate ? [
    ...activeSubjects.filter((subject) => !subject.archived && subject.id !== pendingCreate.body.subjectId),
    activeSubjects.find((subject) => subject.id === pendingCreate.body.subjectId) ?? labelSubjects.find((subject) => subject.id === pendingCreate.body.subjectId) ?? { id: pendingCreate.body.subjectId, name: t('items.subject.unknown'), type: 'custom' as const, linkedUserId: null, archived: true, createdAt: '', updatedAt: '' },
  ] : recoverySubjectId ? [
    ...activeSubjects.filter((subject) => !subject.archived && subject.id !== recoverySubjectId),
    activeSubjects.find((subject) => subject.id === recoverySubjectId) ?? labelSubjects.find((subject) => subject.id === recoverySubjectId) ?? { id: recoverySubjectId, name: t('items.subject.unknown'), type: 'custom' as const, linkedUserId: null, archived: true, createdAt: '', updatedAt: '' },
  ] : activeSubjects.filter((subject) => !subject.archived);

  const loadHome = useCallback(async ({ allowStaleRestart }: { allowStaleRestart: boolean }) => {
    const started = ++generation.current;
    const current = () => mounted.current && started === generation.current;
    setLoading(true);
    setGroupData((old) => Object.fromEntries(Object.entries(old).map(([key, value]) => [key, { ...value, loadingMore: false }])) as Record<ItemGroup, GroupData>);
    showMoreInFlight.current.clear();
    async function load(allowRestart: boolean): Promise<void> {
      const summaryResult = await getItemsSummary(householdId);
      if (!current()) return;
      if (summaryResult.kind === 'unauthenticated') { if (!current()) return; onSignedOut(); setLoading(false); return; }
      if (summaryResult.kind !== 'ok') { setError(true); setLoading(false); return; }
      const nextSummary = summaryResult.summary;
      const entries = await Promise.all(groups.filter((group) => nextSummary[group.count] > 0).map(async (group) => [group.id, await listItemGroup(householdId, group.id, nextSummary.evaluatedOn)] as const));
      if (!current()) return;
      if (entries.some(([, result]) => result.kind === 'unauthenticated')) { if (!current()) return; onSignedOut(); setLoading(false); return; }
      if (entries.some(([, result]) => result.kind === 'stale')) {
        if (allowRestart) return load(false);
        setError(true); setLoading(false); return;
      }
      if (entries.some(([, result]) => result.kind !== 'ok')) { setError(true); setLoading(false); return; }
      const nextData = emptyGroups();
      for (const [id, result] of entries) if (result.kind === 'ok') nextData[id] = { items: result.items.slice(0, HOME_GROUP_ROW_CAP), cursor: result.nextCursor, loadingMore: false, overflow: result.items.length > HOME_GROUP_ROW_CAP };
      dataHousehold.current = householdId;
      setSummary(nextSummary);
      setGroupData(nextData);
      setError(false);
      setLoading(false);
    }
    await load(allowStaleRestart);
    if (current()) setItemsRequestComplete(true);
  }, [householdId, onSignedOut]);
  const refresh = useCallback(() => loadHome({ allowStaleRestart: true }), [loadHome]);

  useEffect(() => {
    mounted.current = true;
    memberLoadGeneration.current++;
    const generationRef = generation;
    const inFlightRef = showMoreInFlight;
    return () => { mounted.current = false; generationRef.current++; inFlightRef.current.clear(); };
  }, []);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- start async loading; state updates follow network responses.
    void loadHome({ allowStaleRestart: true });
  }, [loadHome]);

  useEffect(() => {
    const token = ++homeMemberGeneration.current;
    const generationRef = homeMemberGeneration;
    const isCurrent = () => mounted.current && token === generationRef.current;
    const collected: Member[] = [];
    let cursor: string | undefined;
    const loadHomeMembers = async () => {
      for (let page = 0; page < MAX_MEMBER_PAGES; page++) {
        const result = await listMembers(householdId, cursor);
        if (!isCurrent()) return;
        if (result.kind === 'unauthenticated') { onSignedOut(); return; }
        if (result.kind !== 'ok') { setHomeMembersState(null); return; }
        collected.push(...result.items);
        if (!result.nextCursor) { setHomeMembersState({ householdId, members: collected }); return; }
        cursor = result.nextCursor;
      }
      if (isCurrent()) setHomeMembersState(null);
    };
    void loadHomeMembers();
    return () => { generationRef.current++; };
  }, [householdId, onSignedOut]);

  async function loadAddSubjects(token: number) {
    const scope = `${userId}:${householdId}`;
    const isCurrent = () => mounted.current && token === subjectGeneration.current && addingRef.current;
    if (!isCurrent()) return;
    setSubjects([]);
    setSubjectScope(scope);
    setSubjectState('loading');
    setSubjectLoading(true);
    const collected: Subject[] = [];
    let cursor: string | undefined;
    for (let page = 0; page < MAX_SUBJECT_PAGES; page++) {
      const result = await listActiveSubjects(householdId, false, cursor);
      if (!isCurrent()) return;
      if (result.kind === 'unauthenticated') { setSubjectLoading(false); onSignedOut(); return; }
      if (result.kind !== 'ok') {
        setSubjects([]);
        setSubjectLoading(false);
        setSubjectState('error');
        return;
      }
      collected.push(...result.subjects);
      if (!result.nextCursor) {
        setSubjects(collected);
        catalogueLoaded.current = true;
        setSubjectLoading(false);
        setSubjectState('ready');
        return;
      }
      cursor = result.nextCursor;
    }
    if (isCurrent()) { setSubjects([]); setSubjectLoading(false); setSubjectState('error'); }
  }

  async function openAddForm() {
    if (dataHousehold.current !== householdId) return;
    const token = ++memberLoadGeneration.current;
    addingRef.current = true;
    setAdding(true);
    subjectGeneration.current++;
    const catalogueToken = subjectGeneration.current;
    const pending = getPendingItemCreate(userId, householdId);
    setPendingCreate(pending);
    if (!pending) { setSubjects([]); setSubjectScope(`${userId}:${householdId}`); setSubjectState('loading'); setSubjectLoading(true); }
    else { setSubjectState('ready'); setSubjectLoading(false); }
    setCreateUncertain(Boolean(pending));
    if (pending) setSubjectLoading(false);
    setCreateNotice(pending ? 'uncertain' : null);
    setRecoveryEditable(false);
    if (!pending) void loadAddSubjects(catalogueToken);
    setMembers([]);
    const collected: Member[] = [];
    let cursor: string | undefined;
    for (let page = 0; page < MAX_MEMBER_PAGES; page++) {
      const result = await listMembers(householdId, cursor);
      if (!mounted.current || token !== memberLoadGeneration.current || !addingRef.current) return;
      if (result.kind === 'unauthenticated') { onSignedOut(); return; }
      if (result.kind !== 'ok') { setMembers([]); return; }
      collected.push(...result.items);
      if (!result.nextCursor) { setMembers(collected); return; }
      cursor = result.nextCursor;
    }
    if (mounted.current && token === memberLoadGeneration.current && addingRef.current) setMembers([]);
  }

  function retryAddSubjects() {
    if (subjectLoading) return;
    const token = ++subjectGeneration.current;
    setSubjectState('loading');
    setSubjectLoading(true);
    void loadAddSubjects(token);
  }

  useEffect(() => {
    if (subjectState === 'loading') return;
    if (catalogueRetryHadFocus.current && (document.activeElement === catalogueRetryRef.current || document.activeElement === document.body)) {
      // A successful retry mounts ItemForm, which focuses its title field itself; an empty catalogue focuses its People and things action.
      if (subjectState === 'error') catalogueRetryRef.current?.focus();
      else noSubjectsRef.current?.focus();
    }
    catalogueRetryHadFocus.current = false;
  }, [subjectState]);

  function cancelAddForm() {
    memberLoadGeneration.current++;
    subjectGeneration.current++;
    addingRef.current = false;
    setAdding(false);
    setSubjectLoading(false);
    returnFocus.current = true;
  }

  useEffect(() => {
    if (!pendingCreate || !adding || names.has(pendingCreate.body.subjectId) || recoveryLookup.current.has(pendingCreate.body.subjectId)) return;
    const id = pendingCreate.body.subjectId;
    const token = subjectGeneration.current;
    const lookup = { active: true };
    const lookupSet = recoveryLookup.current;
    lookupSet.add(id);
    void getSubject(householdId, id).then((result) => {
      if (!lookup.active || !mounted.current || token !== subjectGeneration.current || !addingRef.current) return;
      if (result.kind === 'unauthenticated') { onSignedOut(); return; }
      if (result.kind === 'ok') setSubjects((current) => [...current.filter((entry) => entry.id !== id), result.subject]);
    });
    return () => { lookup.active = false; lookupSet.delete(id); };
  }, [adding, pendingCreate, names, householdId, userId, onSignedOut]);

  useEffect(() => {
    if (!labelRetryPending || labels.loading) return;
    // The retry control stays mounted while its batch runs; afterwards focus follows the outcome unless the user moved it.
    if (document.activeElement === labelRetryRef.current || document.activeElement === document.body) {
      if (labels.hasFailures) labelRetryRef.current?.focus();
      else sectionHeadingRefs.current.get(groups.find((group) => groupData[group.id].items.length)?.id ?? 'needs')?.focus();
    }
    // eslint-disable-next-line react-hooks/set-state-in-effect -- settle the retry once its batch completes.
    setLabelRetryPending(false);
  }, [labelRetryPending, labels.loading, labels.hasFailures, groupData]);

  useEffect(() => {
    if (createNotice === 'reused' || (notice && (focusedRow.current || retryHadFocus.current))) {
      noticeRef.current?.focus();
      focusedRow.current = false;
      retryHadFocus.current = false;
    }
  }, [createNotice, notice]);
  useEffect(() => {
    if (!adding && returnFocus.current) { addRef.current?.focus(); returnFocus.current = false; }
  }, [adding]);
  useEffect(() => {
    const id = focusedMoreGroup.current;
    if (!id || groupData[id].loadingMore) return;
    const button = showMoreButtons.current.get(id);
    if (document.activeElement !== button && document.activeElement !== document.body) { focusedMoreGroup.current = null; return; }
    const allItems = document.querySelector<HTMLAnchorElement>(`[data-group-all="${id}"]`);
    if (groupData[id].items.length >= HOME_GROUP_ROW_CAP && allItems) allItems.focus();
    else if (!button || !groupData[id].cursor) sectionHeadingRefs.current.get(id)?.focus();
    else if (document.activeElement !== button) button.focus();
    focusedMoreGroup.current = null;
  }, [groupData]);

  function beginMutation() {
    if (mutationBusy.current) return false;
    mutationBusy.current = true;
    setBusy(true);
    return true;
  }
  function endMutation() { mutationBusy.current = false; setBusy(false); }

  async function create(values: ItemFormValues) {
    if (!beginMutation()) return { kind: 'handled' as const };
    const recoveryAttempt = createUncertain && !!pendingCreate;
    const hadFocus = document.activeElement === addRef.current;
    try {
      const pending = pendingCreate && !recoveryEditable ? pendingCreate : { version: 1 as const, key: idempotencyKey(), body: { title: values.title, subjectId: values.subjectId, ...(values.attentionOn ? { attentionOn: values.attentionOn } : {}), ...(values.notes ? { notes: values.notes } : {}), recurrence: values.recurrence, ...(values.responsibleUserId !== null ? { responsibleUserId: values.responsibleUserId } : {}), ...(values.recurrence && values.historicalCompletedOn ? { historicalCompletedOn: values.historicalCompletedOn } : {}) } };
      if (!pendingCreate) { setPendingItemCreate(userId, householdId, pending); setPendingCreate(pending); }
      const result = await createItem(householdId, pending.body, pending.key);
      if (result.kind === 'unauthenticated') { clearPendingItemCreate(userId, householdId); setPendingCreate(undefined); onSignedOut(); return { kind: 'handled' as const }; }
      if (result.kind === 'invalid' || result.kind === 'definitive' || result.kind === 'reused') {
        if (result.kind === 'invalid') {
          clearPendingItemCreate(userId, householdId);
          setCreateNotice(null);
          setPendingCreate(undefined);
          setCreateUncertain(false);
          setRecoveryEditable(true);
          setRecoverySubjectId(values.subjectId);
          if (recoveryAttempt && !catalogueLoaded.current && !subjectLoading) {
            const token = ++subjectGeneration.current;
            void loadAddSubjects(token);
          }
          return result;
        }
        clearPendingItemCreate(userId, householdId);
        setPendingCreate(undefined);
        setCreateUncertain(false);
        setRecoveryEditable(false);
        if (result.kind === 'reused') { setCreateNotice('reused'); return { kind: 'handled' as const }; }
        return { kind: 'failed' as const };
      }
      if (result.kind === 'error') { setCreateUncertain(true); setRecoveryEditable(false); setCreateNotice('uncertain'); return { kind: 'handled' as const }; }
      clearPendingItemCreate(userId, householdId);
      setPendingCreate(undefined);
      setCreateUncertain(false);
      setRecoveryEditable(false);
      setCreateNotice(null);
      generation.current++;
      memberLoadGeneration.current++;
      addingRef.current = false;
      returnFocus.current = true;
      setAdding(false);
      await refresh();
      setNotice({ text: t('items.notice.added', { title: result.item.title }) });
      if (hadFocus) requestAnimationFrame(() => addRef.current?.focus());
      return { kind: 'done' as const };
    } finally { endMutation(); }
  }

  async function showMore(id: ItemGroup) {
    const state = groupData[id];
    if (!state.cursor || state.items.length >= HOME_GROUP_ROW_CAP || showMoreInFlight.current.has(id) || !summary || !mounted.current) return;
    const started = generation.current;
    const focused = document.activeElement === showMoreButtons.current.get(id);
    showMoreInFlight.current.add(id);
    focusedMoreGroup.current = focused ? id : null;
    setGroupData((current) => ({ ...current, [id]: { ...current[id], loadingMore: true } }));
    const result = await listItemGroup(householdId, id, summary.evaluatedOn, state.cursor);
    if (!mounted.current || started !== generation.current) return;
    showMoreInFlight.current.delete(id);
    if (result.kind === 'unauthenticated') { onSignedOut(); setGroupData((current) => ({ ...current, [id]: { ...current[id], loadingMore: false } })); }
    else if (result.kind === 'stale') { focusedMoreGroup.current = null; await loadHome({ allowStaleRestart: false }); }
    else if (result.kind === 'ok') {
      setGroupData((current) => {
        const old = current[id];
        const seen = new Set(old.items.map((item) => item.id));
        const merged = [...old.items, ...result.items.filter((item) => !seen.has(item.id))];
        const overflow = old.overflow || merged.length > HOME_GROUP_ROW_CAP;
        const items = merged.slice(0, HOME_GROUP_ROW_CAP);
        return { ...current, [id]: { items, cursor: result.nextCursor, loadingMore: false, overflow } };
      });
    } else {
      setGroupData((current) => ({ ...current, [id]: { ...current[id], loadingMore: false } }));
      setError(true);
    }
    if (focused && mounted.current && started === generation.current && result.kind === 'ok') {
      focusedMoreGroup.current = id;
    }
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
    const focusedGroup = itemGroup(notice.itemId);
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
          else if (focusedGroup && sectionHeadingRefs.current.has(focusedGroup)) sectionHeadingRefs.current.get(focusedGroup)?.focus();
          else if (groups.map((group) => sectionHeadingRefs.current.get(group.id)).find(Boolean)) groups.map((group) => sectionHeadingRefs.current.get(group.id)).find(Boolean)?.focus();
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

  function cancelAddAndFocus() {
    cancelAddForm();
    requestAnimationFrame(() => addRef.current?.focus());
  }

  async function retryNotice() {
    if (!notice) return;
    if (notice.retryKind === 'complete' && notice.itemId) await finish(notice.itemId);
    else if (notice.retryKind === 'undo') await undo();
  }

  return <div className="space-y-6">
    <div ref={noticeRef} tabIndex={-1} className="outline-none">{createNotice === 'reused' ? <div role="status" aria-live="polite" className="flex flex-wrap items-center justify-between gap-2 rounded-xl bg-sand px-4 py-2"><p>{t('items.create.reused')}</p></div> : notice ? <NoticeView notice={notice} busy={busy} onUndo={() => void undo()} onRetry={() => void retryNotice()} onRetryFocus={() => { retryHadFocus.current = true; }} /> : null}</div>
    {error ? <div className="space-y-3"><p className="text-muted">{t('app.error.body')}</p><Button type="button" variant="quiet" onClick={() => void refresh()}>{t('app.error.retry')}</Button></div> : null}
    {loading ? <p role="status" className="text-muted">{t('items.loading')}</p> : null}
    {adding && subjectLoading && !(recoveryEditable && pendingCreate) ? <p role="status" className="text-muted">{t('items.subjects.loading')}</p> : null}
    {adding && subjectState === 'error' && !(recoveryEditable && pendingCreate) ? <div className="space-y-3"><p className="text-muted">{t('items.subjects.error')}</p><Button ref={catalogueRetryRef} type="button" variant="quiet" disabled={subjectLoading} onClick={() => { catalogueRetryHadFocus.current = document.activeElement === catalogueRetryRef.current; retryAddSubjects(); }}>{t('app.error.retry')}</Button><Button type="button" variant="quiet" onClick={cancelAddAndFocus}>{t('items.cancel')}</Button></div> : null}
    {labels.loading && visibleSubjectIDs.some((id) => !names.has(id)) ? <p role="status" className="sr-only">{t('items.subjectLabels.loading')}</p> : null}
    {labels.hasFailures || labelRetryPending ? <div className="flex items-center gap-2"><p className="text-sm text-muted">{t('items.subjectLabels.error')}</p><Button ref={labelRetryRef} type="button" variant="quiet" data-action="subject-label-retry" aria-label={t('items.subjectLabels.retry')} aria-disabled={labelRetryPending} onClick={() => { if (labelRetryPending) return; setLabelRetryPending(true); labels.retry(); }}>{t('app.error.retry')}</Button></div> : null}
    {!loading && !error && summary && groups.every((group) => summary[group.count] === 0) && !adding ? <div className="rounded-3xl border border-line bg-white/70 p-6 sm:p-10"><h2 className="font-display text-2xl">{t('home.empty.title')}</h2><p className="mt-3 text-muted">{t('home.empty.body')}</p></div> : null}
    {!loading && !adding && summary && overviewGroups.length >= 2 ? <nav aria-label={t('items.group.overview')} className="flex flex-wrap gap-x-4 gap-y-2 text-sm">{overviewGroups.map((group) => <a key={group.id} href={`#group-${group.id}`} onClick={(event) => { event.preventDefault(); sectionHeadingRefs.current.get(group.id)?.focus(); }} className="inline-flex min-h-6 items-center rounded-sm underline decoration-line underline-offset-4 hover:decoration-accent focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent">{t(group.key)} {summary[group.count]}</a>)}</nav> : null}
    {itemsRequestComplete && !adding ? <Button ref={addRef} data-action="completion-trigger" type="button" onClick={openAddForm} className="w-full sm:w-auto">{t('items.add')}</Button> : null}
    {adding && ((createUncertain && pendingCreate) || recoveryEditable || (subjectState === 'ready' && activeSubjects.some((subject) => !subject.archived))) ? <ItemForm busy={busy} members={members} subjects={createUncertain && pendingCreate || recoveryEditable ? recoverySubjects : activeSubjects.filter((subject) => !subject.archived)} initialValues={createUncertain && pendingCreate ? { title: pendingCreate.body.title, subjectId: pendingCreate.body.subjectId, attentionOn: pendingCreate.body.attentionOn ?? '', notes: pendingCreate.body.notes ?? '', recurrence: pendingCreate.body.recurrence ?? null, responsibleUserId: pendingCreate.body.responsibleUserId ?? null, ...(pendingCreate.body.historicalCompletedOn ? { historicalCompletedOn: pendingCreate.body.historicalCompletedOn } : {}) } : retryDraft} locked={createUncertain && !!pendingCreate} preserveDraftOnUnlock={recoveryEditable} retryLabel={t('items.create.checkAgain')} submittingLabel={t('items.add.submitting')} notice={createUncertain ? t('items.create.uncertain') : undefined} onSubmit={create} onCancel={cancelAddAndFocus} /> : null}
    {adding && !(createUncertain && pendingCreate) && !recoveryEditable && subjectState === 'ready' && !activeSubjects.some((subject) => !subject.archived) ? <div className="rounded-2xl border border-line bg-white/70 p-5"><p>{t('items.noSubjects')}</p><Button ref={noSubjectsRef} type="button" variant="quiet" className="mt-3" onClick={() => { cancelAddForm(); addRef.current?.focus(); onOpenPeople(); }}>{t('home.people')}</Button><Button type="button" variant="quiet" className="mt-3" onClick={cancelAddAndFocus}>{t('items.cancel')}</Button></div> : null}
    {adding && subjectLoading && !(createUncertain && pendingCreate) ? <Button type="button" variant="quiet" className="mt-3" onClick={cancelAddAndFocus}>{t('items.cancel')}</Button> : null}
    {!loading && summary ? groups.map((group) => {
      const state = groupData[group.id];
      if (!state.items.length) return null;
      return <section key={group.id} id={`group-${group.id}`} className={group.quiet ? 'border-t border-line pt-5' : ''}><h2 ref={(node) => { if (node) sectionHeadingRefs.current.set(group.id, node); else sectionHeadingRefs.current.delete(group.id); }} data-group-heading={group.id} tabIndex={-1} className={`font-display ${group.quiet ? 'text-xl text-muted' : 'text-2xl'}`}>{t(group.key)} <span className="text-base font-normal text-muted">{t(countPluralKey(summary[group.count], locale), { count: String(summary[group.count]) })}</span></h2><ul className="mt-3 divide-y divide-line rounded-2xl border border-line bg-white/70">{state.items.map((item) => <ItemRow key={item.id} item={item} group={group.id} subjectName={names.get(item.subjectId) ?? (labels.stateFor(item.subjectId) === 'queued' || labels.stateFor(item.subjectId) === 'loading' ? t('items.subjectLabels.loading') : t('items.subject.unknown'))} locale={locale} userId={userId} members={homeMembers} membersLoaded={homeMembersLoaded} busy={busy} onDone={() => void finish(item.id)} onOpenItem={onOpenItem} />)}</ul>{state.cursor && state.items.length < HOME_GROUP_ROW_CAP ? <Button ref={(node) => { if (node) showMoreButtons.current.set(group.id, node); else showMoreButtons.current.delete(group.id); }} type="button" data-group-more={group.id} aria-disabled={state.loadingMore} variant="quiet" onClick={() => { if (!state.loadingMore) void showMore(group.id); }}>{state.loadingMore ? t('items.moreLoading') : t('items.showMore', { count: String(Math.max(0, summary[group.count] - state.items.length)) })}</Button> : null}{state.items.length >= HOME_GROUP_ROW_CAP && (state.cursor || state.overflow || summary[group.count] > state.items.length) ? <NavLink href={`/items?status=${group.id}`} data-group-all={group.id} onNavigate={() => onOpenAllItems(group.id)} className="inline-flex min-h-11 items-center underline decoration-line underline-offset-4 hover:decoration-accent focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent">{summary[group.count] > state.items.length ? t('items.group.seeAll', { count: String(summary[group.count]) }) : t('items.group.seeAllNoCount')}</NavLink> : null}</section>;
    }) : null}
  </div>;
}
