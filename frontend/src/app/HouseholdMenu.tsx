import { useCallback, useContext, useEffect, useId, useRef, useState } from 'react';
import { useI18n, type MessageKey } from '../i18n';
import { HouseholdNavigationContext } from './HouseholdNavigation';
import { HouseholdSwitchContext } from './HouseholdSwitchContext';
import type { HouseholdSummary } from './householdsApi';
import { NavLink } from './NavLink';
import { useDismiss } from './useDismiss';
import type { HouseholdList } from './useHouseholdList';
import type { SwitchFailure } from './useHouseholdSwitch';

const FOCUS = 'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus';
/** Closed trigger: unchanged, so the household name sits in the bar exactly as it always has. */
const TRIGGER = 'flex min-h-11 min-w-[6.5rem] max-w-[min(14rem,100%)] items-center gap-1.5 rounded-lg px-2 text-small font-medium text-ink transition-colors hover:bg-sand focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus';
// Phones pin the panel to the header's own width (16 px insets); from sm it hangs under its trigger. Either way it
// can never reach past the viewport, even at 320 px with a long household name. Long lists scroll inside the panel
// instead of growing past the viewport, so the last household stays reachable.
const PANEL = 'absolute left-0 top-[calc(100%+0.375rem)] z-30 max-h-[min(26rem,calc(100dvh-6rem))] w-[min(19rem,calc(100vw-4rem))] overflow-y-auto overscroll-contain max-sm:inset-x-4 max-sm:w-auto rounded-xl border border-line bg-raised p-1.5 shadow-[0_20px_50px_-32px_rgb(20_18_14/0.5)]';
const ROW = `flex min-h-11 w-full items-start gap-2 rounded-lg px-2.5 py-2 text-left text-small text-ink transition-colors ${FOCUS}`;
const SHOW_MORE = `mt-1 flex min-h-11 w-full items-center rounded-lg border-t border-line px-2.5 text-meta font-medium text-muted transition-colors hover:bg-sand hover:text-ink aria-disabled:cursor-not-allowed aria-disabled:opacity-60 ${FOCUS}`;
const FAULT_ROW = `flex min-h-11 w-full items-center rounded-lg px-2.5 text-left text-meta font-medium text-ink transition-colors hover:bg-sand ${FOCUS}`;
const ADMIN_ROW = `mt-1 flex min-h-11 items-center rounded-lg border-t border-line px-2.5 text-small font-medium text-ink transition-colors hover:bg-sand ${FOCUS}`;
const HELP = 'mt-1 border-t border-line px-2.5 pb-1.5 pt-2.5 text-meta text-muted';
const QUIET = 'px-2.5 py-1.5 text-meta text-muted';
const FAILURE: Record<SwitchFailure, MessageKey> = {
  gone: 'household.menu.gone',
  unavailable: 'household.menu.unavailable',
};

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

/** The household this account works in now: the panel's current row, never a button of its own. */
function ActiveRow({ name }: { name: string }) {
  const { t } = useI18n();
  return <p aria-current="true" className="flex items-start gap-2 rounded-lg px-2.5 py-2">
    <Check />
    <span className="min-w-0">
      <span className="block break-words text-small font-medium text-ink">{name}</span>
      <span className="block text-meta text-muted">{t('household.menu.active')}</span>
    </span>
  </p>;
}

/**
 * Another household: the whole name is the accessible name, and it wraps instead of clipping. A row on its way to
 * another household is marked `aria-disabled` instead of `disabled`, so the reader's focus stays on the row (and
 * with it the open panel and its explanation) while the request is out.
 */
function HouseholdOption({ household, disabled, onChoose }: {
  household: HouseholdSummary; disabled: boolean; onChoose: (household: HouseholdSummary) => void;
}) {
  return <button
    type="button"
    data-household-id={household.id}
    aria-disabled={disabled}
    onClick={() => onChoose(household)}
    className={`${ROW} font-medium hover:bg-sand aria-disabled:cursor-not-allowed aria-disabled:opacity-60`}
  >
    <span aria-hidden="true" className="mt-0.5 size-4 shrink-0" />
    <span className="min-w-0 break-words">{household.name}</span>
  </button>;
}

/** A quiet, actionable line: what could not be read, and one full-size row to read it again. */
function FailureLine({ message, retryLabel, onRetry }: { message: string; retryLabel: string; onRetry: () => void }) {
  return <div className="pt-1">
    <p className="px-2.5 text-meta text-danger">{message}</p>
    <button type="button" onClick={onRetry} className={FAULT_ROW}>{retryLabel}</button>
  </div>;
}

/**
 * The households beyond the active one. A single membership shows nothing here, so the panel stays exactly the
 * selector it has always been; the next page appears only when the reader asks for it.
 */
