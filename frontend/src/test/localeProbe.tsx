import { useEffect } from 'react';
import { useI18n, type Locale } from '../i18n';

export type LocaleSetter = (locale: Locale) => void;
export type LocaleSetterRef = { current: LocaleSetter | null };

/**
 * Test-only bridge to the real i18n setter. It adds no product control, so a test can prove that an
 * in-place locale change (the same setter the language switch used) preserves screen state.
 */
export function LocaleProbe({ setterRef }: { setterRef: LocaleSetterRef }) {
  const { setLocale } = useI18n();
  useEffect(() => { setterRef.current = setLocale; }, [setLocale, setterRef]);
  return null;
}
