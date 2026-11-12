import { useContext, useRef, type ReactNode, type RefObject } from 'react';
import { useI18n } from '../i18n';
import { BrandMark } from './BrandMark';
import { useHouseholdData } from './HouseholdData';
import { HouseholdMenu } from './HouseholdMenu';
import { HouseholdNavigation, HouseholdNavigationContext } from './HouseholdNavigation';
import { LanguageSwitch } from './LanguageSwitch';

/**
 * Shell variants. `household`: quiet sidebar from `lg`, fixed bottom bar below it. `householdLess`:
 * signed in without a household — small inline navigation, no rail space. `centered`: signed out
 * (sign-in, setup, invite, boot states).
 */
type Shell = 'household' | 'householdLess' | 'centered';

/**
 * Padding alone only clears the end of the page. When Tab scrolls a control into view, the browser also needs to
 * know the fixed bottom bar covers the viewport's last ~5rem, so every descendant reserves it as scroll margin.
 * Off where the bar is static (short viewports) or replaced by the sidebar (lg).
 */
const KEEP_FOCUS_ABOVE_BAR = '[&_*]:scroll-mb-[calc(6.5rem+env(safe-area-inset-bottom))] short:[&_*]:scroll-mb-0 lg:[&_*]:scroll-mb-0';

const FRAME: Record<Shell, string> = {
  household: `${KEEP_FOCUS_ABOVE_BAR} flex min-h-dvh flex-col`,
  householdLess: 'mx-auto flex min-h-dvh w-full max-w-3xl flex-col',
  centered: 'mx-auto flex min-h-dvh w-full max-w-6xl flex-col',
};
/**
 * The bar spans the whole viewport and never a framed banner. It only gets a minimum height: a long
 * login or a sign-out failure must grow it instead of painting over main. Phones wrap when needed.
 */
const HEADER = 'flex min-h-14 flex-wrap items-center justify-between gap-x-3 gap-y-1 border-b border-line bg-raised px-4 py-1 sm:px-6 lg:min-h-16 lg:px-8';
const MAIN = 'w-full min-w-0 flex-1 px-4 py-6 sm:px-6 lg:px-8 lg:py-10 xl:px-10';
/** Bottom padding keeps the last content clear of the fixed phone bar; the sidebar needs none. */
const MAIN_HOUSEHOLD = 'pb-[calc(6.5rem+env(safe-area-inset-bottom))] short:pb-6 lg:col-start-2 lg:row-start-1 lg:pb-16';
const BODY = 'flex min-h-0 flex-1 flex-col lg:grid lg:grid-cols-[14.5rem_minmax(0,1fr)] lg:items-start';
const FOOTER = 'flex justify-end px-5 py-3 sm:px-8';

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
  const householdData = useHouseholdData();
  const shell: Shell = navigation?.household ? 'household' : navigation ? 'householdLess' : 'centered';
  const household = shell === 'household';
  const readyHousehold = householdData?.data.kind === 'ready' ? householdData.data.household : null;

  return (
    <div className={FRAME[shell]}>
      <SkipLink target={mainRef} />
      <header className={HEADER}>
        <div className="flex min-w-0 flex-1 items-center gap-2 sm:gap-4">
          <BrandMark />
          {readyHousehold && navigation ? (
            <>
              <span aria-hidden="true" className="hidden h-5 w-px shrink-0 bg-line sm:block" />
              <HouseholdMenu name={readyHousehold.name} canManage={readyHousehold.viewerRole === 'admin'} />
            </>
          ) : null}
        </div>
        {actions}
      </header>
      <div className={household ? BODY : 'contents'}>
        <main id="main" ref={mainRef} tabIndex={-1} className={`${MAIN} ${household ? MAIN_HOUSEHOLD : ''}`}>
          {children}
        </main>
        {navigation ? <HouseholdNavigation rail={household} /> : null}
      </div>
      {/* Signed-in navigation carries Account, where the language choice lives. The footer switch is for signed-out paths. */}
      {navigation ? null : (
        <footer className={FOOTER}>
          <LanguageSwitch />
        </footer>
      )}
    </div>
  );
}
