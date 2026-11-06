import { Button } from '../components/ui/button';
import { useI18n, type MessageKey } from '../i18n';
import { THEME_PREFERENCES, type ThemePreference } from './theme';
import { useThemePreference } from './useThemePreference';

const labelKeys: Record<ThemePreference, MessageKey> = {
  system: 'account.appearance.system',
  light: 'account.appearance.light',
  dark: 'account.appearance.dark',
};

/** Three-way Appearance choice; buttons with aria-pressed match the Language switch next to it. */
export function AppearanceControl() {
  const { t } = useI18n();
  const [preference, choose] = useThemePreference();
  return (
    <div role="group" aria-label={t('account.appearance.title')} className="flex items-center gap-1">
      {THEME_PREFERENCES.map((option) => (
        <Button key={option} type="button" variant="quiet" size="small"
          aria-pressed={preference === option}
          className={preference === option ? 'text-ink underline underline-offset-4' : ''}
          onClick={() => choose(option)}>
          {t(labelKeys[option])}
        </Button>
      ))}
    </div>
  );
}
