import { Button } from '../../components/ui/button';
import type { Translate } from '../../i18n';
import { signInMethod, type Account } from './adminApi';
import type { RemovalTarget } from './useAccountRemoval';

type ResetHandlers = {
  self: boolean;
  busy: boolean;
  onReset: (account: Account, trigger: HTMLElement) => void;
  onCancelReset: (account: Account) => void;
  t: Translate;
};

/**
 * One flat account row, like a Members row: the login over one muted line that says the role and how the account
 * signs in, then the role, password and membership actions. No avatar tile and no pill: the header already shows the
 * viewer's initial, and the role is a fact, not a badge. Password reset appears only for a local password that is not
 * the viewer's own; single sign-on accounts keep a short explanation instead so the missing action never looks broken.
 * Removal comes last and, on the viewer's own row, asks about leaving instead.
 */
export function AdminAccountRow({ account, viewerId, busy, formatExpiry, onChange, onReset, onCancelReset, onRemove, t }: {
  account: Account;
  viewerId: string;
  busy: boolean;
  formatExpiry: (iso: string) => string;
  onChange: (account: Account, trigger: HTMLElement) => void;
  onReset: (account: Account, trigger: HTMLElement) => void;
  onCancelReset: (account: Account) => void;
  /** Absent while the shell cannot name the household, so a destructive action never runs on an unknown one. */
  onRemove?: (target: RemovalTarget, trigger: HTMLElement) => void;
  t: Translate;
}) {
  const isAdmin = account.role === 'admin';
  const self = account.userId === viewerId;
  const reset = account.passwordReset;
  return <li data-account-id={account.userId} className="flex flex-wrap items-center gap-x-3 gap-y-2 py-3">
    <div className="min-w-0 flex-1 basis-40">
      <p className="min-w-0 break-words text-row-title font-semibold text-ink">
        <span data-account-login>{account.login}</span>
        {self ? <span className="font-normal text-muted"> ({t('members.you')})</span> : null}
      </p>
      <p data-account-meta className="mt-0.5 text-meta leading-snug text-muted">
        <span data-account-role>{t(isAdmin ? 'members.role.admin' : 'members.role.member')}</span>
        {' '}<span aria-hidden="true" className="select-none">·</span>{' '}
        <span>{t(`admin.signIn.${signInMethod(account)}`)}</span>
      </p>
      {reset
        ? <p data-account-reset className="mt-1 text-small text-muted">
          {t('admin.reset.active', { date: formatExpiry(reset.expiresAt) })}
        </p>
        : null}
      {!account.signInMethods.password
        ? <p className="mt-1 max-w-prose text-small text-muted">{t('admin.reset.oidcOnly')}</p>
        : null}
    </div>
    <div className="flex w-full flex-wrap gap-2 sm:w-auto sm:justify-end">
      <ResetControls account={account} self={self} busy={busy} onReset={onReset} onCancelReset={onCancelReset} t={t} />
      <Button
        variant="secondary"
        size="small"
        disabled={busy}
        aria-label={t(isAdmin ? 'admin.action.demoteLabel' : 'admin.action.promoteLabel', { login: account.login })}
        onClick={(event) => onChange(account, event.currentTarget)}
      >
        {t(isAdmin ? 'admin.action.demote' : 'admin.action.promote')}
      </Button>
      {onRemove
        ? <Button
          variant="secondary"
          size="small"
          disabled={busy}
          aria-label={t(self ? 'admin.action.leaveLabel' : 'admin.action.removeLabel', { login: account.login })}
          onClick={(event) => onRemove({ userId: account.userId, login: account.login, role: account.role }, event.currentTarget)}
        >
          {t(self ? 'admin.action.leave' : 'admin.action.remove')}
        </Button>
        : null}
    </div>
  </li>;
}

/** Only a local password can be reset, and never the viewer's own; a pending reset can be cancelled instead. */
function ResetControls({ account, self, busy, onReset, onCancelReset, t }: ResetHandlers & { account: Account }) {
  const reset = account.passwordReset;
  if (!account.signInMethods.password || self) return null;
  return <>
    {reset
      ? <Button variant="quiet" size="small" disabled={busy}
        aria-label={t('admin.reset.cancelLabel', { login: account.login })}
        onClick={() => onCancelReset(account)}>
        {t('admin.reset.cancel')}
      </Button>
      : null}
    <Button variant="secondary" size="small" disabled={busy}
      aria-label={t(reset ? 'admin.reset.renewLabel' : 'admin.reset.actionLabel', { login: account.login })}
      onClick={(event) => onReset(account, event.currentTarget)}>
      {t(reset ? 'admin.reset.renew' : 'admin.reset.action')}
    </Button>
  </>;
}
