import { Heading } from '../../app/Heading';
import { SubjectList } from './SubjectList';
import { SubjectNotice } from './SubjectNotice';
import { SubjectsViewToggle } from './SubjectsViewToggle';
import { SubjectsStatus } from './SubjectsStatus';
import type { Editing, ListState, Notice } from './subjectsModel';
import type { Member } from '../members/membersApi';
import type { Subject } from './subjectsApi';
import type { SubjectValues, SubmitOutcome } from './SubjectForm';
import type { MemberPersonTransferApi } from './useMemberPersonTransfer';
import type { MessageKey } from '../../i18n';
import { pendingFormInitial } from './subjectsModel';

export function SubjectsScreenContent({
  showArchived, list, notice, noticeRef, interactionLocked, actionFailed, retryLoad, adding, empty, items,
  cursorStack, members, membersLoaded, editing, transfer, createUncertain, pendingCreate, t, yourEntry, addDisabled, startAdding,
  cancelAdding, add, startEditing, changeArchived, save, closeEditing, previousPage, nextPage, switchView, undoArchive,
}: {
  showArchived: boolean; list: ListState; notice: Notice | null | undefined; noticeRef: React.MutableRefObject<HTMLDivElement | null>;
  interactionLocked: boolean; addDisabled: boolean; actionFailed: boolean; retryLoad: () => void; adding: boolean; empty: boolean;
  items: Subject[]; cursorStack: string[]; members: Member[]; membersLoaded: boolean; editing: Editing | null;
  transfer: MemberPersonTransferApi;
  createUncertain: boolean; pendingCreate: import('./pendingSubjectCreate').PendingSubjectCreate | undefined;
  t: (key: MessageKey, values?: Record<string, string>) => string; yourEntry?: string;
  startAdding: () => void; cancelAdding: () => void; add: (values: SubjectValues) => Promise<SubmitOutcome>;
  startEditing: (subject: Subject) => void; changeArchived: (subject: Subject, archived: boolean, undo: boolean) => void;
  undoArchive: (notice: Notice & { kind: 'archived' | 'restored' }) => void;
  save: (values: SubjectValues) => Promise<SubmitOutcome>; closeEditing: (id: string) => void;
  previousPage: (event: React.MouseEvent<HTMLButtonElement>) => void; nextPage: (event: React.MouseEvent<HTMLButtonElement>) => void;
  switchView: () => void;
}) {
  return <section className="settle min-w-0">
    <header className="min-w-0">
      <p className="text-[0.68rem] font-semibold uppercase tracking-[0.18em] text-muted">{t('subjects.eyebrow')}</p>
      <Heading className="mt-2 text-h1 leading-tight text-ink">{t('subjects.title')}</Heading>
      <p className="mt-2 max-w-prose text-base text-muted">{t('subjects.subtitle')}</p>
    </header>
    <div className="mt-6 min-w-0">
    {showArchived && list.kind !== 'noHousehold' ? <div className="mb-5 min-w-0">
      <h2 className="text-h2 font-bold leading-snug text-ink">{t('subjects.archived.title')}</h2>
      <p className="mt-1 max-w-prose text-muted">{t('subjects.archived.body')}</p>
    </div> : null}
    <SubjectNotice notice={notice} noticeRef={noticeRef} interactionLocked={interactionLocked} t={t}
      onUndo={undoArchive} />
    {actionFailed ? <p role="alert" className="mt-4 text-danger">{t('subjects.error.action')}</p> : null}
    <SubjectsStatus list={list} t={t} onRetry={retryLoad} />
    <SubjectList list={list} showArchived={showArchived} adding={adding} empty={empty} items={items}
      cursorStack={cursorStack} members={members} membersLoaded={membersLoaded} editing={editing} transfer={transfer}
      createUncertain={createUncertain} createLocked={createUncertain && !!pendingCreate}
      pendingInitial={pendingFormInitial(createUncertain, pendingCreate)}
      interactionLocked={interactionLocked} addDisabled={addDisabled} t={t} yourEntry={yourEntry} onAdd={startAdding} onCancelAdd={cancelAdding}
      onAddSubmit={add} onStartEditing={startEditing} onArchive={changeArchived} onRestore={changeArchived}
      onSave={save} onCancelEditing={closeEditing} onPrevious={previousPage} onNext={nextPage} />
    <SubjectsViewToggle showArchived={showArchived} listKind={list.kind} t={t} onSwitchView={switchView} />
    </div>
  </section>;
}
