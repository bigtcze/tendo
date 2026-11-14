import { Button } from '../components/ui/button';
import { LoadingLine } from '../components/ui/loading-line';
import { useI18n } from '../i18n';
import { Heading } from './Heading';

/**
 * Signed in, the shell's main fills the viewport, so a state sits in its vertical middle. Signed out, main follows its
 * content and the language footer comes right after it, so the state opens at the top like the rest of the sign-in
 * family instead of pushing the footer into the middle of the page.
 */
function stateColumn(signedOut: boolean) {
  return signedOut
    ? 'mx-auto w-full max-w-2xl lg:mt-10'
    : 'mx-auto flex min-h-[55vh] w-full max-w-2xl flex-col justify-center';
}

/**
 * The secondary full-page states are plain content in that column: the shell names each screen with its own
 * heading alone, so no panel frame or eyebrow line is drawn around it.
 */
export function NotFoundScreen({ signedOut = false }: { signedOut?: boolean }) {
  const { t } = useI18n();
  return (
    <section className={`settle ${stateColumn(signedOut)}`}>
      <Heading className="text-h1 leading-8 font-semibold text-ink">{t('app.notFound.title')}</Heading>
      <p className="mt-3 max-w-prose text-body text-muted">{t('app.notFound.body')}</p>
      <div className="mt-6">
        <a href={signedOut ? '/login' : '/'} onClick={signedOut ? (event) => { event.preventDefault(); window.history.replaceState(null, '', '/login'); window.dispatchEvent(new PopStateEvent('popstate')); } : undefined} className="inline-flex min-h-11 items-center rounded-xl bg-accent px-5 text-control font-semibold text-on-accent hover:bg-accent-strong focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus">
          {t(signedOut ? 'app.notFound.signIn' : 'app.notFound.home')}
        </a>
      </div>
    </section>
  );
}

export function LoadingScreen({ message, signedOut = false }: { message?: string; signedOut?: boolean }) {
  const { t } = useI18n();
  return (
    <section className={stateColumn(signedOut)}>
      <LoadingLine>{message ?? t('app.loading')}</LoadingLine>
    </section>
  );
}

export function ErrorScreen({ onRetry, signedOut = false }: { onRetry: () => void; signedOut?: boolean }) {
  const { t } = useI18n();
  return (
    <section className={`settle ${stateColumn(signedOut)}`}>
      <Heading className="text-h1 leading-8 font-semibold text-ink">{t('app.error.title')}</Heading>
      <p className="mt-3 max-w-prose text-body text-muted">{t('app.error.body')}</p>
      <div className="mt-6">
        <Button type="button" onClick={onRetry}>
          {t('app.error.retry')}
        </Button>
      </div>
    </section>
  );
}
