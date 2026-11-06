import { Button } from '../components/ui/button';
import { useI18n } from '../i18n';
import { Heading } from './Heading';

export function NotFoundScreen({ signedOut = false }: { signedOut?: boolean }) {
  const { t } = useI18n();
  return (
    <section className="settle mx-auto flex min-h-[40vh] w-full max-w-3xl flex-col justify-center space-y-4">
      <div className="max-w-2xl space-y-4 border-l-2 border-accent pl-6">
        <Heading className="font-display text-display leading-tight">{t('app.notFound.title')}</Heading>
        <p className="max-w-prose text-lg text-muted">{t('app.notFound.body')}</p>
      </div>
      <div>
        <a href={signedOut ? '/login' : '/'} onClick={signedOut ? (event) => { event.preventDefault(); window.history.replaceState(null, '', '/login'); window.dispatchEvent(new PopStateEvent('popstate')); } : undefined} className="inline-flex min-h-11 items-center rounded-xl bg-accent px-5 text-base font-medium text-on-accent hover:bg-accent-strong focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus">
          {t(signedOut ? 'app.notFound.signIn' : 'app.notFound.home')}
        </a>
      </div>
    </section>
  );
}

export function LoadingScreen({ message }: { message?: string }) {
  const { t } = useI18n();
  return (
    <section className="mx-auto flex min-h-[40vh] w-full max-w-3xl flex-col justify-center">
      <div className="border-l-2 border-accent pl-6">
        <p role="status" className="text-lg text-ink">{message ?? t('app.loading')}</p>
      </div>
    </section>
  );
}

export function ErrorScreen({ onRetry }: { onRetry: () => void }) {
  const { t } = useI18n();
  return (
    <section className="settle mx-auto flex min-h-[40vh] w-full max-w-3xl flex-col justify-center space-y-5">
      <div className="max-w-2xl space-y-4 border-l-2 border-danger pl-6">
        <Heading className="font-display text-display leading-tight">{t('app.error.title')}</Heading>
        <p className="text-lg text-muted">{t('app.error.body')}</p>
        <Button type="button" onClick={onRetry}>
          {t('app.error.retry')}
        </Button>
      </div>
    </section>
  );
}
