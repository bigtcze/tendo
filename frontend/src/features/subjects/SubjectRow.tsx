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
 * Compact row: the icon tile, the name and the row actions share the first line while the name keeps a
 * readable column, and the type/member metadata follows the name in the DOM so assistive tech hears it
 * before the actions. The wrapping is width-driven: the name column has a 10 rem basis, so the actions drop
 * to their own right-aligned line only when they would squeeze the name below that (the Czech actions are
 * wider than the English ones, so neither locale collapses a name into mid-word breaks).
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
      onCancel={() => onCancelEditing(subject.id)} /> : <div className="flex flex-wrap items-center gap-x-3 gap-y-2 px-4 py-3">
      <span aria-hidden="true" data-subject-icon={subject.type}
        className="grid size-9 shrink-0 self-start place-items-center rounded-xl bg-sand text-muted">
        <SubjectTypeIcon type={subject.type} />
      </span>
      <div className="min-w-0 grow basis-40">
        <p data-subject-name
          className="[overflow-wrap:anywhere] text-row-title font-semibold leading-snug text-ink">
          {subject.name}
        </p>
        <p className="mt-0.5 text-meta text-muted" data-subject-type>{t(typeLabelKey(subject.type))}</p>
        {memberPerson ? <p className="text-meta text-muted [overflow-wrap:anywhere]" data-subject-member>
          {member ? t('subjects.member.row', { login: member.login }) : t('subjects.member')}
        </p> : null}
      </div>
      <div className="ml-auto flex shrink-0 flex-wrap items-center gap-1">
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
    </div>}
  </li>;
}