function HouseholdChoices({ list, activeId, busy, onChoose }: {
  list: HouseholdList; activeId: string; busy: boolean; onChoose: (household: HouseholdSummary) => void;
}) {
  const { t } = useI18n();
  if (list.state === 'error') {
    return <FailureLine message={t('household.menu.loadFailed')} retryLabel={t('household.menu.retry')} onRetry={list.reload} />;
  }
  if (list.state !== 'ready') return <p className={QUIET}>{t('household.menu.loading')}</p>;
  const others = list.items.filter((household) => household.id !== activeId);
  return <>
    {others.length ? <ul className="mt-0.5">
      {others.map((household) => <li key={household.id}>
        <HouseholdOption household={household} disabled={busy} onChoose={onChoose} />
      </li>)}
    </ul> : null}
    {list.moreFailed ? <FailureLine message={t('household.menu.loadFailed')} retryLabel={t('household.menu.retry')} onRetry={list.retryMore} /> : null}
    {list.nextCursor ? <button
      type="button"
      aria-disabled={list.loadingMore}
      onClick={() => { if (!list.loadingMore) void list.more(); }}
      className={SHOW_MORE}
    >
      {t('household.menu.more')}
    </button> : null}
  </>;
}

/**
 * The active household, and every other household this account belongs to. Choosing one asks the server to record
 * it on the session and then adopts the returned session, so the screens and the address below the bar follow the
 * chosen household. The panel promises nothing optimistic: a refusal keeps it open and changes nothing.
 *
 * The list and the request itself belong to HouseholdSwitchProvider: this menu may be unmounted by a screen change
 * while a switch is out, and the answer is still adopted.
 *
 * The trigger keeps a readable floor: at 320 px Czech the name may truncate, but the accessible name always
 * carries the whole thing.
 */
export function HouseholdMenu({ householdId, name, canManage }: { householdId: string; name: string; canManage: boolean }) {
  const { t } = useI18n();
  const navigation = useContext(HouseholdNavigationContext);
  const switching = useContext(HouseholdSwitchContext);
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const panelId = useId();
  const label = t('household.menu.label', { name });
  const list = switching?.list;
  const added = list?.added;
  const close = useCallback(() => setOpen(false), []);
  useDismiss(open, close, rootRef, buttonRef);
  // The last page takes the row that asked for it with it, so the reader is handed to the first household it added
  // rather than dropped back at the top of the document. A panel the reader closed meanwhile keeps its own focus.
  useEffect(() => {
    if (!added || panelRef.current?.hidden) return;
    const rows = panelRef.current ? Array.from(panelRef.current.querySelectorAll<HTMLElement>('[data-household-id]')) : [];
    const first = rows.find((row) => added.ids.includes(row.dataset.householdId ?? ''));
    (first ?? buttonRef.current)?.focus();
  }, [added]);
  // A household the server refuses as gone is read away from the list, and that read takes the very row the reader
  // pressed with it. Handing focus back to the trigger keeps the panel — and its explanation — open, instead of
  // dropping a keyboard reader at the top of the document. A panel the reader closed meanwhile keeps its own focus.
  useEffect(() => {
    if (switching?.failure !== 'gone' || panelRef.current?.hidden) return;
    buttonRef.current?.focus();
  }, [switching?.failure]);
  const toggle = () => {
    if (!open) { switching?.dismiss(); switching?.openList(); }
    setOpen(!open);
  };
  const choose = async (household: HouseholdSummary) => {
    if (await switching?.choose(household)) close();
  };

  return (
    <div ref={rootRef} className="relative min-w-0 max-sm:static">
      <button
        ref={buttonRef}
        type="button"
        aria-label={label}
        aria-expanded={open}
        aria-controls={panelId}
        onClick={toggle}
        className={TRIGGER}
      >
        <span className="min-w-0 truncate">{name}</span>
        <Chevron open={open} />
      </button>
      <div ref={panelRef} id={panelId} role="group" aria-label={label} hidden={!open} className={PANEL}>
        <ActiveRow name={name} />
        {switching?.failure ? <p role="alert" className="px-2.5 py-1.5 text-meta text-danger">{t(FAILURE[switching.failure])}</p> : null}
        {list ? <HouseholdChoices
          list={list}
          activeId={householdId}
          busy={switching?.busy ?? false}
          onChoose={(household) => void choose(household)}
        /> : null}
        {canManage && navigation ? (
          <NavLink
            href="/admin"
            onNavigate={() => { setOpen(false); navigation.actions.admin(); }}
            className={ADMIN_ROW}
          >
            {t('nav.admin')}
          </NavLink>
        ) : (
          <p className={HELP}>{t('household.menu.help')}</p>
        )}
      </div>
    </div>
  );
}
