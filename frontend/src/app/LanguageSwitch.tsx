import { Button } from '../components/ui/button';
import { localeNames, useI18n, type Locale } from '../i18n';

export function LanguageSwitch() {
  const { locale, setLocale, t } = useI18n();
  return (
    <div role="group" aria-label={t('language.label')} className="flex items-center gap-1">
      {(Object.keys(localeNames) as Locale[]).map((code) => (
        <Button key={code} type="button" variant="quiet" size="small" lang={code}
          aria-pressed={locale === code}
          className={locale === code ? 'text-ink underline underline-offset-4' : ''}
          onClick={() => setLocale(code)}>
          {localeNames[code]}
        </Button>
      ))}
    </div>
  );
}
