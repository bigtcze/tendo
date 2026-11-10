import { Button } from '../../components/ui/button';
import type { Translate } from '../../i18n';
import { signInMethod, type Account } from './adminApi';

function initialOf(login: string): string {
  return login.trim().charAt(0).toLocaleUpperCase() || '?';
}

/** One compact account row: who it is, what they can do, how they sign in, and the single role action. */
export function AdminAccountRow({ account, viewerId, busy, onChange, t }: {
  account: Account;
  viewerId: string;
  busy: boolean;
  onChange: (account: Account, trigger: HTMLElement) => void;
  t: Translate;
}) {
  const isAdmin = account.role === 'admin';
  return <li data-account-id={account.userId} className="flex min-h-14 flex-wrap items-center gap-x-3 gap-y-2 px-4 py-2">
    <span aria-hidden="true" className="grid size-9 shrink-0 place-items-center rounded-full bg-nav-selected text-small font-semibold text-ink">
      {initialOf(account.login)}
    </span>
    <div className="min-w-0 flex-1">
      <p className="min-w-0 break-words text-row-title font-semibold text-ink">
        <span data-account-login>{account.login}</span>
        {account.userId === viewerId ? <span className="font-normal text-muted"> ({t('members.you')})</span> : null}
      </p>
      <p className="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-1 text-meta text-muted">
        <span data-account-role className="rounded-full bg-sand px-2 py-0.5 text-small font-semibold text-muted">
          {t(isAdmin ? 'members.role.admin' : 'members.role.member')}
        </span>
        <span>{t(`admin.signIn.${signInMethod(account)}`)}</span>
      </p>
    </div>
    <Button
      variant="secondary"
      size="small"
      className="w-full sm:w-auto"
      disabled={busy}
      aria-label={t(isAdmin ? 'admin.action.demoteLabel' : 'admin.action.promoteLabel', { login: account.login })}
      onClick={(event) => onChange(account, event.currentTarget)}
    >
      {t(isAdmin ? 'admin.action.demote' : 'admin.action.promote')}
    </Button>
  </li>;
}
