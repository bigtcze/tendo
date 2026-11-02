import { useContext, useRef, type ReactNode, type RefObject } from 'react';
import { useI18n } from '../i18n';
import { HouseholdNavigation, HouseholdNavigationContext } from './HouseholdNavigation';
import { LanguageSwitch } from './LanguageSwitch';

/**
 * Shell variants. `household`: rail from `lg`, fixed bottom bar below it. `householdLess`: signed in without a
 * household — small inline navigation, no rail space. `centered`: signed out (sign-in, setup, invite, boot states).
 */
type Shell = 'household' | 'householdLess' | 'centered';

/**
 * Padding alone only clears the end of the page. When Tab scrolls a control into view, the browser also needs to
 * know the fixed bottom bar covers the viewport's last ~5rem, so every descendant reserves it as scroll margin.
 * Off where the bar is static (short viewports) or replaced by the rail (lg).
 */
const KEEP_FOCUS_ABOVE_BAR = '[&_*]:scroll-mb-[calc(5rem+env(safe-area-inset-bottom))] short:[&_*]:scroll-mb-0 lg:[&_*]:scroll-mb-0';

const FRAME: Record<Shell, string> = {
  household: `${KEEP_FOCUS_ABOVE_BAR} flex min-h-dvh flex-col lg:mx-auto lg:grid lg:w-full lg:max-w-[90rem]`
    + ' lg:grid-cols-[15rem_minmax(0,1fr)] lg:grid-rows-[auto_1fr_auto]',
  householdLess: 'mx-auto flex min-h-dvh max-w-3xl flex-col',
  centered: 'mx-auto flex min-h-dvh max-w-3xl flex-col',
};
const HEADER = 'flex min-h-16 flex-wrap items-center justify-between gap-x-4 gap-y-1 border-b border-line px-5 py-3 sm:px-8';
const HEADER_HOUSEHOLD = 'lg:col-span-2 lg:px-8';
const MAIN = 'w-full min-w-0 flex-1 px-5 py-6 sm:px-8 sm:py-8';
/** Bottom padding keeps the last content row clear of the fixed bar (bar height + safe area). */
const MAIN_HOUSEHOLD = 'pb-[calc(6rem+env(safe-area-inset-bottom))] short:pb-6 lg:col-start-2 lg:row-start-2 lg:max-w-6xl lg:px-12 lg:pb-12 lg:pt-10';
const FOOTER = 'flex justify-end px-5 py-3 sm:px-8';
const FOOTER_HOUSEHOLD = 'border-t border-line pb-[calc(5rem+env(safe-area-inset-bottom))] short:pb-3 lg:col-span-2 lg:row-start-3 lg:pb-3';

function SkipLink({ target }: { target: RefObject<HTMLElement | null> }) {
  const { t } = useI18n();
  return (
    <a href="#main" onClick={(event) => { event.preventDefault(); target.current?.focus(); target.current?.scrollIntoView(); }}
      className="sr-only focus-visible:not-sr-only focus-visible:absolute focus-visible:top-2 focus-visible:left-2 focus-visible:z-30 focus-visible:rounded-lg focus-visible:border focus-visible:border-line focus-visible:bg-raised focus-visible:p-3 focus-visible:text-ink focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus">
      {t('app.skipToContent')}
    </a>
  );
}

export function Layout({ actions, children }: { actions?: ReactNode; children: ReactNode }) {
  const { t } = useI18n();
  const mainRef = useRef<HTMLElement>(null);
  const navigation = useContext(HouseholdNavigationContext);
  const shell: Shell = navigation?.household ? 'household' : navigation ? 'householdLess' : 'centered';
  const household = shell === 'household';
  return (
    <div className={FRAME[shell]}>
      <SkipLink target={mainRef} />
      <header className={`${HEADER} ${household ? HEADER_HOUSEHOLD : ''}`}>
        <span className="font-display text-xl tracking-tight">{t('app.name')}</span>
        {actions}
      </header>
      {navigation ? <HouseholdNavigation rail={household} /> : null}
      <main id="main" ref={mainRef} tabIndex={-1} className={`${MAIN} ${household ? MAIN_HOUSEHOLD : ''}`}>
        {children}
      </main>
      <footer className={`${FOOTER} ${household ? FOOTER_HOUSEHOLD : ''}`}>
        <LanguageSwitch />
      </footer>
    </div>
  );
}
