import { Button } from '../../components/ui/button';
import { SubjectForm, type SubjectValues, type SubmitOutcome } from './SubjectForm';
import { SubjectsEmpty } from './SubjectsEmpty';
import { SubjectRow } from './SubjectRow';
import type { Editing, ListState } from './subjectsModel';
import type { Member } from '../members/membersApi';
import type { Subject } from './subjectsApi';
import type { MessageKey } from '../../i18n';

export function SubjectList({ list, showArchived, adding, empty, items, cursorStack, members, membersLoaded, editing,
  createUncertain, createLocked, pendingInitial, interactionLocked, addDisabled, t, yourEntry, onAdd, onCancelAdd, onAddSubmit,
  onStartEditing, onArchive, onRestore, onSave, onCancelEditing, onPrevious, onNext }: {
  list: ListState; showArchived: boolean; adding: boolean; empty: boolean; items: Subject[]; cursorStack: string[];
  members: Member[]; membersLoaded: boolean; editing: Editing | null; createUncertain: boolean; createLocked: boolean;
  pendingInitial: SubjectValues; interactionLocked: boolean; addDisabled: boolean; t: (key: MessageKey, values?: Record<string, string>) => string;
  yourEntry?: string;
  onAdd: () => void; onCancelAdd: () => void;
  onAddSubmit: (values: SubjectValues) => Promise<SubmitOutcome>; onStartEditing: (subject: Subject) => void;
  onArchive: (subject: Subject, archived: boolean, undo: boolean) => void; onRestore: (subject: Subject, archived: boolean, undo: boolean) => void;
  onSave: (values: SubjectValues) => Promise<SubmitOutcome>; onCancelEditing: (id: string) => void;
  onPrevious: (event: React.MouseEvent<HTMLButtonElement>) => void; onNext: (event: React.MouseEvent<HTMLButtonElement>) => void;
}) {
  if (list.kind !== 'ready') return null;
  return <div className="mt-8 space-y-6">
    {!showArchived && !adding && !empty ? <Button type="button" data-focus="add" disabled={addDisabled}
      onClick={onAdd} className="w-full sm:w-auto">{t('subjects.add')}</Button> : null}
    {adding ? <SubjectForm label={t('subjects.add.formLabel')} initial={pendingInitial} members={members}
      membersLoaded={membersLoaded} submitLabel={t('subjects.add.submit')} pendingLabel={t('subjects.add.submitting')}
      locked={createLocked} retryLabel={t('subjects.create.checkAgain')}
      notice={createUncertain ? t('subjects.notice.uncertain') : undefined} onSubmit={onAddSubmit} onCancel={onCancelAdd} /> : null}
    {empty ? <SubjectsEmpty showArchived={showArchived} cursorStackEmpty={cursorStack.length === 0} adding={adding} t={t} onAdd={onAdd} /> : null}
    {items.length > 0 ? <ul className="divide-y divide-line rounded-2xl border border-line bg-white/70">
      {items.map((subject) => <SubjectRow key={subject.id} subject={subject} editing={editing} members={members}
        membersLoaded={membersLoaded} showArchived={showArchived} interactionLocked={interactionLocked} t={t} yourEntry={yourEntry}
        onStartEditing={onStartEditing} onArchive={onArchive} onRestore={onRestore} onSave={onSave} onCancelEditing={onCancelEditing} />)}
    </ul> : null}
    {cursorStack.length > 0 || list.nextCursor ? <div className="flex flex-wrap items-center justify-between gap-2">
      {cursorStack.length > 0 ? <Button data-subject-page type="button" variant="quiet" onClick={onPrevious} disabled={interactionLocked}>
        {cursorStack.length > 1 ? t('subjects.page.previous') : t('subjects.page.first')}
      </Button> : <span />}
      {list.nextCursor ? <Button data-subject-page type="button" variant="quiet" onClick={onNext}
        disabled={interactionLocked}>{t('subjects.page.next')}</Button> : null}
      <p aria-live="polite" className="w-full text-center text-sm text-muted">{t('subjects.page.number', { n: String(cursorStack.length + 1) })}</p>
    </div> : null}
  </div>;
}
