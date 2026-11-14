import { Heading } from '../../app/Heading';
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
  // One compact band: the screen name and its single primary action on the same line, as on Home. It never wraps, so
  // the title wraps its own words at 320 px instead of pushing Add onto a second line. It comes first in the DOM, so
  // the Add control is also the first thing a keyboard user reaches.
  return <section className="settle flex min-w-0 flex-col">
    <div className="min-w-0 max-w-5xl">
      <div className="flex min-w-0 items-center justify-between gap-x-4">
        <Heading className="min-w-0 text-h1 font-semibold text-ink">{t('subjects.title')}</Heading>
        {canAdd ? <div className="shrink-0">
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
      {showArchived && list.kind !== 'noHousehold' ? <h2 className="text-h2 font-bold leading-snug text-ink">
        {t('subjects.archived.title')}
      </h2> : null}
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
