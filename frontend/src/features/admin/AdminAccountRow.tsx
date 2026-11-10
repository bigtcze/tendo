import { Button } from '../../components/ui/button';
import type { Translate } from '../../i18n';
import { signInMethod, type Account } from './adminApi';

function initialOf(login: string): string {
  return login.trim().charAt(0).toLocaleUpperCase() || '?';
}

/**
 * One compact account row: who it is, what they can do, how they sign in, and the role and password actions.
 * Password reset appears only for a local password that is not the viewer's own; single sign-on accounts keep
 * a short explanation instead so the missing action never looks broken.
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
  return <li data-account-id={account.userId} className="flex min-h-14 flex-wrap items-center gap-x-3 gap-y-2 px-4 py-3">
    <span aria-hidden="true" className="grid size-9 shrink-0 place-items-center rounded-full bg-nav-selected text-small font-semibold text-ink">
      {initialOf(account.login)}
    </span>
    <div className="min-w-0 flex-1">
      <p className="min-w-0 break-words text-row-title font-semibold text-ink">
        <span data-account-login>{account.login}</span>
        {self ? <span className="font-normal text-muted"> ({t('members.you')})</span> : null}
      </p>
      <p className="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-1 text-meta text-muted">
        <span data-account-role className="rounded-full bg-sand px-2 py-0.5 text-small font-semibold text-muted">
          {t(isAdmin ? 'members.role.admin' : 'members.role.member')}
        </span>
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
      {reset
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
