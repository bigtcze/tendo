import { Heading } from '../../app/Heading';
import { SubjectList } from './SubjectList';
import { SubjectNotice } from './SubjectNotice';
import { SubjectsViewToggle } from './SubjectsViewToggle';
import { SubjectsStatus } from './SubjectsStatus';
import type { Editing, ListState, Notice } from './subjectsModel';
import type { Member } from '../members/membersApi';
import type { Subject } from './subjectsApi';
import type { SubjectValues, SubmitOutcome } from './SubjectForm';
import type { MessageKey } from '../../i18n';
import { pendingFormInitial } from './subjectsModel';

export function SubjectsScreenContent({
  showArchived, list, notice, noticeRef, interactionLocked, actionFailed, retryLoad, adding, empty, items,
  cursorStack, members, membersLoaded, editing, createUncertain, pendingCreate, t, yourEntry, addDisabled, startAdding,
  cancelAdding, add, startEditing, changeArchived, save, closeEditing, previousPage, nextPage, switchView, undoArchive,
}: {
  showArchived: boolean; list: ListState; notice: Notice | null | undefined; noticeRef: React.MutableRefObject<HTMLDivElement | null>;
  interactionLocked: boolean; addDisabled: boolean; actionFailed: boolean; retryLoad: () => void; adding: boolean; empty: boolean;
  items: Subject[]; cursorStack: string[]; members: Member[]; membersLoaded: boolean; editing: Editing | null;
  createUncertain: boolean; pendingCreate: import('./pendingSubjectCreate').PendingSubjectCreate | undefined;
  t: (key: MessageKey, values?: Record<string, string>) => string; yourEntry?: string;
  startAdding: () => void; cancelAdding: () => void; add: (values: SubjectValues) => Promise<SubmitOutcome>;
  startEditing: (subject: Subject) => void; changeArchived: (subject: Subject, archived: boolean, undo: boolean) => void;
  undoArchive: (notice: Notice & { kind: 'archived' | 'restored' }) => void;
  save: (values: SubjectValues) => Promise<SubmitOutcome>; closeEditing: (id: string) => void;
  previousPage: (event: React.MouseEvent<HTMLButtonElement>) => void; nextPage: (event: React.MouseEvent<HTMLButtonElement>) => void;
  switchView: () => void;
}) {
  return <section className="settle">
    <Heading className="font-display text-h1 leading-tight">{t('subjects.title')}</Heading>
    <p className="mt-2 text-muted">{t('subjects.subtitle')}</p>
    {showArchived && list.kind !== 'noHousehold' ? <div className="mt-8">
      <h2 className="font-display text-2xl leading-snug">{t('subjects.archived.title')}</h2>
      <p className="mt-1 text-muted">{t('subjects.archived.body')}</p>
    </div> : null}
    <SubjectNotice notice={notice} noticeRef={noticeRef} interactionLocked={interactionLocked} t={t}
      onUndo={undoArchive} />
    {actionFailed ? <p role="alert" className="mt-4 text-danger">{t('subjects.error.action')}</p> : null}
    <SubjectsStatus list={list} t={t} onRetry={retryLoad} />
    <SubjectList list={list} showArchived={showArchived} adding={adding} empty={empty} items={items}
      cursorStack={cursorStack} members={members} membersLoaded={membersLoaded} editing={editing}
      createUncertain={createUncertain} createLocked={createUncertain && !!pendingCreate}
      pendingInitial={pendingFormInitial(createUncertain, pendingCreate)}
      interactionLocked={interactionLocked} addDisabled={addDisabled} t={t} yourEntry={yourEntry} onAdd={startAdding} onCancelAdd={cancelAdding}
      onAddSubmit={add} onStartEditing={startEditing} onArchive={changeArchived} onRestore={changeArchived}
      onSave={save} onCancelEditing={closeEditing} onPrevious={previousPage} onNext={nextPage} />
    <SubjectsViewToggle showArchived={showArchived} listKind={list.kind} t={t} onSwitchView={switchView} />
  </section>;
}
