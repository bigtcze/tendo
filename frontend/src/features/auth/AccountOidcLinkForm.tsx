import { useRef } from 'react';
import { useI18n, type MessageKey } from '../../i18n';
import { Button } from '../../components/ui/button';
import { Input } from '../../components/ui/input';
import { Label } from '../../components/ui/label';
import { useOidcLinkStart, type LinkStartFailure } from './useOidcLinkStart';

const errorKeys: Record<LinkStartFailure, MessageKey> = {
  wrongPassword: 'account.oidc.wrongPassword',
  alreadySignedIn: 'oidc.start.unavailable',
  disabled: 'oidc.start.disabled',
  rateLimited: 'oidc.start.rateLimited',
  providerUnavailable: 'oidc.start.providerUnavailable',
  unavailable: 'oidc.start.unavailable',
};

export function AccountOidcLinkForm({ provider, onSignedOut, onDisabled }: {
  provider: string;
  onSignedOut: () => void;
  onDisabled: () => void;
}) {
  const { t } = useI18n();
  const passwordRef = useRef<HTMLInputElement>(null);
  const { password, setPassword, pending, error, connect } = useOidcLinkStart(
    onSignedOut,
    onDisabled,
    () => passwordRef.current?.focus(),
  );
  return (
    <form onSubmit={connect} className="mt-5 space-y-5" noValidate>
      <div className="space-y-2">
        <Label htmlFor="account-current-password">{t('account.oidc.password')}</Label>
        <Input
          id="account-current-password"
          name="currentPassword"
          type="password"
          autoComplete="current-password"
          required
          ref={passwordRef}
          value={password}
          onChange={(event) => setPassword(event.target.value)}
          aria-invalid={error === 'wrongPassword'}
          aria-describedby={error ? 'account-oidc-error' : undefined}
        />
      </div>
      <div id="account-oidc-error" role="alert" className="text-base text-danger">
        {error ? t(errorKeys[error], { provider }) : null}
      </div>
      <Button type="submit" disabled={pending} className="w-full sm:w-auto">
        {pending ? t('account.oidc.connecting', { provider }) : t('account.oidc.connect', { provider })}
      </Button>
    </form>
  );
}
