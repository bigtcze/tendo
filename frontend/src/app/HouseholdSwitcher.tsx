import { useContext, useId, useRef, useState, type FocusEvent, type KeyboardEvent } from 'react';
import { useI18n } from '../i18n';
import { useHouseholdData, useViewerRole } from './HouseholdData';
import { HouseholdNavigationContext } from './HouseholdNavigation';
import { NavLink } from './NavLink';

/**
 * Active household as a disclosure button. One household per account today, so the panel names it and offers
 * Manage households (the Admin center) only to a viewer whose role hint is admin. Every admin request is still
 * authorized by the server; this control never grants anything.
 */
export function HouseholdSwitcher() {
  const { t } = useI18n();
  const household = useHouseholdData();
  const role = useViewerRole();
  const navigation = useContext(HouseholdNavigationContext);
  const [open, setOpen] = useState(false);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const panelId = useId();
  if (!navigation?.household || household?.data.kind !== 'ready') return null;
  const name = household.data.household.name;

  function onKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (event.key !== 'Escape' || !open) return;
    setOpen(false);
    triggerRef.current?.focus();
  }

  // Focus leaving the switcher (Tab past the panel, a click elsewhere) closes it without a trap.
  function onBlur(event: FocusEvent<HTMLDivElement>) {
    if (!(event.relatedTarget instanceof Node) || !event.currentTarget.contains(event.relatedTarget)) setOpen(false);
  }

  return (
    <div className="relative min-w-0 flex-1 sm:flex-none" onKeyDown={onKeyDown} onBlur={onBlur}>
      <button
        ref={triggerRef}
        type="button"
        aria-expanded={open}
        aria-controls={panelId}
        onClick={() => setOpen((value) => !value)}
        className="inline-flex min-h-11 max-w-full min-w-0 items-center gap-1.5 rounded-lg px-2.5 text-small font-semibold text-ink transition-colors hover:bg-sand focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus sm:max-w-[16rem]"
        aria-label={`${t('household.switch.label')}: ${name}`}
      >
        <span aria-hidden="true" className="min-w-0 truncate">{name}</span>
        <svg aria-hidden="true" focusable="false" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"
          strokeLinecap="round" strokeLinejoin="round" className={`size-4 shrink-0 text-muted transition-transform ${open ? 'rotate-180' : ''}`}>
          <path d="m6 9 6 6 6-6" />
        </svg>
      </button>
      {open ? (
        <div id={panelId} className="absolute left-0 top-full z-30 mt-1 w-64 max-w-[calc(100vw-2rem)] rounded-xl border border-line bg-raised p-2 shadow-[0_16px_40px_-20px_rgb(7_20_30/0.45)]">
          <p className="px-2.5 pb-1 pt-1.5 text-caption font-semibold uppercase tracking-[0.08em] text-muted">{t('household.switch.label')}</p>
          <p aria-current="true" className="px-2.5 py-2 text-small font-semibold text-ink [overflow-wrap:anywhere]">{name}</p>
          {role === 'admin' && navigation ? (
            <NavLink
              href="/admin"
              onNavigate={() => { setOpen(false); navigation.actions.admin(); }}
              className="mt-1 flex min-h-11 items-center rounded-lg border-t border-line px-2.5 pt-1 text-small font-semibold text-accent hover:bg-sand focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
            >
              {t('household.switch.manage')}
            </NavLink>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}
