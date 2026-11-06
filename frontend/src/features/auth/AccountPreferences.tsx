import { LanguageSwitch } from '../../app/LanguageSwitch';
import { AppearanceControl } from '../../theme/AppearanceControl';
import { useI18n } from '../../i18n';

export function AccountPreferences() {
  const { t } = useI18n();
  return (
    <div>
      <section aria-labelledby="account-appearance-heading" className="border-t border-line pt-6">
        <h2 id="account-appearance-heading" className="font-display text-h2">{t('account.appearance.title')}</h2>
        <p className="mt-2 text-muted">{t('account.appearance.body')}</p>
        <div className="mt-3 inline-flex rounded-xl border border-line bg-raised p-1">
          <AppearanceControl />
        </div>
      </section>
      <section aria-labelledby="account-language-heading" className="mt-8 border-t border-line pt-6">
        <h2 id="account-language-heading" className="font-display text-h2">{t('account.preferences.title')}</h2>
        <p className="mt-2 text-muted">{t('account.preferences.body')}</p>
        <div className="mt-3 inline-flex rounded-xl border border-line bg-raised p-1">
          <LanguageSwitch />
        </div>
      </section>
    </div>
  );
}
