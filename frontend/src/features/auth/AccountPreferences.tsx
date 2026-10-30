import { useI18n } from '../../i18n';

export function AccountPreferences() {
  const { t } = useI18n();
  return (
    <section className="mt-8 border-t border-line pt-6">
      <h2 className="font-display text-2xl">{t('account.preferences.title')}</h2>
      <p className="mt-2 text-muted">{t('account.preferences.body')}</p>
    </section>
  );
}
