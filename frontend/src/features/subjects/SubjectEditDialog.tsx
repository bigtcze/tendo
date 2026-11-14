import { useRef, useState, type RefObject } from 'react';
import { Dialog } from '../../components/ui/dialog';
import type { MessageKey } from '../../i18n';
import type { Member } from '../members/membersApi';
import { isMemberPerson } from './subjectsModel';
import type { Subject } from './subjectsApi';
import type { MemberPersonTransferApi } from './useMemberPersonTransfer';
import { MemberPersonTransfer } from './MemberPersonTransfer';
import { SubjectForm, type SubjectValues, type SubmitOutcome } from './SubjectForm';

export function SubjectEditDialog({ subject, etag, conflict, transfer, members, membersLoaded, yourEntry, triggerRef, t,
  onSave, onArchive, onCancel }: {
  subject: Subject; etag: string; conflict: boolean; transfer: MemberPersonTransferApi;
  members: Member[]; membersLoaded: boolean; yourEntry?: string;
  triggerRef: RefObject<HTMLButtonElement | null>; t: (key: MessageKey, values?: Record<string, string>) => string;
  onSave: (values: SubjectValues) => Promise<SubmitOutcome>; onArchive: () => void; onCancel: () => void;
}) {
  const initialFocusRef = useRef<HTMLInputElement | null>(null);
  const [saving, setSaving] = useState(false);
  const [transferring, setTransferring] = useState(false);
  const memberPerson = isMemberPerson(subject);
  const label = t('subjects.edit.label', { name: subject.name });
  return <Dialog open onOpenChange={(open) => { if (!open) onCancel(); }} title={label}
    sheet triggerRef={triggerRef} initialFocusRef={initialFocusRef} busy={saving || transferring}>
    <SubjectForm key={`${subject.id}:${etag}`} label={label}
      initial={{ name: subject.name, type: subject.type }} memberPerson={memberPerson}
      submitLabel={t('subjects.save')} pendingLabel={t('subjects.saving')}
      notice={conflict ? t('subjects.notice.conflict') : undefined} yourEntry={yourEntry} initialFocusRef={initialFocusRef} onSubmit={onSave}
      archiveLabel={memberPerson ? undefined : t('subjects.archive')} onArchive={memberPerson ? undefined : onArchive}
      onCancel={onCancel} onPendingChange={setSaving} />
    {!memberPerson && subject.type === 'person' ? <MemberPersonTransfer subject={subject} etag={etag}
      members={members} membersLoaded={membersLoaded} api={transfer} onPendingChange={setTransferring} /> : null}
  </Dialog>;
}
