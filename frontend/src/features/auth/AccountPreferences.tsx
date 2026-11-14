import { LanguageSwitch } from '../../app/LanguageSwitch';
import { AppearanceControl } from '../../theme/AppearanceControl';
import { useI18n } from '../../i18n';

/** One labelled settings section on the flat Account page; the control sits below the copy on phones. */
function SettingsRow({ id, title, description, children }: {
  id: string;
  title: string;
  description: string;
  children: React.ReactNode;
}) {
  return (
    <section aria-labelledby={id} className="border-t border-line pt-6">
      <div className="grid gap-4 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center sm:gap-6">
        <div className="min-w-0">
          <h2 id={id} className="text-h2 text-ink">{title}</h2>
          <p className="mt-1 max-w-prose text-small text-muted">{description}</p>
        </div>
        <div className="min-w-0 sm:justify-self-end">{children}</div>
      </div>
    </section>
  );
}

export function AccountPreferences() {
  const { t } = useI18n();
  return (
    <div className="space-y-6">
      <SettingsRow
        id="account-appearance-heading"
        title={t('account.appearance.title')}
        description={t('account.appearance.body')}
      >
        <AppearanceControl />
      </SettingsRow>
      <SettingsRow
        id="account-language-heading"
        title={t('account.preferences.title')}
        description={t('account.preferences.body')}
      >
        <LanguageSwitch stacked />
      </SettingsRow>
    </div>
  );
}
