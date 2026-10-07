import { useState } from 'react';
import { Button } from '../components/ui/button';
import { logout } from '../features/auth/sessionApi';
import { useI18n } from '../i18n';

/** Header content for signed-in screens: who is signed in, and sign out. */
export function AccountActions({ login, onSignedOut }: { login: string; onSignedOut: () => void }) {
  const { t } = useI18n();
  const [signingOut, setSigningOut] = useState(false);
  const [failed, setFailed] = useState(false);

  async function signOut() {
    setSigningOut(true);
    setFailed(false);
    const result = await logout();
    if (result === 'ok') {
      onSignedOut();
      return;
    }
    setSigningOut(false);
    setFailed(true);
  }

  return (
    <div className="flex max-w-full flex-col items-end">
      <div className="flex items-center gap-1 text-sm text-muted">
        <span className="max-w-[10rem] truncate sm:max-w-xs">{t('header.signedInAs', { login })}</span>
        <Button type="button" variant="quiet" size="small" onClick={signOut} disabled={signingOut}>
          {signingOut ? t('header.signingOut') : t('header.signOut')}
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
