import { Button } from '../../components/ui/button';
import type { Member } from '../members/membersApi';
import { typeLabelKey, type SubjectValues, SubjectForm, type SubmitOutcome } from './SubjectForm';
import type { MessageKey } from '../../i18n';
import type { Editing } from './subjectsModel';
import type { Subject } from './subjectsApi';

export function SubjectRow({ subject, editing, members, membersLoaded, showArchived, interactionLocked, t, yourEntry,
  onStartEditing, onArchive, onRestore, onSave, onCancelEditing }: {
  subject: Subject; editing: Editing | null; members: Member[]; membersLoaded: boolean; showArchived: boolean;
  interactionLocked: boolean; t: (key: MessageKey, values?: Record<string, string>) => string; yourEntry?: string;
  onStartEditing: (subject: Subject) => void; onArchive: (subject: Subject, archived: boolean, undo: boolean) => void;
  onRestore: (subject: Subject, archived: boolean, undo: boolean) => void;
  onSave: (values: SubjectValues) => Promise<SubmitOutcome>; onCancelEditing: (id: string) => void;
}) {
  return <li className="p-3 sm:px-5">
    {editing && editing.subject.id === subject.id ? <SubjectForm
      key={`${subject.id}:${editing.etag}`}
      label={t('subjects.edit.label', { name: subject.name })}
      initial={{ name: editing.subject.name, type: editing.subject.type, linkedUserId: editing.subject.linkedUserId }}
      members={members} membersLoaded={membersLoaded} submitLabel={t('subjects.save')} pendingLabel={t('subjects.saving')}
      notice={editing.conflict ? t('subjects.notice.conflict') : undefined} yourEntry={yourEntry} onSubmit={onSave}
      onCancel={() => onCancelEditing(subject.id)}
    /> : <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1">
      <div className="min-w-0 flex-1 basis-40">
        <p className="break-words text-lg leading-snug" data-subject-name>{subject.name}</p>
        <p className="text-sm text-muted" data-subject-type>{t(typeLabelKey(subject.type))}</p>
        {subject.type === 'person' && subject.linkedUserId ? (() => {
          const member = members.find((entry) => entry.userId === subject.linkedUserId);
          return member ? <p className="break-words text-sm text-muted">{t('subjects.link.row', { login: member.login })}</p> : null;
        })() : null}
      </div>
      <div className="flex items-center gap-1">
        {showArchived ? <Button type="button" variant="quiet" size="small" data-focus={`row:${subject.id}`}
          aria-label={t('subjects.restore.label', { name: subject.name })} disabled={interactionLocked}
          onClick={() => onRestore(subject, false, false)}>{t('subjects.restore')}</Button> : <>
          <Button type="button" variant="quiet" size="small" data-focus={`row:${subject.id}`}
            aria-label={t('subjects.edit.label', { name: subject.name })} disabled={interactionLocked}
            onClick={() => onStartEditing(subject)}>{t('subjects.edit')}</Button>
          <Button type="button" variant="quiet" size="small" aria-label={t('subjects.archive.label', { name: subject.name })}
            disabled={interactionLocked} onClick={() => onArchive(subject, true, false)}>{t('subjects.archive')}</Button>
        </>}
      </div>
    </div>}
  </li>;
}
