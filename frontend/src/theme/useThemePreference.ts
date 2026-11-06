import { useCallback, useState } from 'react';
import { readThemePreference, setThemePreference, type ThemePreference } from './theme';

/** Saved appearance choice for the Account control. Live OS changes need no state: CSS light-dark() follows them. */
export function useThemePreference(): [ThemePreference, (next: ThemePreference) => void] {
  const [preference, setPreference] = useState<ThemePreference>(readThemePreference);
  const choose = useCallback((next: ThemePreference) => {
    setThemePreference(next);
    setPreference(next);
  }, []);
  return [preference, choose];
}
