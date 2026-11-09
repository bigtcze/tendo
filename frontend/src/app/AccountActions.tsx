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

/** Signed-in identity: login, avatar and sign out on desktop; a quiet account affordance and compact sign out on phones. */
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
      <div className="flex items-center gap-2 text-sm text-muted">
        <span className="hidden max-w-[16rem] truncate lg:inline">{t('header.signedInAs', { login })}</span>
        <span aria-hidden="true" data-avatar
          className="hidden size-8 shrink-0 place-items-center rounded-full bg-nav-selected text-sm font-semibold text-ink lg:grid">
          {initial}
        </span>
        {navigation ? (
          <NavLink
            href="/account"
            onNavigate={navigation.actions.account}
            aria-label={t('header.account')}
            className="grid size-11 shrink-0 place-items-center rounded-full bg-nav-selected text-sm font-semibold text-ink transition-colors hover:bg-line focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus lg:hidden"
          >
            <span aria-hidden="true">{initial}</span>
          </NavLink>
        ) : null}
        <Button
          type="button"
          variant="quiet"
          size="small"
          aria-label={t('header.signOut')}
          disabled={signingOut}
          onClick={signOut}
          className="min-w-11 px-2 lg:px-3"
        >
          <svg aria-hidden="true" focusable="false" viewBox="0 0 24 24" fill="none" stroke="currentColor"
            strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round" className="size-5 lg:hidden">
            <path d="M14 4h4.5A1.5 1.5 0 0 1 20 5.5v13a1.5 1.5 0 0 1-1.5 1.5H14" />
            <path d="M10 8l4 4-4 4M14 12H3" />
          </svg>
          <span className="hidden lg:inline">{signingOut ? t('header.signingOut') : t('header.signOut')}</span>
        </Button>
      </div>
      {failed ? (
        <p role="alert" className="text-sm text-danger">
          {t('header.signOutError')}
        </p>
      ) : null}
    </div>
  );
}
