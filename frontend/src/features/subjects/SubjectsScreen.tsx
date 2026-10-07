import { useCallback, useEffect, useRef, useState } from 'react';
import { AccountActions } from '../../app/AccountActions';
import { Heading } from '../../app/Heading';
import { Layout } from '../../app/Layout';
import { NavLink } from '../../app/NavLink';
import { Button } from '../../components/ui/button';
import { useI18n } from '../../i18n';
import { SubjectForm, typeLabelKey, type SubjectValues, type SubmitOutcome } from './SubjectForm';
import {
  createSubject,
  getSubject,
  listSubjects,
  patchSubject,
  setArchived,
  type PatchResult,
  type Subject,
} from './subjectsApi';

type ListState =
  | { kind: 'loading' }
  | { kind: 'error' }
  | { kind: 'noHousehold' }
  | { kind: 'ready'; items: Subject[]; nextCursor: string | null };

type Notice =
  | { kind: 'added'; subject: Subject }
  | { kind: 'archived'; subject: Subject }
  | { kind: 'restored'; subject: Subject }
  | { kind: 'gone' }
  | { kind: 'changed' };

type Editing = { subject: Subject; etag: string; conflict: boolean; mine?: SubjectValues };

function mergeById(current: Subject[], more: Subject[]): Subject[] {
  const seen = new Set(current.map((s) => s.id));
  return [...current, ...more.filter((s) => !seen.has(s.id))];
}

/** Ids are UUIDv7, so id order is creation order, the same order the server lists in. */
function insertById(current: Subject[], subject: Subject): Subject[] {
  return mergeById(current, [subject]).sort((a, b) => (a.id < b.id ? -1 : a.id > b.id ? 1 : 0));
}

function replaceById(items: Subject[], subject: Subject): Subject[] {
  return items.map((s) => (s.id === subject.id ? subject : s));
}

const rowLink =
  '-ml-3 inline-flex min-h-11 items-center rounded-xl px-3 text-muted hover:bg-sand hover:text-ink focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent';

