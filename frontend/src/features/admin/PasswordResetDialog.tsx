import { useEffect, useRef, useState, type RefObject } from 'react';
import { Button } from '../../components/ui/button';
import { Dialog } from '../../components/ui/dialog';
import { Notice, NoticeRegion } from '../../components/ui/notice';
import { useI18n, type Translate } from '../../i18n';
import type { IssuedReset, PendingReset } from './usePasswordReset';

function Confirmation({ pending, busy, onConfirm, onCancel, t }: {
  pending: PendingReset; busy: boolean; onConfirm: () => void; onCancel: () => void; t: Translate;
}) {
  const { account, mode } = pending;
  return <div className="space-y-4">
    <p className="max-w-prose text-body text-ink">{t('admin.reset.dialog.body', { login: account.login })}</p>
    <ul className="list-disc space-y-1 pl-5 text-body text-ink">
      <li>{t('admin.reset.dialog.oldPassword')}</li>
      <li>{t('admin.reset.dialog.signsOut')}</li>
      <li>{t('admin.reset.dialog.once')}</li>
      <li>{t('admin.reset.dialog.neverSee')}</li>
    </ul>
    {mode === 'renew' ? <Notice>{t('admin.reset.dialog.renewNote')}</Notice> : null}
    <div className="flex flex-wrap justify-end gap-2">
      <Button variant="quiet" disabled={busy} onClick={onCancel}>{t('admin.action.cancel')}</Button>
      <Button disabled={busy} onClick={onConfirm}>
        {busy ? t('admin.reset.dialog.creating') : t('admin.reset.dialog.confirm')}
      </Button>
    </div>
  </div>;
}

function LinkShown({ issued, onClose }: { issued: IssuedReset; onClose: () => void }) {
  const { t, locale } = useI18n();
  const [copied, setCopied] = useState(false);
  const [copyMessage, setCopyMessage] = useState<string | null>(null);
  const linkRef = useRef<HTMLInputElement>(null);
  const expires = new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(issued.expiresAt));

  useEffect(() => {
    // The link is the point of this dialog: put the caret on it and select it for a quick manual copy.
    linkRef.current?.focus();
    linkRef.current?.select();
  }, []);

  async function copy() {
    try {
      await navigator.clipboard.writeText(issued.link);
      setCopied(true);
      setCopyMessage(t('admin.reset.link.copied'));
    } catch {
      setCopyMessage(t('admin.reset.link.manualCopy'));
      linkRef.current?.focus();
      linkRef.current?.select();
    }
  }

  return <div className="space-y-4">
    <p className="max-w-prose text-body text-ink">{t('admin.reset.link.lead', { login: issued.account.login })}</p>
    <div className="flex flex-wrap gap-2">
      <input
        ref={linkRef}
        readOnly
        value={issued.link}
        aria-label={t('admin.reset.link.label')}
        className="min-h-11 min-w-0 flex-1 basis-48 rounded-xl border border-field bg-raised px-3 text-small break-all text-ink focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
      />
      <Button variant="secondary" onClick={() => void copy()}>
        {copied ? t('admin.reset.link.copiedButton') : t('admin.reset.link.copy')}
      </Button>
    </div>
    <NoticeRegion message={copyMessage ?? ''} className="empty:contents" />
    <p className="text-small text-muted">{t('admin.reset.link.expires', { date: expires })}</p>
    <p className="max-w-prose text-small text-muted">{t('admin.reset.link.caveat')}</p>
    <div className="flex flex-wrap justify-end">
      <Button onClick={onClose}>{t('admin.reset.link.done')}</Button>
    </div>
  </div>;
}

/**
 * Two phases in one dialog: the consequence warning, then the one-time link. Closing either phase returns
 * focus to the row action; closing the link also drops the token from memory.
 */
export function PasswordResetDialog({ pending, issued, busy, triggerRef, onConfirm, onCancel, onCloseLink }: {
  pending: PendingReset | null;
  issued: IssuedReset | null;
  busy: boolean;
  triggerRef: RefObject<HTMLElement | null>;
  onConfirm: () => void;
  onCancel: () => void;
  onCloseLink: () => void;
}) {
  const { t } = useI18n();
  const open = pending !== null || issued !== null;
  const title = issued
    ? t('admin.reset.link.title', { login: issued.account.login })
    : pending
      ? t(pending.mode === 'renew' ? 'admin.reset.dialog.renewTitle' : 'admin.reset.dialog.title', { login: pending.account.login })
      : '';
  return <Dialog
    open={open}
    onOpenChange={(next) => { if (!next) { if (issued) onCloseLink(); else onCancel(); } }}
    title={title}
    closeLabel={t('admin.dialog.close')}
    busy={busy}
    triggerRef={triggerRef}
  >
    {issued
      ? <LinkShown issued={issued} onClose={onCloseLink} />
      : pending
        ? <Confirmation pending={pending} busy={busy} onConfirm={onConfirm} onCancel={onCancel} t={t} />
        : null}
  </Dialog>;
}
