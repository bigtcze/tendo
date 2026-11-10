import { useEffect, useRef, useState } from 'react';
import { Button } from '../../components/ui/button';
import { useI18n, type MessageKey } from '../../i18n';
import { browserNavigation, startOidc, type StartResult } from '../auth/oidcApi';
import { useOidcProviderStatus } from '../auth/useOidcProviderStatus';

type StartFailure = Exclude<StartResult['kind'], 'redirect'>;

const failureKeys: Record<StartFailure, MessageKey> = {
  invalidInvitation: 'invite.error.invalid',
  wrongPassword: 'oidc.start.unavailable',
  unauthenticated: 'oidc.start.unavailable',
  alreadySignedIn: 'oidc.start.unavailable',
  disabled: 'oidc.start.disabled',
  rateLimited: 'oidc.start.rateLimited',
  providerUnavailable: 'oidc.start.providerUnavailable',
  unavailable: 'oidc.start.unavailable',
};

const noFocus = () => {};

/**
 * Offers the household provider as an alternative to creating a password account on the invite screen.
 * The invitation token travels with the provider round trip so the invitation survives it.
 */
export function InviteProviderSignIn({ token, disabled }: { token: string; disabled: boolean }) {
  const { t } = useI18n();
  const buttonRef = useRef<HTMLButtonElement>(null);
  const [pending, setPending] = useState(false);
  const [failure, setFailure] = useState<StartFailure | null>(null);
  const mountedRef = useRef(false);
  const pendingRef = useRef(false);
  const status = useOidcProviderStatus(noFocus, noFocus);

  useEffect(() => {
    mountedRef.current = true;
    return () => { mountedRef.current = false; };
  }, []);

  async function start() {
    if (pendingRef.current || disabled) return;
    pendingRef.current = true;
    setPending(true);
    setFailure(null);
    const result = await startOidc({ purpose: 'login', invitationToken: token });
    if (!mountedRef.current) return;
    if (result.kind === 'redirect') {
      browserNavigation.assign(result.url);
      return;
    }
    pendingRef.current = false;
    setPending(false);
    if (result.kind === 'disabled') status.disableProvider();
    else buttonRef.current?.focus();
    setFailure(result.kind);
  }

  if (status.status !== 'enabled' || !status.provider) return null;

  return (
    <div className="mb-5">
      <Button
        ref={buttonRef}
        type="button"
        variant="secondary"
        disabled={pending || disabled}
        aria-describedby={failure ? 'invite-oidc-error' : undefined}
        className="w-full"
        onClick={() => void start()}
      >
        {pending
          ? t('invite.oidc.submitting', { provider: status.provider })
          : t('invite.oidc.submit', { provider: status.provider })}
      </Button>
      <div id="invite-oidc-error" role="alert" className="empty:contents">
        {failure ? (
          <div className="mt-3 rounded-xl border border-danger bg-danger-bg px-4 py-3 text-small text-danger">
            {t(failureKeys[failure], { provider: status.provider })}
          </div>
        ) : null}
      </div>
      <div className="mt-5 flex items-center gap-3 text-small text-muted" aria-hidden="true">
        <span className="h-px flex-1 bg-line" />
        {t('login.or')}
        <span className="h-px flex-1 bg-line" />
      </div>
    </div>
  );
}
