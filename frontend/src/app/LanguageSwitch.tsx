import { SegmentedGroup, SegmentedOption } from '../components/ui/segmented';
import { localeNames, useI18n, type Locale } from '../i18n';

/** `stacked` is for the Account settings row on phones; the signed-out footer keeps the inline form. */
export function LanguageSwitch({ stacked = false }: { stacked?: boolean }) {
  const { locale, setLocale, t } = useI18n();
  return (
    <SegmentedGroup label={t('language.label')} stacked={stacked}>
      {(Object.keys(localeNames) as Locale[]).map((code) => (
        <SegmentedOption key={code} lang={code} stacked={stacked} selected={locale === code} onClick={() => setLocale(code)}>
          {localeNames[code]}
        </SegmentedOption>
      ))}
    </SegmentedGroup>
  );
}
