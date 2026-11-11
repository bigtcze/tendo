import { useContext, useRef } from 'react';
import { NavLink } from '../../app/NavLink';
import { HouseholdNavigationContext } from '../../app/HouseholdNavigation';
import { Button } from '../../components/ui/button';
import { SubjectEditDialog } from './SubjectEditDialog';
import { SubjectTypeIcon } from './SubjectTypeIcon';
import type { Member } from '../members/membersApi';
import { typeLabelKey } from './SubjectForm';
import type { SubjectValues, SubmitOutcome } from './SubjectForm';
import type { MessageKey } from '../../i18n';
import { isMemberPerson, type Editing } from './subjectsModel';
import type { Subject } from './subjectsApi';
import type { MemberPersonTransferApi } from './useMemberPersonTransfer';

/**
 * Compact row: the icon tile, the name and the row actions share the first line. The type and member
 * metadata then fills the width beneath them — spanning under the actions on phones, where the name column
 * would otherwise squeeze it, and staying in the name column from `sm`, where the actions span both text
 * lines instead and add no height. No two grid items ever share a cell. Below 360px the actions drop to
 * their own row, the only width where the name would be squeezed.
 */
export function SubjectRow({ subject, editing, transfer, members, membersLoaded, showArchived, interactionLocked, t, yourEntry,
  onStartEditing, onArchive, onRestore, onSave, onCancelEditing }: {
  subject: Subject; editing: Editing | null; transfer: MemberPersonTransferApi; members: Member[]; membersLoaded: boolean;
  showArchived: boolean; interactionLocked: boolean; t: (key: MessageKey, values?: Record<string, string>) => string;
  yourEntry?: string;
  onStartEditing: (subject: Subject) => void; onArchive: (subject: Subject, archived: boolean, undo: boolean) => void;
  onRestore: (subject: Subject, archived: boolean, undo: boolean) => void;
  onSave: (values: SubjectValues) => Promise<SubmitOutcome>; onCancelEditing: (id: string) => void;
}) {
  const editRef = useRef<HTMLButtonElement>(null);
  const navigation = useContext(HouseholdNavigationContext);
  const memberPerson = isMemberPerson(subject);
  const member = memberPerson ? members.find((entry) => entry.userId === subject.linkedUserId) : undefined;
  return <li className="min-w-0">
    {editing && editing.subject.id === subject.id ? <SubjectEditDialog subject={editing.subject} etag={editing.etag}
      conflict={editing.conflict} transfer={transfer} members={members} membersLoaded={membersLoaded} yourEntry={yourEntry}
      triggerRef={editRef} t={t} onSave={onSave} onArchive={() => onArchive(subject, true, false)}
      onCancel={() => onCancelEditing(subject.id)} /> : <div className="grid min-w-0 grid-cols-[2.25rem_minmax(0,1fr)_auto] items-center gap-x-3 px-4 py-3 max-[359px]:grid-cols-[2.25rem_minmax(0,1fr)]">
      <span aria-hidden="true" data-subject-icon={subject.type}
        className="col-start-1 col-end-2 row-start-1 row-end-3 grid size-9 place-items-center self-center rounded-xl bg-sand text-muted max-[359px]:row-end-4">
        <SubjectTypeIcon type={subject.type} />
      </span>
      <p data-subject-name
        className="col-start-2 col-end-3 row-start-1 min-w-0 [overflow-wrap:anywhere] text-row-title font-semibold leading-snug text-ink">
        {subject.name}
      </p>
      <div className="col-start-3 col-end-4 row-start-1 ml-auto flex flex-wrap items-center justify-end gap-1 self-center sm:row-end-3 max-[359px]:col-start-2 max-[359px]:col-end-3 max-[359px]:row-start-3 max-[359px]:row-end-4">
        {!showArchived && navigation ? <NavLink href={`/items?subject=${encodeURIComponent(subject.id)}`}
          onNavigate={() => navigation.actions.allItems(subject.id)}
          aria-label={t('subjects.seeItems.label', { name: subject.name })}
          className="inline-flex min-h-11 items-center rounded-xl px-3 text-small font-semibold text-accent transition-colors hover:bg-sand focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus">
          {t('subjects.seeItems')}
        </NavLink> : null}
        {showArchived ? <Button type="button" variant="quiet" size="small" data-focus={`row:${subject.id}`}
          aria-label={t('subjects.restore.label', { name: subject.name })} disabled={interactionLocked}
          onClick={() => onRestore(subject, false, false)}>{t('subjects.restore')}</Button> : <Button ref={editRef} type="button" variant="secondary" size="small" data-focus={`row:${subject.id}`}
          aria-label={t('subjects.edit.label', { name: subject.name })} disabled={interactionLocked}
          onClick={() => onStartEditing(subject)}>{t('subjects.edit')}</Button>}
      </div>
      <div className="col-start-2 col-end-4 row-start-2 min-w-0 sm:col-end-3 max-[359px]:col-end-3">
        <p className="mt-0.5 text-meta text-muted" data-subject-type>{t(typeLabelKey(subject.type))}</p>
        {memberPerson ? <p className="text-meta text-muted [overflow-wrap:anywhere]" data-subject-member>
          {member ? t('subjects.member.row', { login: member.login }) : t('subjects.member')}
        </p> : null}
      </div>
    </div>}
  </li>;
}
