import { SegmentedGroup, SegmentedOption } from '../components/ui/segmented';
import { useI18n, type MessageKey } from '../i18n';
import { THEME_PREFERENCES, type ThemePreference } from './theme';
import { useThemePreference } from './useThemePreference';

const labelKeys: Record<ThemePreference, MessageKey> = {
  system: 'account.appearance.system',
  light: 'account.appearance.light',
  dark: 'account.appearance.dark',
};

/** Three-way Appearance choice; full-width rows on phones, one row from `sm`; selected carries a visible check. */
export function AppearanceControl() {
  const { t } = useI18n();
  const [preference, choose] = useThemePreference();
  return (
    <SegmentedGroup label={t('account.appearance.title')} stacked>
      {THEME_PREFERENCES.map((option) => (
        <SegmentedOption key={option} stacked selected={preference === option} onClick={() => choose(option)}>
          {t(labelKeys[option])}
        </SegmentedOption>
      ))}
    </SegmentedGroup>
  );
}
