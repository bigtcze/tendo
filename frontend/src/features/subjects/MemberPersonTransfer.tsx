import { useId, useRef, useState } from 'react';
import { Button } from '../../components/ui/button';
import { Label } from '../../components/ui/label';
import { cn } from '../../lib/cn';
import { useI18n } from '../../i18n';
import type { Member } from '../members/membersApi';
import type { Subject } from './subjectsApi';
import type { MemberPersonPreview, MemberPersonTransferApi, TransferOutcome } from './useMemberPersonTransfer';

/** A preview belongs to the member it was requested for, so a late result can never describe another selection. */
type PreviewState =
  | { kind: 'idle' }
  | { kind: 'loading'; userId: string }
  | (MemberPersonPreview & { userId: string });

/**
 * Quiet, optional path for a person record that predates V4: choose the member it really belongs to,
 * read the outcome, then confirm. Never offered for a member's own person or a non-person record.
 */
export function MemberPersonTransfer({ subject, etag, members, membersLoaded, api, onPendingChange }: {
  subject: Subject; etag: string; members: Member[]; membersLoaded: boolean;
  api: MemberPersonTransferApi; onPendingChange?: (pending: boolean) => void;
}) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const [memberId, setMemberId] = useState('');
  const [preview, setPreview] = useState<PreviewState>({ kind: 'idle' });
  const [pending, setPending] = useState(false);
  const [problem, setProblem] = useState<'failed' | null>(null);
  const previewToken = useRef(0);
  const titleId = useId();
  const selectId = useId();
  if (!membersLoaded || members.length === 0) return null;
  const member = members.find((entry) => entry.userId === memberId) ?? null;
  const previewMatchesMember = preview.kind !== 'idle' && preview.kind !== 'loading' && preview.userId === memberId;

  async function choose(userId: string) {
    const token = ++previewToken.current;
    setMemberId(userId);
    setProblem(null);
    if (!userId) { setPreview({ kind: 'idle' }); return; }
    setPreview({ kind: 'loading', userId });
    const result = await api.preview(userId);
    // Only the newest choice may publish a preview; older requests stay in flight but are ignored here.
    if (token === previewToken.current) setPreview({ ...result, userId });
  }

  function reset() {
    previewToken.current += 1;
    setOpen(false);
    setMemberId('');
    setPreview({ kind: 'idle' });
    setProblem(null);
  }

  async function confirm() {
    if (!member || preview.kind !== 'ok' || !previewMatchesMember || pending) return;
    setPending(true);
    onPendingChange?.(true);
    setProblem(null);
    let outcome: TransferOutcome;
    try {
      outcome = await api.transfer(subject, etag, member);
    } catch {
      outcome = 'failed';
    } finally {
      setPending(false);
      onPendingChange?.(false);
    }
    // Any other outcome closes the editor and explains itself in the list notice.
    if (outcome === 'failed') setProblem('failed');
  }

  if (!open) {
    return <div className="border-t border-line pt-4">
      <Button type="button" variant="quiet" onClick={() => setOpen(true)}>{t('subjects.transfer.action')}</Button>
    </div>;
  }

  return <section aria-labelledby={titleId} className="mt-6 space-y-3 border-t border-line pt-4">
    <h3 id={titleId} className="text-lg font-semibold text-ink">{t('subjects.transfer.title')}</h3>
    <p className="text-sm text-muted">{t('subjects.transfer.intro')}</p>
    <MemberPicker id={selectId} memberId={memberId} members={members} pending={pending} onChange={choose} />
    <PreviewFeedback preview={preview} member={member} memberId={memberId} subjectName={subject.name} pending={pending}
      onRetry={() => void choose(memberId)} />
    {problem === 'failed' ? <p role="alert" className="text-sm text-danger">{t('subjects.transfer.error.failed')}</p> : null}
    <div className="flex flex-wrap gap-2">
      <Button type="button" disabled={!member || preview.kind !== 'ok' || !previewMatchesMember || pending}
        onClick={() => void confirm()}>
        {pending ? t('subjects.transfer.pending') : t('subjects.transfer.confirmAction')}
      </Button>
      <Button type="button" variant="quiet" disabled={pending} onClick={reset}>
        {t('subjects.cancel')}
      </Button>
    </div>
  </section>;
}

function MemberPicker({ id, memberId, members, pending, onChange }: {
  id: string; memberId: string; members: Member[]; pending: boolean; onChange: (userId: string) => void;
}) {
  const { t } = useI18n();
  return <div className="space-y-1">
    <Label htmlFor={id}>{t('subjects.transfer.memberLabel')}</Label>
    <select id={id} value={memberId} disabled={pending} onChange={(event) => onChange(event.target.value)}
      className={cn(
        'min-h-11 w-full rounded-xl border border-field bg-raised px-3 text-ink',
        'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent',
      )}>
      <option value="">{t('subjects.transfer.choose')}</option>
      {members.map((entry) => <option key={entry.userId} value={entry.userId}>{entry.login}</option>)}
    </select>
  </div>;
}

function PreviewFeedback({ preview, member, memberId, subjectName, pending, onRetry }: {
  preview: PreviewState; member: Member | null; memberId: string; subjectName: string; pending: boolean; onRetry: () => void;
}) {
  const { t } = useI18n();
  // A preview for another member is never shown as this member's confirmation; treat it as still loading.
  if (preview.kind === 'loading' || (preview.kind !== 'idle' && preview.userId !== memberId)) {
    return <p className="text-sm text-muted">{t('subjects.transfer.checking')}</p>;
  }
  if (preview.kind === 'ok' && member) {
    return <p className="rounded-xl bg-sand px-4 py-3 text-sm text-ink">
      {t('subjects.transfer.confirm', { name: subjectName, login: member.login, current: preview.name })}
    </p>;
  }
  if (preview.kind === 'missing') return <p role="alert" className="text-sm text-danger">{t('subjects.transfer.previewMissing')}</p>;
  if (preview.kind !== 'failed') return null;
  return <div className="space-y-2">
    <p role="alert" className="text-sm text-danger">{t('subjects.transfer.previewFailed')}</p>
    <Button type="button" variant="quiet" size="small" disabled={pending} onClick={onRetry}>
      {t('subjects.transfer.retry')}
    </Button>
  </div>;
}
