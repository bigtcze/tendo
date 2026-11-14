import { Heading } from '../../app/Heading';
import { Button, buttonVariants } from '../../components/ui/button';
import { Notice } from '../../components/ui/notice';
import { useI18n, type MessageKey } from '../../i18n';
import { OidcContinueLinkForm } from './OidcContinueLinkForm';
import { useOidcContinue, type ContinueLoad } from './useOidcContinue';
import { useOidcProviderStatus } from './useOidcProviderStatus';
import type { Session } from './sessionApi';

type Flow = ReturnType<typeof useOidcContinue>;

const noFocus = () => {};

function LoadingPanel() {
  const { t } = useI18n();
  return <p role="status" className="p-5 text-body text-muted sm:p-7">{t('oidc.continue.loading')}</p>;
}

function RecoveryPanel({ load, retry, cancel }: { load: ContinueLoad; retry: () => void; cancel: () => void }) {
  const { t } = useI18n();
  return (
    <div className="space-y-4 p-5 sm:p-7">
      <Notice tone="error">
        {t(load === 'rateLimited' ? 'oidc.continue.rateLimited' : 'oidc.continue.unavailable')}
      </Notice>
      <div className="flex flex-wrap gap-2">
        <Button onClick={retry}>{t('oidc.continue.retry')}</Button>
        <Button variant="quiet" onClick={cancel}>{t('oidc.continue.cancel')}</Button>
      </div>
    </div>
  );
}

function AlreadySignedInPanel() {
  const { t } = useI18n();
  return (
    <div className="space-y-4 p-5 sm:p-7">
      <Notice>{t('oidc.continue.alreadySignedIn')}</Notice>
      <a href="/" className={buttonVariants({ variant: 'secondary' })}>{t('oidc.continue.reload')}</a>
    </div>
  );
}

function ConflictPanel({ back }: { back: () => void }) {
  const { t } = useI18n();
  return (
    <div className="space-y-4 p-5 sm:p-7">
      <Notice tone="error">{t('oidc.continue.error.conflict')}</Notice>
      <Button variant="secondary" autoFocus onClick={back}>{t('oidc.continue.backToSignIn')}</Button>
    </div>
  );
}

function OrDivider() {
  const { t } = useI18n();
  return (
    <div className="flex items-center gap-3 text-small text-muted" aria-hidden="true">
      <span className="h-px flex-1 bg-line" />
      {t('oidc.continue.or')}
      <span className="h-px flex-1 bg-line" />
    </div>
  );
}

function ChoosePanel({ invitation, flow }: { invitation: boolean; flow: Flow }) {
  const { t } = useI18n();
  const pending = flow.action !== null;
  const notice = flow.linkOpen
    ? null
    : flow.failure === 'rateLimited'
      ? t('oidc.continue.error.rateLimited')
      : flow.failure === 'unavailable' ? t('oidc.continue.error.unavailable') : null;

  return (
    <div className="space-y-5 p-5 sm:p-7">
      {invitation ? <Notice>{t('oidc.continue.invitation')}</Notice> : null}
      {notice ? <Notice tone="error">{notice}</Notice> : null}
      {flow.linkOpen ? (
        <OidcContinueLinkForm
          pending={pending}
          failure={flow.failure}
          loginName={flow.loginName}
          setLoginName={flow.setLoginName}
          password={flow.password}
          setPassword={flow.setPassword}
          submit={flow.submitLink}
          cancel={flow.closeLink}
        />
      ) : (
        <>
          <section aria-labelledby="oidc-create-heading">
            <h2 id="oidc-create-heading" className="text-h2 text-ink">{t('oidc.continue.create.title')}</h2>
            <p className="mt-1 text-body text-muted">{t('oidc.continue.create.body')}</p>
            <Button className="mt-3 w-full" disabled={pending} onClick={flow.createAccount}>
              {flow.action === 'create' ? t('oidc.continue.create.pending') : t('oidc.continue.create.action')}
            </Button>
          </section>
          <OrDivider />
          <section aria-labelledby="oidc-link-heading">
            <h2 id="oidc-link-heading" className="text-h2 text-ink">{t('oidc.continue.link.title')}</h2>
            <p className="mt-1 text-body text-muted">{t('oidc.continue.link.body')}</p>
            <Button variant="secondary" className="mt-3 w-full" disabled={pending} onClick={flow.openLink}>
              {t('oidc.continue.link.action')}
            </Button>
          </section>
        </>
      )}
      <Button variant="quiet" className="w-full" disabled={pending} onClick={flow.cancel}>
        {flow.action === 'cancel' ? t('oidc.continue.cancelling') : t('oidc.continue.cancel')}
      </Button>
    </div>
  );
}

function ContinueBody({ flow }: { flow: Flow }) {
  if (flow.load === 'loading') return <LoadingPanel />;
  if (flow.load === 'rateLimited' || flow.load === 'unavailable') {
    return <RecoveryPanel load={flow.load} retry={flow.retry} cancel={flow.cancel} />;
  }
  if (flow.load === 'alreadySignedIn') return <AlreadySignedInPanel />;
  if (flow.failure === 'conflict') return <ConflictPanel back={flow.backToSignIn} />;
  return <ChoosePanel invitation={Boolean(flow.status?.invitation)} flow={flow} />;
}

/**
 * One calm decision after a provider sign-in that has no Tendo account yet:
 * create a new account, or bind the sign-in to the account the person already uses.
 */
export function OidcContinueScreen({ onSignedIn, onCancelled, onExpired }: {
  onSignedIn: (session: Session, notice: MessageKey | null) => void;
  onCancelled: () => void;
  onExpired: () => void;
}) {
  const { t } = useI18n();
  const { provider } = useOidcProviderStatus(noFocus, noFocus);
  const flow = useOidcContinue({ signedIn: onSignedIn, expired: onExpired, cancelled: onCancelled });
  const title = provider
    ? t('oidc.continue.title', { provider })
    : t('oidc.continue.title.generic');

  return (
    <section className="settle mx-auto w-full max-w-md lg:mt-10">
      <Heading className="text-center text-[2.125rem] font-bold leading-[2.25rem] text-ink sm:text-display">{title}</Heading>
      {flow.load === 'loading' || flow.load === 'ready'
        ? <p className="mt-3 text-center text-base text-muted">{t('oidc.continue.intro')}</p>
        : null}
      <div className="mt-8 w-full overflow-hidden rounded-2xl border border-line bg-raised">
        <ContinueBody flow={flow} />
      </div>
    </section>
  );
}
