import { useContext, useRef, type ReactNode, type RefObject } from 'react';
import { useI18n } from '../i18n';
import { BrandMark } from './BrandMark';
import { HeaderSearch } from './HeaderSearch';
import { HouseholdNavigation, HouseholdNavigationContext } from './HouseholdNavigation';
import { HouseholdSwitcher } from './HouseholdSwitcher';
import { LanguageSwitch } from './LanguageSwitch';

/**
 * Shell variants. `household`: full-width top bar, 224 px sidebar from `lg`, fixed bottom bar below it.
 * `householdLess`: signed in without a household — the same top bar with inline navigation and no sidebar.
 * `centered`: signed out (sign-in, setup, invite, boot states).
 */
type Shell = 'household' | 'householdLess' | 'centered';

/**
 * Padding alone only clears the end of the page. When Tab scrolls a control into view, the browser also needs to
 * know the fixed bottom bar covers the viewport's last ~5rem, so every descendant reserves it as scroll margin.
 * Off where the bar is static (short viewports) or replaced by the sidebar (lg).
 */
const KEEP_FOCUS_ABOVE_BAR = '[&_*]:scroll-mb-[calc(6rem+env(safe-area-inset-bottom))] short:[&_*]:scroll-mb-0 lg:[&_*]:scroll-mb-0';

const FRAME: Record<Shell, string> = {
  household: `${KEEP_FOCUS_ABOVE_BAR} flex min-h-dvh w-full flex-col lg:grid lg:grid-cols-[14rem_minmax(0,1fr)] lg:grid-rows-[auto_1fr_auto]`,
  householdLess: 'mx-auto flex min-h-dvh w-full max-w-3xl flex-col',
  centered: 'mx-auto flex min-h-dvh w-full max-w-6xl flex-col',
};
// Full-viewport bar (no framed container). The minimum keeps it at 56 px in the normal case and lets a wrapped
// identity, a long sign-out failure message or the household name grow the header instead of covering main.
// Phones keep one row: the household switcher truncates instead of wrapping onto a second line that pushes main down.
const HEADER = 'flex min-h-14 flex-nowrap items-center gap-x-3 gap-y-1 border-b border-line bg-raised px-4 py-1.5 sm:flex-wrap sm:px-6';
const HEADER_HOUSEHOLD = 'lg:col-span-2 lg:px-6';
const MAIN = 'w-full min-w-0 flex-1 px-4 py-6 sm:px-6 sm:py-8';
/** Bottom padding keeps the last content row clear of the fixed bar (bar height + safe area). */
const MAIN_HOUSEHOLD = 'pb-[calc(6rem+env(safe-area-inset-bottom))] sm:pb-[calc(6rem+env(safe-area-inset-bottom))] short:pb-6 lg:col-start-2 lg:row-start-2 lg:min-w-0 lg:px-8 lg:pb-12 lg:pt-8 xl:px-10';
const FOOTER = 'flex justify-end px-4 py-3 sm:px-6';

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
  const mainRef = useRef<HTMLElement>(null);
  const navigation = useContext(HouseholdNavigationContext);
  const shell: Shell = navigation?.household ? 'household' : navigation ? 'householdLess' : 'centered';
  const household = shell === 'household';
  return (
    <div className={FRAME[shell]}>
      <SkipLink target={mainRef} />
      <header className={`${HEADER} ${household ? HEADER_HOUSEHOLD : ''}`}>
        <BrandMark />
        {navigation ? <HouseholdSwitcher /> : null}
        {navigation ? <HeaderSearch /> : <span className="flex-1" />}
        {actions}
      </header>
      <main id="main" ref={mainRef} tabIndex={-1} className={`${MAIN} ${household ? MAIN_HOUSEHOLD : ''}`}>
        {children}
      </main>
      {navigation ? <HouseholdNavigation rail={household} /> : null}
      {/* Signed-in navigation carries Account, where the language choice lives. The footer switch is for signed-out paths. */}
      {navigation ? null : (
        <footer className={FOOTER}>
          <LanguageSwitch />
        </footer>
      )}
    </div>
  );
}
