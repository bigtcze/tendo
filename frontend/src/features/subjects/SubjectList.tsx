import { Button } from '../../components/ui/button';
import { SubjectsEmpty } from './SubjectsEmpty';
import { SubjectRow } from './SubjectRow';
import type { Editing, ListState } from './subjectsModel';
import type { Member } from '../members/membersApi';
import type { Subject } from './subjectsApi';
import type { MemberPersonTransferApi } from './useMemberPersonTransfer';
import type { SubjectValues, SubmitOutcome } from './SubjectForm';
import type { MessageKey } from '../../i18n';

export function SubjectList({ list, showArchived, adding, empty, items, cursorStack, members, membersLoaded, editing, transfer,
  interactionLocked, t, yourEntry, onStartEditing, onArchive, onRestore, onSave, onCancelEditing, onPrevious, onNext }: {
  list: ListState; showArchived: boolean; adding: boolean; empty: boolean; items: Subject[]; cursorStack: string[];
  members: Member[]; membersLoaded: boolean; editing: Editing | null; transfer: MemberPersonTransferApi;
  interactionLocked: boolean; t: (key: MessageKey, values?: Record<string, string>) => string; yourEntry?: string;
  onStartEditing: (subject: Subject) => void;
  onArchive: (subject: Subject, archived: boolean, undo: boolean) => void; onRestore: (subject: Subject, archived: boolean, undo: boolean) => void;
  onSave: (values: SubjectValues) => Promise<SubmitOutcome>; onCancelEditing: (id: string) => void;
  onPrevious: (event: React.MouseEvent<HTMLButtonElement>) => void; onNext: (event: React.MouseEvent<HTMLButtonElement>) => void;
}) {
  if (list.kind !== 'ready') return null;
  return <div className="mt-5 space-y-4">
    {empty ? <SubjectsEmpty showArchived={showArchived} cursorStackEmpty={cursorStack.length === 0} adding={adding} t={t} /> : null}
    {items.length > 0 ? <ul className="divide-y divide-line border-t border-line">
      {items.map((subject) => <SubjectRow key={subject.id} subject={subject} editing={editing} transfer={transfer} members={members}
        membersLoaded={membersLoaded} showArchived={showArchived} interactionLocked={interactionLocked} t={t} yourEntry={yourEntry}
        onStartEditing={onStartEditing} onArchive={onArchive} onRestore={onRestore} onSave={onSave} onCancelEditing={onCancelEditing} />)}
    </ul> : null}
    {cursorStack.length > 0 || list.nextCursor ? <nav aria-label={t('subjects.pages')}
      className="flex flex-wrap items-center justify-between gap-2">
      {cursorStack.length > 0 ? <Button data-subject-page type="button" variant="quiet" onClick={onPrevious} disabled={interactionLocked}>
        {cursorStack.length > 1 ? t('subjects.page.previous') : t('subjects.page.first')}
      </Button> : <span />}
      <p aria-live="polite" aria-atomic="true" className="text-meta text-muted">{t('subjects.page.number', { n: String(cursorStack.length + 1) })}</p>
      {list.nextCursor ? <Button data-subject-page type="button" variant="quiet" onClick={onNext}
        disabled={interactionLocked}>{t('subjects.page.next')}</Button> : null}
    </nav> : null}
  </div>;
}
