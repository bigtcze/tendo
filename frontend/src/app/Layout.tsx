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
 * know what covers the viewport's last stretch, so every descendant reserves it as scroll margin: the fixed bottom
 * bar plus the notice it carries. Short viewports make the bar static (it must never cover content at 200–400 % zoom)
 * but the notice keeps its 5.5rem lift there so it still clears the bar at maximum scroll, so they reserve the same
 * band as the base rule. `lg` replaces the bar with the sidebar and keeps only the notice — its measured height
 * (`--notice-clearance`) plus its own offset. The notice floats at every width, so this clearance is never dropped:
 * a zero margin would put a focused control under the card (WCAG 2.4.11).
 */
const KEEP_FOCUS_ABOVE_BAR = '[&_*]:scroll-mb-[calc(var(--notice-clearance,0px)+6.5rem+env(safe-area-inset-bottom))]'
  + ' lg:[&_*]:scroll-mb-[calc(var(--notice-clearance,0px)+1.5rem)]';

const FRAME: Record<Shell, string> = {
  household: `${KEEP_FOCUS_ABOVE_BAR} flex min-h-dvh flex-col`,
  householdLess: 'flex min-h-dvh flex-col',
  centered: 'flex min-h-dvh flex-col',
};
/**
 * The bar spans the whole viewport — never a centered frame — at every width and in every shell variant. It has a
 * minimum height but never a fixed one: a long login or a sign-out failure must grow it instead of painting over
 * main. Phones get 16 px side insets, desktops 24–32 px, so the bar's controls line up with the content edge.
 */
const HEADER = 'relative flex min-h-14 flex-wrap items-center justify-between gap-x-3 gap-y-1 border-b border-line bg-raised px-4 py-1.5 sm:px-6 lg:min-h-16 lg:px-8 xl:px-10';
const HEADER_GROUP = 'flex min-w-0 flex-1 items-center gap-2 sm:gap-4';
const BODY = 'flex min-h-0 flex-1 flex-col lg:grid lg:grid-cols-[14.5rem_minmax(0,1fr)] lg:items-start';
const MAIN = 'w-full min-w-0 flex-1 px-4 py-6 sm:px-6 sm:py-8 lg:px-8 lg:pt-8 xl:px-10';
/**
 * Bottom padding keeps the last content clear of the fixed phone bar and of the floating notice; the sidebar needs
 * only the notice's measured height, so `lg` reserves that plus the shared 3rem. Short viewports drop the bar (it
 * turns static) but not the notice, so they reserve the notice's height and its lower offset. MAIN's `sm:py-8` is
 * declared after the base utilities, so the clearance must be repeated at `sm` too — otherwise the final row of a
 * long list can sit behind the bar at max scroll.
 */
const MAIN_HOUSEHOLD = 'pb-[calc(var(--notice-clearance,0px)+6.5rem+env(safe-area-inset-bottom))]'
  + ' sm:pb-[calc(var(--notice-clearance,0px)+6.5rem+env(safe-area-inset-bottom))]'
  + ' short:pb-[calc(var(--notice-clearance,0px)+1.5rem+env(safe-area-inset-bottom))]'
  + ' lg:col-start-2 lg:row-start-1 lg:pb-[calc(var(--notice-clearance,0px)+3rem)] lg:min-w-0';
const FOOTER = 'flex justify-end px-4 py-3 sm:px-6 lg:px-8 xl:px-10';

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
        <div className={HEADER_GROUP}>
          <BrandMark />
          {readyHousehold && navigation ? (
            <>
              <span aria-hidden="true" className="hidden h-5 w-px shrink-0 bg-line sm:block" />
              <HouseholdMenu householdId={readyHousehold.id} name={readyHousehold.name} canManage={readyHousehold.viewerRole === 'admin'} />
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
