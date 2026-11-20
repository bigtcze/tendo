import { useState } from 'react';
import { logout } from '../auth/sessionApi';
import { useI18n } from '../../i18n';
import { Fill } from './ConnectFrame';

/**
 * Who is about to receive access. "Not you?" signs this browser out so a different person can sign in; a failure is
 * said in place, because the consent stays usable until the sign-out really happens.
 */
export function ConnectSignedIn({ login, busy, onSignOut }: { login: string; busy: boolean; onSignOut: () => void }) {
  const { t } = useI18n();
  const [failed, setFailed] = useState(false);
  const [pending, setPending] = useState(false);

  async function signOut() {
    setPending(true);
    setFailed(false);
    const result = await logout();
    if (result === 'ok') {
      onSignOut();
      return;
    }
    setPending(false);
    setFailed(true);
  }

  return <div className="space-y-2 text-small text-muted">
    <p><Fill id="connect.signedInAs" values={{ login }} /></p>
    <button
      type="button"
      onClick={() => void signOut()}
      disabled={pending || busy}
      className="inline-flex min-h-11 items-center rounded-xl px-3 font-semibold text-ink underline underline-offset-4 hover:bg-sand focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus disabled:cursor-not-allowed disabled:opacity-60"
    >
      {t('connect.notYou')}
    </button>
    {failed ? <p role="alert" className="text-danger">{t('header.signOutError')}</p> : null}
  </div>;
}
