import { useContext, useState } from 'react';
import { SignOutContext } from './SignOutContext';
import { Button } from '../components/ui/button';
import { NavLink } from './NavLink';
import { HouseholdNavigationContext } from './HouseholdNavigation';
import { logout } from '../features/auth/sessionApi';
import { useI18n } from '../i18n';

function initialOf(login: string): string {
  return login.trim().charAt(0).toLocaleUpperCase() || '?';
}

/**
 * The account cluster: the avatar opens Account, and a quiet Sign out stays visible (no hidden menu)
 * so ending a session is always one clear click away. The login is named only where the bar has room.
 */
export function AccountActions({ login, onSignedOut }: { login: string; onSignedOut: () => void }) {
  const { t } = useI18n();
  const contextSignOut = useContext(SignOutContext);
  const navigation = useContext(HouseholdNavigationContext);
  const [signingOut, setSigningOut] = useState(false);
  const [failed, setFailed] = useState(false);
  const initial = initialOf(login);

  async function signOut() {
    setSigningOut(true);
    setFailed(false);
    const result = await logout();
    if (result === 'ok') {
      (contextSignOut ?? onSignedOut)();
      return;
    }
    setSigningOut(false);
    setFailed(true);
  }

  return (
    <div className="flex max-w-full flex-col items-end">
      <div className="flex items-center gap-1.5 text-small text-muted">
        {navigation ? (
          <NavLink
            href="/account"
            onNavigate={navigation.actions.account}
            className="flex min-h-11 min-w-11 items-center gap-2 rounded-full pr-1 text-small text-muted transition-colors hover:text-ink focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
          >
            <span aria-hidden="true" data-avatar
              className="grid size-8 shrink-0 place-items-center rounded-full bg-nav-selected text-small font-semibold text-ink">
              {initial}
            </span>
            {/* The accessible name must contain the visible text, so the label is the link's own hidden text. */}
            <span className="sr-only">{t('header.account')}</span>
            <span aria-hidden="true" className="hidden max-w-[12rem] truncate xl:inline">{t('header.signedInAs', { login })}</span>
          </NavLink>
        ) : null}
        <Button
          type="button"
          variant="quiet"
          size="small"
          aria-label={t('header.signOut')}
          disabled={signingOut}
          onClick={signOut}
          className="min-w-11 px-2 xl:px-3"
        >
          <svg aria-hidden="true" focusable="false" viewBox="0 0 24 24" fill="none" stroke="currentColor"
            strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round" className="size-5 xl:hidden">
            <path d="M14 4h4.5A1.5 1.5 0 0 1 20 5.5v13a1.5 1.5 0 0 1-1.5 1.5H14" />
            <path d="M10 8l4 4-4 4M14 12H3" />
          </svg>
          <span className="hidden xl:inline">{signingOut ? t('header.signingOut') : t('header.signOut')}</span>
        </Button>
      </div>
      {failed ? (
        <p role="alert" className="text-small text-danger">
          {t('header.signOutError')}
        </p>
      ) : null}
    </div>
  );
}
