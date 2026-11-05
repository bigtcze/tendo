import { useRef, useState, type RefObject } from 'react';
import { Dialog } from '../../components/ui/dialog';
import type { MessageKey } from '../../i18n';
import type { Member } from '../members/membersApi';
import type { Subject } from './subjectsApi';
import { SubjectForm, type SubjectValues, type SubmitOutcome } from './SubjectForm';

export function SubjectEditDialog({ subject, etag, conflict, members, membersLoaded, yourEntry, triggerRef, t,
  onSave, onArchive, onCancel }: {
  subject: Subject; etag: string; conflict: boolean; members: Member[]; membersLoaded: boolean; yourEntry?: string;
  triggerRef: RefObject<HTMLButtonElement | null>; t: (key: MessageKey, values?: Record<string, string>) => string;
  onSave: (values: SubjectValues) => Promise<SubmitOutcome>; onArchive: () => void; onCancel: () => void;
}) {
  const initialFocusRef = useRef<HTMLInputElement | null>(null);
  const [saving, setSaving] = useState(false);
  const label = t('subjects.edit.label', { name: subject.name });
  return <Dialog open onOpenChange={(open) => { if (!open) onCancel(); }} title={label}
    closeLabel={t('subjects.cancel')} sheet triggerRef={triggerRef} initialFocusRef={initialFocusRef} busy={saving}>
    <SubjectForm key={`${subject.id}:${etag}`} label={label}
      initial={{ name: subject.name, type: subject.type, linkedUserId: subject.linkedUserId }}
      members={members} membersLoaded={membersLoaded} submitLabel={t('subjects.save')} pendingLabel={t('subjects.saving')}
      notice={conflict ? t('subjects.notice.conflict') : undefined} yourEntry={yourEntry} initialFocusRef={initialFocusRef} onSubmit={onSave}
      archiveLabel={t('subjects.archive')} onArchive={onArchive} onCancel={onCancel} onPendingChange={setSaving} />
  </Dialog>;
}
