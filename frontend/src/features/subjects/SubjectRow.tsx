import { useContext, useRef } from 'react';
import { NavLink } from '../../app/NavLink';
import { HouseholdNavigationContext } from '../../app/HouseholdNavigation';
import { Button } from '../../components/ui/button';
import { SubjectEditDialog } from './SubjectEditDialog';
import type { Member } from '../members/membersApi';
import { typeLabelKey } from './SubjectForm';
import type { SubjectValues, SubmitOutcome } from './SubjectForm';
import type { MessageKey } from '../../i18n';
import { isMemberPerson, type Editing } from './subjectsModel';
import type { Subject } from './subjectsApi';
import type { MemberPersonTransferApi } from './useMemberPersonTransfer';

export function SubjectRow({ subject, editing, transfer, members, membersLoaded, showArchived, interactionLocked, t, yourEntry,
  onStartEditing, onArchive, onRestore, onSave, onCancelEditing }: {
  subject: Subject; editing: Editing | null; transfer: MemberPersonTransferApi; members: Member[]; membersLoaded: boolean;
  showArchived: boolean; interactionLocked: boolean; t: (key: MessageKey, values?: Record<string, string>) => string; yourEntry?: string;
  onStartEditing: (subject: Subject) => void; onArchive: (subject: Subject, archived: boolean, undo: boolean) => void;
  onRestore: (subject: Subject, archived: boolean, undo: boolean) => void;
  onSave: (values: SubjectValues) => Promise<SubmitOutcome>; onCancelEditing: (id: string) => void;
}) {
  const editRef = useRef<HTMLButtonElement>(null);
  const navigation = useContext(HouseholdNavigationContext);
  const memberPerson = isMemberPerson(subject);
  const member = memberPerson ? members.find((entry) => entry.userId === subject.linkedUserId) : undefined;
  return <li className="min-w-0 rounded-2xl border border-line bg-raised p-3 sm:p-4">
    {editing && editing.subject.id === subject.id ? <SubjectEditDialog subject={editing.subject} etag={editing.etag}
      conflict={editing.conflict} transfer={transfer} members={members} membersLoaded={membersLoaded} yourEntry={yourEntry}
      triggerRef={editRef} t={t} onSave={onSave} onArchive={() => onArchive(subject, true, false)}
      onCancel={() => onCancelEditing(subject.id)} /> : <div className={`flex min-w-0 gap-2 ${showArchived ? 'flex-row items-start justify-between' : 'flex-col sm:flex-row sm:items-center sm:justify-between sm:gap-3 lg:flex-col lg:items-start xl:flex-row xl:items-center'}`}>
      <div className="min-w-0 flex-1">
        <p className="break-words text-lg font-semibold leading-snug" data-subject-name>{subject.name}</p>
        <p className="mt-1 text-sm text-muted" data-subject-type>{t(typeLabelKey(subject.type))}</p>
        {memberPerson ? <p className="break-words text-sm text-muted" data-subject-member>
          {member ? t('subjects.member.row', { login: member.login }) : t('subjects.member')}
        </p> : null}
      </div>
      <div className={`flex flex-wrap items-center gap-1 ${showArchived ? 'shrink-0' : 'sm:shrink-0 lg:w-full lg:shrink lg:justify-start xl:w-auto xl:shrink-0'}`}>
        {!showArchived && navigation ? <NavLink href={`/items?subject=${encodeURIComponent(subject.id)}`}
          onNavigate={() => navigation.actions.allItems(subject.id)}
          aria-label={t('subjects.seeItems.label', { name: subject.name })} className="inline-flex min-h-11 items-center rounded-xl px-3 text-sm font-medium text-accent transition-colors hover:bg-sand focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus">
          {t('subjects.seeItems')}
        </NavLink> : null}
        {showArchived ? <Button type="button" variant="quiet" size="small" data-focus={`row:${subject.id}`}
          aria-label={t('subjects.restore.label', { name: subject.name })} disabled={interactionLocked}
          onClick={() => onRestore(subject, false, false)}>{t('subjects.restore')}</Button> : <Button ref={editRef} type="button" variant="secondary" size="small" data-focus={`row:${subject.id}`}
          aria-label={t('subjects.edit.label', { name: subject.name })} disabled={interactionLocked}
          onClick={() => onStartEditing(subject)}>{t('subjects.edit')}</Button>}
      </div>
    </div>}
  </li>;
}
