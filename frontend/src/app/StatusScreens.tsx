import { Button } from '../components/ui/button';
import { useI18n } from '../i18n';
import { Heading } from './Heading';

export function LoadingScreen({ message }: { message?: string }) {
  const { t } = useI18n();
  return (
    <p role="status" className="text-muted">
      {message ?? t('app.loading')}
    </p>
  );
}

export function ErrorScreen({ onRetry }: { onRetry: () => void }) {
  const { t } = useI18n();
  return (
    <section className="settle space-y-4">
      <Heading className="font-display text-3xl leading-tight sm:text-4xl">{t('app.error.title')}</Heading>
      <p className="text-muted">{t('app.error.body')}</p>
      <Button type="button" onClick={onRetry}>
        {t('app.error.retry')}
      </Button>
    </section>
  );
}