export function SubjectsScreen({
  householdId,
  login,
  onBack,
  onSignedOut,
}: {
  householdId: string;
  login: string;
  onBack: () => void;
  onSignedOut: () => void;
}) {
  const { t } = useI18n();
  const [showArchived, setShowArchived] = useState(false);
  const [list, setList] = useState<ListState>({ kind: 'loading' });
  const [attempt, setAttempt] = useState(0);
  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState<Editing | null>(null);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [notice, setNotice] = useState<Notice | null>(null);
  const [actionFailed, setActionFailed] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const busy = useRef(false);
  const noticeRef = useRef<HTMLDivElement>(null);
  // Bumped whenever the view changes or the list is reloaded. Async work captures it before awaiting
  // and drops its result if it changed, so a slow response never lands in the wrong view.
  const generation = useRef(0);
  // Element (data-focus value) that should take focus once it is on screen.
  const focusKey = useRef<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    listSubjects(householdId, { archived: showArchived }).then((result) => {
      if (cancelled) return;
      if (result.kind === 'unauthenticated') onSignedOut();
      else if (result.kind === 'ok') setList({ kind: 'ready', items: result.items, nextCursor: result.nextCursor });
      else if (result.kind === 'noHousehold') setList({ kind: 'noHousehold' });
      else setList({ kind: 'error' });
    });
    return () => {
      cancelled = true;
    };
  }, [householdId, showArchived, attempt, onSignedOut]);

  // Moves focus to the confirmation so keyboard and screen reader users are not left on a removed button.
  useEffect(() => {
    if (notice) noticeRef.current?.focus();
  }, [notice]);

  useEffect(() => {
    const key = focusKey.current;
    if (!key) return;
    const el = document.querySelector<HTMLElement>(`[data-focus="${key}"]`);
    if (el) {
      el.focus();
      focusKey.current = null;
    }
  });

  const reload = useCallback(() => {
    generation.current += 1;
    setAttempt((n) => n + 1);
  }, []);

  function retryLoad() {
    setList({ kind: 'loading' });
    reload();
  }

  function switchView(archived: boolean) {
    generation.current += 1;
    focusKey.current = null;
    setShowArchived(archived);
    setList({ kind: 'loading' });
    setAdding(false);
    setEditing(null);
    setNotice(null);
    setActionFailed(false);
  }

  function startAdding() {
    setEditing(null);
    setNotice(null);
    setActionFailed(false);
    setAdding(true);
  }

  function cancelAdding() {
    focusKey.current = 'add';
    setAdding(false);
  }

  function closeEditing(id: string) {
    focusKey.current = `row:${id}`;
    setEditing(null);
  }

  function gone() {
    setEditing(null);
    setNotice({ kind: 'gone' });
    reload();
  }

  function changedMeanwhile() {
    setEditing(null);
    setNotice({ kind: 'changed' });
    reload();
  }

  function updateRow(subject: Subject) {
    setList((current) => (current.kind === 'ready' ? { ...current, items: replaceById(current.items, subject) } : current));
  }

  async function guarded(id: string, work: () => Promise<void>) {
    if (busy.current) return;
    busy.current = true;
    setBusyId(id);
    setActionFailed(false);
    try {
      await work();
    } finally {
      busy.current = false;
      setBusyId(null);
    }
  }

  function startEditing(subject: Subject) {
    const started = generation.current;
    void guarded(subject.id, async () => {
      setNotice(null);
      const result = await getSubject(householdId, subject.id);
      if (result.kind === 'unauthenticated') {
        onSignedOut();
        return;
      }
      if (started !== generation.current) return;
      if (result.kind === 'ok') {
        updateRow(result.subject);
        setAdding(false);
        setEditing({ subject: result.subject, etag: result.etag, conflict: false });
      } else if (result.kind === 'notFound') gone();
      else setActionFailed(true);
    });
  }

  function changeArchived(subject: Subject, archived: boolean, isUndo: boolean) {
    const started = generation.current;
    void guarded(subject.id, async () => {
      setNotice(null);
      const result = await setArchived(householdId, subject.id, archived);
      if (result.kind === 'unauthenticated') {
        onSignedOut();
        return;
      }
      if (started !== generation.current) return;
      if (result.kind === 'ok') {
        if (isUndo) {
          // Put the row back where it belongs and land on it, rather than refetching the whole list.
          focusKey.current = `row:${subject.id}`;
          setList((current) =>
            current.kind === 'ready' ? { ...current, items: insertById(current.items, result.subject) } : current,
          );
        } else {
          setList((current) =>
            current.kind === 'ready'
              ? { ...current, items: current.items.filter((s) => s.id !== subject.id) }
              : current,
          );
          setNotice(archived ? { kind: 'archived', subject } : { kind: 'restored', subject });
        }
      } else if (result.kind === 'notFound') gone();
      else if (result.kind === 'conflict') changedMeanwhile();
      else setActionFailed(true);
    });
  }

  function undoArchive(done: Notice & { kind: 'archived' | 'restored' }) {
    changeArchived(done.subject, done.kind === 'restored', true);
  }

  async function add(values: SubjectValues): Promise<SubmitOutcome> {
    const started = generation.current;
    const result = await createSubject(householdId, values);
    if (result.kind === 'unauthenticated') {
      onSignedOut();
      return { kind: 'handled' };
    }
    if (started !== generation.current) return { kind: 'handled' };
    if (result.kind === 'ok') {
      setList((current) =>
        current.kind === 'ready' ? { ...current, items: mergeById(current.items, [result.subject]) } : current,
      );
      setAdding(false);
      setNotice({ kind: 'added', subject: result.subject });
      return { kind: 'done' };
    }
    if (result.kind === 'invalid') return result;
    return { kind: 'failed' };
  }

  async function save(values: SubjectValues): Promise<SubmitOutcome> {
    if (!editing) return { kind: 'handled' };
    const started = generation.current;
    const { subject, etag } = editing;
    const changes = {
      ...(values.name !== subject.name ? { name: values.name } : {}),
      ...(values.type !== subject.type ? { type: values.type } : {}),
    };
    if (Object.keys(changes).length === 0) {
      closeEditing(subject.id);
      return { kind: 'done' };
    }
    const result: PatchResult = await patchSubject(householdId, subject.id, etag, changes);
    if (result.kind === 'unauthenticated') {
      onSignedOut();
      return { kind: 'handled' };
    }
    if (started !== generation.current) return { kind: 'handled' };
    switch (result.kind) {
      case 'ok':
        updateRow(result.subject);
        closeEditing(subject.id);
        return { kind: 'done' };
      case 'conflict': {
        const latest = await getSubject(householdId, subject.id);
        if (latest.kind === 'unauthenticated') {
          onSignedOut();
          return { kind: 'handled' };
        }
        if (started !== generation.current) return { kind: 'handled' };
        if (latest.kind === 'ok') {
          updateRow(latest.subject);
          setEditing({ subject: latest.subject, etag: latest.etag, conflict: true, mine: values });
          return { kind: 'handled' };
        }
        if (latest.kind === 'notFound') {
          gone();
          return { kind: 'handled' };
        }
        return { kind: 'failed' };
      }
      case 'notFound':
        gone();
        return { kind: 'handled' };
      case 'invalid':
        return result;
      default:
        return { kind: 'failed' };
    }
  }

  async function showMore() {
    if (list.kind !== 'ready' || !list.nextCursor || loadingMore) return;
    const started = generation.current;
    const known = new Set(list.items.map((s) => s.id));
    setLoadingMore(true);
    setActionFailed(false);
    const result = await listSubjects(householdId, { archived: showArchived, cursor: list.nextCursor });
    setLoadingMore(false);
    if (result.kind === 'unauthenticated') {
      onSignedOut();
      return;
    }
    if (started !== generation.current) return;
    if (result.kind === 'ok') {
      const first = result.items.find((s) => !known.has(s.id));
      if (first) focusKey.current = `row:${first.id}`;
      setList((current) =>
        current.kind === 'ready'
          ? { kind: 'ready', items: mergeById(current.items, result.items), nextCursor: result.nextCursor }
          : current,
      );
    } else setActionFailed(true);
  }

  const items = list.kind === 'ready' ? list.items : [];
  const empty = list.kind === 'ready' && items.length === 0;

  const mine = editing?.mine;
  const yourEntry =
    mine && (mine.name !== editing.subject.name || mine.type !== editing.subject.type)
      ? t('subjects.notice.yourEntry', { name: `${mine.name.trim()} (${t(typeLabelKey(mine.type))})` })
      : undefined;

  return (
    <Layout actions={<AccountActions login={login} onSignedOut={onSignedOut} />}>
      <section className="settle">
        <NavLink href="/" onNavigate={onBack} className={rowLink}>
          {t('subjects.backHome')}
        </NavLink>
        <Heading className="mt-2 font-display text-3xl leading-tight sm:text-4xl">{t('subjects.title')}</Heading>
        <p className="mt-2 text-muted">{t('subjects.subtitle')}</p>

        {showArchived && list.kind !== 'noHousehold' ? (
          <div className="mt-8">
            <h2 className="font-display text-2xl leading-snug">{t('subjects.archived.title')}</h2>
            <p className="mt-1 text-muted">{t('subjects.archived.body')}</p>
          </div>
        ) : null}

        <div ref={noticeRef} tabIndex={-1} role="status" className="mt-6 outline-none empty:mt-0">
          {notice ? (
            <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1 rounded-xl bg-sand px-4 py-1 text-ink">
              <p className="py-2">
                {notice.kind === 'gone' || notice.kind === 'changed'
                  ? t(`subjects.notice.${notice.kind}`)
                  : t(`subjects.notice.${notice.kind}`, { name: notice.subject.name })}
              </p>
              {notice.kind === 'archived' || notice.kind === 'restored' ? (
                <Button
                  type="button"
                  variant="quiet"
                  size="small"
                  disabled={busyId !== null}
                  onClick={() => undoArchive(notice)}
                >
                  {t('subjects.undo')}
                </Button>
              ) : null}
            </div>
          ) : null}
        </div>

        {actionFailed ? (
          <p role="alert" className="mt-4 text-danger">
            {t('subjects.error.action')}
          </p>
        ) : null}

        {list.kind === 'loading' ? (
          <p role="status" className="mt-8 text-muted">
            {t('subjects.loading')}
          </p>
        ) : null}

        {list.kind === 'error' ? (
          <div className="mt-8 space-y-3">
            <p className="text-lg">{t('app.error.title')}</p>
            <p className="text-muted">{t('app.error.body')}</p>
            <Button type="button" onClick={retryLoad}>
              {t('app.error.retry')}
            </Button>
          </div>
        ) : null}

        {list.kind === 'noHousehold' ? (
          <div className="mt-8 space-y-2">
            <h2 className="font-display text-2xl leading-snug">{t('home.noHousehold.title')}</h2>
            <p className="text-muted">{t('home.noHousehold.body')}</p>
          </div>
        ) : null}

        {list.kind === 'ready' ? (
          <div className="mt-8 space-y-6">
            {!showArchived && !adding && !empty ? (
              <Button type="button" data-focus="add" onClick={startAdding} className="w-full sm:w-auto">
                {t('subjects.add')}
              </Button>
            ) : null}

            {adding ? (
              <SubjectForm
                label={t('subjects.add.formLabel')}
                initial={{ name: '', type: 'person' }}
                submitLabel={t('subjects.add.submit')}
                pendingLabel={t('subjects.add.submitting')}
                onSubmit={add}
                onCancel={cancelAdding}
              />
            ) : null}

            {empty && !adding ? (
              <div className="rounded-3xl border border-line bg-white/70 p-6 sm:p-10">
                <h2 className="font-display text-2xl leading-snug">
                  {showArchived ? t('subjects.emptyArchived') : t('subjects.empty.title')}
                </h2>
                {showArchived ? null : (
                  <>
                    <p className="mt-3 text-muted">{t('subjects.empty.body')}</p>
                    <Button type="button" data-focus="add" onClick={startAdding} className="mt-6 w-full sm:w-auto">
                      {t('subjects.add')}
                    </Button>
                  </>
                )}
              </div>
            ) : null}

            {items.length > 0 ? (
              <ul className="divide-y divide-line rounded-2xl border border-line bg-white/70">
                {items.map((subject) => (
                  <li key={subject.id} className="p-3 sm:px-5">
                    {editing && editing.subject.id === subject.id ? (
                      <SubjectForm
                        key={`${subject.id}:${editing.etag}`}
                        label={t('subjects.edit.label', { name: subject.name })}
                        initial={{ name: editing.subject.name, type: editing.subject.type }}
                        submitLabel={t('subjects.save')}
                        pendingLabel={t('subjects.saving')}
                        notice={editing.conflict ? t('subjects.notice.conflict') : undefined}
                        yourEntry={yourEntry}
                        onSubmit={save}
                        onCancel={() => closeEditing(subject.id)}
                      />
                    ) : (
                      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1">
                        <div className="min-w-0 flex-1 basis-40">
                          <p className="break-words text-lg leading-snug" data-subject-name>
                            {subject.name}
                          </p>
                          <p className="text-sm text-muted" data-subject-type>
                            {t(typeLabelKey(subject.type))}
                          </p>
                        </div>
                        <div className="flex items-center gap-1">
                          {showArchived ? (
                            <Button
                              type="button"
                              variant="quiet"
                              size="small"
                              data-focus={`row:${subject.id}`}
                              aria-label={t('subjects.restore.label', { name: subject.name })}
                              disabled={busyId !== null}
                              onClick={() => changeArchived(subject, false, false)}
                            >
                              {t('subjects.restore')}
                            </Button>
                          ) : (
                            <>
                              <Button
                                type="button"
                                variant="quiet"
                                size="small"
                                data-focus={`row:${subject.id}`}
                                aria-label={t('subjects.edit.label', { name: subject.name })}
                                disabled={busyId !== null}
                                onClick={() => startEditing(subject)}
                              >
                                {t('subjects.edit')}
                              </Button>
                              <Button
                                type="button"
                                variant="quiet"
                                size="small"
                                aria-label={t('subjects.archive.label', { name: subject.name })}
                                disabled={busyId !== null}
                                onClick={() => changeArchived(subject, true, false)}
                              >
                                {t('subjects.archive')}
                              </Button>
                            </>
                          )}
                        </div>
                      </div>
                    )}
                  </li>
                ))}
              </ul>
            ) : null}

            {list.nextCursor ? (
              <Button type="button" variant="quiet" onClick={showMore} disabled={loadingMore}>
                {loadingMore ? t('subjects.moreLoading') : t('subjects.more')}
              </Button>
            ) : null}
          </div>
        ) : null}

        {/* Kept mounted while a view loads, so keyboard focus stays on it when the view changes. */}
        {list.kind === 'ready' || list.kind === 'loading' ? (
          <div className="mt-6 border-t border-line pt-4">
            <Button type="button" variant="quiet" size="small" onClick={() => switchView(!showArchived)}>
              {showArchived ? t('subjects.showCurrent') : t('subjects.showArchived')}
            </Button>
          </div>
        ) : null}
      </section>
    </Layout>
  );
}
