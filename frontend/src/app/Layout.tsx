import { useRef, type ReactNode } from 'react';
import { Button } from '../components/ui/button';
import { localeNames, useI18n, type Locale } from '../i18n';

function LanguageSwitch() {
  const { locale, setLocale, t } = useI18n();
  return (
    <div role="group" aria-label={t('language.label')} className="flex items-center gap-1">
      {(Object.keys(localeNames) as Locale[]).map((code) => (
        <Button
          key={code}
          type="button"
          variant="quiet"
          size="small"
          lang={code}
          aria-pressed={locale === code}
          className={locale === code ? 'text-ink underline underline-offset-4' : ''}
          onClick={() => setLocale(code)}
        >
          {localeNames[code]}
        </Button>
      ))}
    </div>
  );
}

export function Layout({ actions, children }: { actions?: ReactNode; children: ReactNode }) {
  const { t } = useI18n();
  const mainRef = useRef<HTMLElement>(null);
  return (
    <div className="mx-auto flex min-h-dvh w-full max-w-2xl flex-col px-5 sm:px-8">
      <a
        href="#main"
        onClick={(event) => { event.preventDefault(); mainRef.current?.focus(); mainRef.current?.scrollIntoView(); }}
        className="sr-only focus-visible:not-sr-only focus-visible:absolute focus-visible:top-2 focus-visible:left-2 focus-visible:rounded-lg focus-visible:bg-white focus-visible:p-3"
      >
        {t('app.skipToContent')}
      </a>
      <header className="flex min-h-16 flex-wrap items-center justify-between gap-x-4 py-3">
        <span className="font-display text-xl tracking-tight">{t('app.name')}</span>
        {actions}
      </header>
      <main id="main" ref={mainRef} tabIndex={-1} className="flex-1 py-10 sm:py-16">
        {children}
      </main>
      <footer className="flex justify-end py-4">
        <LanguageSwitch />
      </footer>
    </div>
  );
}
