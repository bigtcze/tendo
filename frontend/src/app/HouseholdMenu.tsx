import { useContext, useId, useRef, useState } from 'react';
import { useI18n } from '../i18n';
import { HouseholdNavigationContext } from './HouseholdNavigation';
import { NavLink } from './NavLink';
import { useDismiss } from './useDismiss';

function Chevron({ open }: { open: boolean }) {
  return <svg aria-hidden="true" focusable="false" viewBox="0 0 24 24" fill="none" stroke="currentColor"
    strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round"
    className={`size-4 shrink-0 transition-transform motion-reduce:transition-none ${open ? 'rotate-180' : ''}`}>
    <path d="m6 9 6 6 6-6" />
  </svg>;
}

function Check() {
  return <svg aria-hidden="true" focusable="false" viewBox="0 0 24 24" fill="none" stroke="currentColor"
    strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="mt-0.5 size-4 shrink-0 text-accent">
    <path d="m5 12.5 4.5 4.5L19 7" />
  </svg>;
}

/**
 * The one household control the API supports today: it names the active household and, for an administrator,
 * offers the Admin center where accounts and access are managed. It deliberately promises no switching that no
 * endpoint implements yet, and its open panel is the place a future authorized-household list would live.
 *
 * The trigger keeps a readable floor: at 320 px Czech the name may truncate, but the accessible name always
 * carries the whole thing.
 */
export function HouseholdMenu({ name, canManage }: { name: string; canManage: boolean }) {
  const { t } = useI18n();
  const navigation = useContext(HouseholdNavigationContext);
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const panelId = useId();
  useDismiss(open, () => setOpen(false), rootRef, buttonRef);
  // "Household: Veselí" stays the whole name even when the visible text has to truncate.
  const label = t('household.menu.label', { name });

  return (
    <div ref={rootRef} className="relative min-w-0">
      <button
        ref={buttonRef}
        type="button"
        aria-label={label}
        aria-expanded={open}
        aria-controls={panelId}
        onClick={() => setOpen((value) => !value)}
        className="flex min-h-11 min-w-[6.5rem] max-w-[min(14rem,100%)] items-center gap-1.5 rounded-lg px-2 text-small font-medium text-ink transition-colors hover:bg-sand focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
      >
        <span className="min-w-0 truncate">{name}</span>
        <Chevron open={open} />
      </button>
      <div
        id={panelId}
        role="group"
        aria-label={label}
        hidden={!open}
        className="absolute left-0 top-[calc(100%+0.375rem)] z-30 w-[min(19rem,calc(100vw-4rem))] rounded-xl border border-line bg-raised p-1.5 shadow-[0_20px_50px_-32px_rgb(20_18_14/0.5)]"
      >
        <p aria-current="true" className="flex items-start gap-2 rounded-lg px-2.5 py-2">
          <Check />
          <span className="min-w-0">
            <span className="block break-words text-small font-medium text-ink">{name}</span>
            <span className="block text-meta text-muted">{t('household.menu.active')}</span>
          </span>
        </p>
        {canManage && navigation ? (
          <NavLink
            href="/admin"
            onNavigate={() => { setOpen(false); navigation.actions.admin(); }}
            className="mt-1 flex min-h-11 items-center rounded-lg border-t border-line px-2.5 text-small font-medium text-ink transition-colors hover:bg-sand focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
          >
            {t('household.menu.members')}
          </NavLink>
        ) : (
          <p className="mt-1 border-t border-line px-2.5 pb-1.5 pt-2.5 text-meta text-muted">{t('household.menu.help')}</p>
        )}
      </div>
    </div>
  );
}
