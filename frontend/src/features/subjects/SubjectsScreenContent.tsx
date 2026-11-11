import { Heading } from '../../app/Heading';
import { Eyebrow } from '../../components/ui/eyebrow';
import { SubjectAddAction } from './SubjectAddAction';
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
  const canAdd = list.kind === 'ready' && !showArchived;
  // Header band first, then the notice: DOM order matches what a reader sees, so the Add control is also the
  // first thing a keyboard user reaches, as on Home.
  return <section className="settle flex min-w-0 flex-col">
    <div className="min-w-0 max-w-5xl">
      <div className="grid gap-y-4 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-end lg:gap-x-8 lg:gap-y-2">
        <div className="col-start-1 row-start-1 min-w-0">
          <Eyebrow>{t('subjects.eyebrow')}</Eyebrow>
          <Heading className="mt-2 text-h1 leading-tight text-ink">{t('subjects.title')}</Heading>
        </div>
        <p className="col-start-1 row-start-2 max-w-prose text-base text-muted">{t('subjects.subtitle')}</p>
        {canAdd ? <div className="col-start-1 row-start-3 shrink-0 lg:col-start-2 lg:row-start-1">
          <SubjectAddAction adding={adding} createUncertain={createUncertain} createLocked={createUncertain && !!pendingCreate}
            pendingInitial={pendingFormInitial(createUncertain, pendingCreate)} addDisabled={addDisabled} t={t}
            onAdd={startAdding} onCancelAdd={cancelAdding} onAddSubmit={add} />
        </div> : null}
      </div>
    </div>
    <div className="min-w-0 max-w-5xl">
      <SubjectNotice notice={notice} noticeRef={noticeRef} interactionLocked={interactionLocked} t={t} onUndo={undoArchive} />
    </div>
    <div className="mt-6 min-w-0 max-w-5xl">
      {showArchived && list.kind !== 'noHousehold' ? <div className="mb-5 min-w-0">
        <h2 className="text-h2 font-bold leading-snug text-ink">{t('subjects.archived.title')}</h2>
        <p className="mt-1 max-w-prose text-muted">{t('subjects.archived.body')}</p>
      </div> : null}
      {actionFailed ? <p role="alert" className="mt-4 text-danger">{t('subjects.error.action')}</p> : null}
      <SubjectsStatus list={list} t={t} onRetry={retryLoad} />
      <SubjectList list={list} showArchived={showArchived} adding={adding} empty={empty} items={items}
        cursorStack={cursorStack} members={members} membersLoaded={membersLoaded} editing={editing} transfer={transfer}
        interactionLocked={interactionLocked} t={t} yourEntry={yourEntry}
        onStartEditing={startEditing} onArchive={changeArchived} onRestore={changeArchived}
        onSave={save} onCancelEditing={closeEditing} onPrevious={previousPage} onNext={nextPage} />
      <SubjectsViewToggle showArchived={showArchived} listKind={list.kind} t={t} onSwitchView={switchView} />
    </div>
  </section>;
}
