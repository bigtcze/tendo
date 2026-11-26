import { useCallback, useContext, useId, useRef, useState } from 'react';
import { SignOutContext } from './SignOutContext';
import { NavLink } from './NavLink';
import { HouseholdNavigationContext } from './HouseholdNavigation';
import { useDismiss } from './useDismiss';
import { logout } from '../features/auth/sessionApi';
import { useI18n } from '../i18n';

const FOCUS = 'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus';
/** Phones: the 44 px initial alone. From lg the login and a chevron join it, so the bar still says who is signed in. */
const TRIGGER = `flex min-h-11 min-w-11 max-w-full items-center justify-center gap-2 rounded-full text-small font-medium text-ink transition-colors hover:bg-sand lg:rounded-lg lg:pl-1 lg:pr-2 ${FOCUS}`;
// Phones pin the panel to the header's own width (16 px insets); from sm it hangs under the trigger's right edge.
const PANEL = 'absolute right-0 top-[calc(100%+0.375rem)] z-30 w-[min(17rem,calc(100vw-2rem))] max-sm:inset-x-4 max-sm:w-auto rounded-xl border border-line bg-raised p-1.5 shadow-[0_20px_50px_-32px_rgb(20_18_14/0.5)]';
const ROW = `flex min-h-11 w-full items-center rounded-lg px-2.5 text-left text-small font-medium text-ink transition-colors hover:bg-sand ${FOCUS}`;

function initialOf(login: string): string {
  return login.trim().charAt(0).toLocaleUpperCase() || '?';
}

function Chevron({ open }: { open: boolean }) {
  return <svg aria-hidden="true" focusable="false" viewBox="0 0 24 24" fill="none" stroke="currentColor"
    strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round"
    className={`hidden size-4 shrink-0 text-muted transition-transform motion-reduce:transition-none lg:block ${open ? 'rotate-180' : ''}`}>
    <path d="m6 9 6 6 6-6" />
  </svg>;
}

/**
 * The top bar's one account control: the account's initial (and, from lg, its login) opens a small menu that says who
 * is signed in, links to the account settings and signs out in words. The bar itself carries no identity sentence and
 * no icon-only action.
 *
 * Sign out in flight is `aria-disabled`, never `disabled`: a disabled button drops keyboard focus to the document,
 * which would close the menu and with it a failure message the reader still needs.
 */
export function AccountActions({ login, onSignedOut }: { login: string; onSignedOut: () => void }) {
  const { t } = useI18n();
  const contextSignOut = useContext(SignOutContext);
  const navigation = useContext(HouseholdNavigationContext);
  const [open, setOpen] = useState(false);
  const [signingOut, setSigningOut] = useState(false);
  const [failed, setFailed] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const panelId = useId();
  const close = useCallback(() => setOpen(false), []);
  useDismiss(open, close, rootRef, buttonRef);

  async function signOut() {
    if (signingOut) return;
    setSigningOut(true);
    setFailed(false);
    const result = await logout();
    if (result === 'ok') {
      (contextSignOut ?? onSignedOut)();
      return;
    }
    setSigningOut(false);
    setFailed(true);
    // The reader may have left the menu while the request was out; reopen it so the failure is seen, not hidden in a
    // closed panel. Focus stays where it is: the alert announces itself.
    setOpen(true);
  }

  function toggle() {
    // A fresh opening starts clean; an in-flight sign out keeps its own state.
    if (!open && !signingOut) setFailed(false);
    setOpen(!open);
  }

  return (
    <div ref={rootRef} className="relative max-w-full max-sm:static">
      <button
        ref={buttonRef}
        type="button"
        aria-label={t('header.menu', { login })}
        aria-expanded={open}
        aria-controls={panelId}
        onClick={toggle}
        className={TRIGGER}
      >
        {/* The initial is drawn, not written: as text it would sit before the login in the button's visible label,
            which then no longer matches the accessible name (WCAG 2.5.3, axe label-content-name-mismatch). Both read DOM text
            and ignore generated content, so keep the initial in ::before; aria-hidden alone does not exempt it. */}
        <span aria-hidden="true" data-avatar data-initial={initialOf(login)}
          className="grid size-8 shrink-0 place-items-center rounded-full bg-nav-selected text-sm font-semibold text-ink before:content-[attr(data-initial)]" />
        <span className="hidden max-w-[12rem] truncate lg:inline">{login}</span>
        <Chevron open={open} />
      </button>
      <div id={panelId} role="group" aria-label={t('header.menu', { login })} hidden={!open} className={PANEL}>
        <p className="break-words px-2.5 py-2 text-meta text-muted">{t('header.signedInAs', { login })}</p>
        {navigation ? (
          <NavLink
            href="/account"
            onNavigate={() => { setOpen(false); navigation.actions.account(); }}
            className={ROW}
          >
            {t('header.account')}
          </NavLink>
        ) : null}
        <div className="mt-1 border-t border-line pt-1">
          <button
            type="button"
            aria-disabled={signingOut}
            onClick={() => void signOut()}
            className={`${ROW} aria-disabled:cursor-not-allowed aria-disabled:opacity-60`}
          >
            {signingOut ? t('header.signingOut') : t('header.signOut')}
          </button>
          {failed ? (
            <p role="alert" className="px-2.5 pb-1.5 pt-1 text-meta text-danger">
              {t('header.signOutError')}
            </p>
          ) : null}
        </div>
      </div>
    </div>
  );
}
