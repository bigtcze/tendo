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

/** The row is its own line: a name, one quiet metadata line, then the actions on the same line where width allows. */
const NAME = 'text-row-title font-semibold leading-snug text-ink [overflow-wrap:anywhere]';
// Inline flow like Home's row meta: the separator stays with the text it joins and every part shares one baseline.
const META = 'mt-0.5 block text-meta leading-snug text-muted';
const SEPARATOR = 'select-none';
const ACTION_LINK = 'inline-flex min-h-11 items-center rounded-lg px-2.5 text-small font-semibold text-accent '
  + 'transition-colors hover:bg-sand focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus';

/**
 * One flat row: the name over a single metadata line (type, and for a member's own person the account it belongs
 * to), with the row's actions at the end. The name column keeps a 10 rem basis, so the actions drop to their own
 * right-aligned line only when they would squeeze the name below that; the type glyph is inline and decorative,
 * matching Home's rows, and the type is always written out beside it.
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
      onCancel={() => onCancelEditing(subject.id)} /> : <div className="flex flex-wrap items-center gap-x-3 gap-y-2 py-3">
      <div className="min-w-0 grow basis-40">
        <p data-subject-name className={NAME}>{subject.name}</p>
        <p className={META}>
          <span className="inline-flex min-w-0 max-w-full items-center gap-1 align-[-0.15em]">
            <span aria-hidden="true" data-subject-icon={subject.type} className="inline-flex shrink-0">
              <SubjectTypeIcon type={subject.type} className="size-3.5" />
            </span>
            <span data-subject-type>{t(typeLabelKey(subject.type))}</span>
          </span>
          {memberPerson ? <>
            {' '}<span aria-hidden="true" className={SEPARATOR}>·</span>{' '}
            <span data-subject-member className="min-w-0 [overflow-wrap:anywhere]">
              {member ? t('subjects.member.row', { login: member.login }) : t('subjects.member')}
            </span>
          </> : null}
        </p>
      </div>
      <div className="ml-auto flex shrink-0 items-center gap-1">
        {!showArchived && navigation ? <NavLink href={`/items?subject=${encodeURIComponent(subject.id)}`}
          onNavigate={() => navigation.actions.allItems(subject.id)}
          aria-label={t('subjects.seeItems.label', { name: subject.name })}
          className={ACTION_LINK}>
          {t('subjects.seeItems')}
        </NavLink> : null}
        {showArchived ? <Button type="button" variant="quiet" size="small" data-focus={`row:${subject.id}`}
          aria-label={t('subjects.restore.label', { name: subject.name })} disabled={interactionLocked}
          onClick={() => onRestore(subject, false, false)}>{t('subjects.restore')}</Button> : <Button ref={editRef} type="button" variant="quiet" size="small" data-focus={`row:${subject.id}`}
          aria-label={t('subjects.edit.label', { name: subject.name })} disabled={interactionLocked}
          onClick={() => onStartEditing(subject)}>{t('subjects.edit')}</Button>}
      </div>
    </div>}
  </li>;
}
