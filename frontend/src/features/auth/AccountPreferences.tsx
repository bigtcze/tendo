import { LanguageSwitch } from '../../app/LanguageSwitch';
import { useI18n } from '../../i18n';

export function AccountPreferences() {
  const { t } = useI18n();
  return (
    <section aria-labelledby="account-language-heading" className="border-t border-line pt-6 lg:mt-0">
      <h2 id="account-language-heading" className="font-display text-h2">{t('account.preferences.title')}</h2>
      <p className="mt-2 text-muted">{t('account.preferences.body')}</p>
      <div className="mt-3 inline-flex rounded-xl border border-line bg-raised p-1">
        <LanguageSwitch />
      </div>
    </section>
  );
}
