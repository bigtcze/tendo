import { Button } from '../../components/ui/button';
import type { Translate } from '../../i18n';
import { signInMethod, type Account } from './adminApi';

/**
 * One compact account row: who it is, what they can do, how they sign in, and the role and password actions.
 * Direction B: aligned text columns and a caption-sized role label instead of an avatar and a role pill, so the
 * row reads as a record rather than a card of badges. Password reset appears only for a local password that is
 * not the viewer's own; single sign-on accounts keep a short explanation instead so the missing action never
 * looks broken.
 */
export function AdminAccountRow({ account, viewerId, busy, formatExpiry, onChange, onReset, onCancelReset, t }: {
  account: Account;
  viewerId: string;
  busy: boolean;
  formatExpiry: (iso: string) => string;
  onChange: (account: Account, trigger: HTMLElement) => void;
  onReset: (account: Account, trigger: HTMLElement) => void;
  onCancelReset: (account: Account) => void;
  t: Translate;
}) {
  const isAdmin = account.role === 'admin';
  const self = account.userId === viewerId;
  const reset = account.passwordReset;
  return <li data-account-id={account.userId} className="flex flex-wrap items-center gap-x-4 gap-y-2 px-4 py-3">
    <div className="min-w-0 flex-1 basis-48">
      <p className="min-w-0 text-row-title font-semibold text-ink">
        <span data-account-login className="break-words">{account.login}</span>
        {self ? <span className="text-small font-normal text-muted"> ({t('members.you')})</span> : null}
      </p>
      <p className="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-caption text-muted">
        <span data-account-role className="font-semibold uppercase tracking-[0.04em]">
          {t(isAdmin ? 'members.role.admin' : 'members.role.member')}
        </span>
        <span aria-hidden="true">·</span>
        <span>{t(`admin.signIn.${signInMethod(account)}`)}</span>
      </p>
      {reset
        ? <p data-account-reset className="mt-1 text-caption text-muted">
          {t('admin.reset.active', { date: formatExpiry(reset.expiresAt) })}
        </p>
        : null}
      {!account.signInMethods.password
        ? <p className="mt-1 max-w-prose text-caption text-muted">{t('admin.reset.oidcOnly')}</p>
        : null}
    </div>
    <div className="flex w-full flex-wrap gap-2 sm:w-auto sm:justify-end">
      {reset && !self
        ? <Button variant="quiet" size="small" disabled={busy}
          aria-label={t('admin.reset.cancelLabel', { login: account.login })}
          onClick={() => onCancelReset(account)}>
          {t('admin.reset.cancel')}
        </Button>
        : null}
      {account.signInMethods.password && !self
        ? <Button variant="secondary" size="small" disabled={busy}
          aria-label={t(reset ? 'admin.reset.renewLabel' : 'admin.reset.actionLabel', { login: account.login })}
          onClick={(event) => onReset(account, event.currentTarget)}>
          {t(reset ? 'admin.reset.renew' : 'admin.reset.action')}
        </Button>
        : null}
      <Button
        variant="secondary"
        size="small"
        disabled={busy}
        aria-label={t(isAdmin ? 'admin.action.demoteLabel' : 'admin.action.promoteLabel', { login: account.login })}
        onClick={(event) => onChange(account, event.currentTarget)}
      >
        {t(isAdmin ? 'admin.action.demote' : 'admin.action.promote')}
      </Button>
    </div>
  </li>;
}
