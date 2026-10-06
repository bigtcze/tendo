import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { cs } from './cs';
import { en, type MessageKey } from './en';

export type { MessageKey };
export type Locale = 'en' | 'cs';

export const LOCALE_STORAGE_KEY = 'tendo.locale';

const catalogs: Record<Locale, Record<MessageKey, string>> = { en, cs };

export const localeNames: Record<Locale, string> = { en: 'English', cs: 'Čeština' };

export function detectLocale(): Locale {
  try {
    const stored = localStorage.getItem(LOCALE_STORAGE_KEY);
    if (stored === 'en' || stored === 'cs') return stored;
  } catch {
    // Storage may be unavailable; fall back to browser languages.
  }
  const languages = navigator.languages?.length ? navigator.languages : [navigator.language];
  return languages.some((l) => l?.toLowerCase().startsWith('cs')) ? 'cs' : 'en';
}

type Translate = (key: MessageKey, params?: Record<string, string>) => string;

interface I18nValue {
  locale: Locale;
  setLocale: (locale: Locale) => void;
  t: Translate;
}

const I18nContext = createContext<I18nValue | null>(null);

export function I18nProvider({ children }: { children: ReactNode }) {
  const [locale, setLocaleState] = useState<Locale>(detectLocale);

  useEffect(() => {
    document.documentElement.lang = locale;
  }, [locale]);

  const setLocale = useCallback((next: Locale) => {
    setLocaleState(next);
    try {
      localStorage.setItem(LOCALE_STORAGE_KEY, next);
    } catch {
      // Preference simply won't persist.
    }
  }, []);

  const value = useMemo<I18nValue>(() => {
    const t: Translate = (key, params) => {
      let text = catalogs[locale][key];
      for (const [name, v] of Object.entries(params ?? {})) text = text.replaceAll(`{${name}}`, v);
      return text;
    };
    return { locale, setLocale, t };
  }, [locale, setLocale]);

  return <I18nContext value={value}>{children}</I18nContext>;
}

export function useI18n(): I18nValue {
  const value = useContext(I18nContext);
  if (!value) throw new Error('useI18n must be used inside I18nProvider');
  return value;
}
