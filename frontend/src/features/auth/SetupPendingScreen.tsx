import { useI18n } from '../../i18n';
import { Heading } from '../../app/Heading';

export function SetupPendingScreen() {
  const { t } = useI18n();
  return (
    <section className="settle space-y-4">
      <Heading className="font-display text-3xl leading-tight sm:text-4xl">{t('setup.title')}</Heading>
      <p className="text-lg text-muted">{t('setup.body')}</p>
    </section>
  );
}
