import { useId, type RefObject } from 'react';
import { Notice } from '../../components/ui/notice';
import { Switch } from '../../components/ui/switch';
import type { MessageKey, Translate } from '../../i18n';
import type { AiAppsAccess } from './aiAppsApi';
import type { VersionedOutcome } from './useVersionedForm';

const ACCESS: { scope: AiAppsAccess; label: MessageKey; hint: MessageKey }[] = [
  { scope: 'tendo:read', label: 'aiApps.access.read', hint: 'aiApps.access.readHint' },
  { scope: 'tendo:write', label: 'aiApps.access.write', hint: 'aiApps.access.writeHint' },
];

const CONTROL = 'mt-0.5 size-5 shrink-0 accent-accent focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus';

/**
 * Read and change as two checkboxes: change does not include reading, so neither implies the other. The legend and
 * the hint say whose maximum this is. `notes` adds one line under an option, for example that someone else does not
 * allow it; the option stays a choice.
 */
export function AccessChoice({ uid, legend, hint, chosen, missing, disabled, firstRef, onToggle, notes = {}, t }: {
  uid: string;
  legend: string;
  hint: string;
  chosen: AiAppsAccess[];
  missing: boolean;
  disabled: boolean;
  firstRef: RefObject<HTMLInputElement | null>;
  onToggle: (scope: AiAppsAccess, on: boolean) => void;
  notes?: Partial<Record<AiAppsAccess, string>>;
  t: Translate;
}) {
  const hintId = `${uid}-access-hint`;
  const errorId = `${uid}-access-error`;
  return <fieldset className="min-w-0 border-t border-line pt-6" aria-describedby={missing ? `${hintId} ${errorId}` : hintId}>
    <legend className="float-left w-full text-body font-semibold text-ink">{legend}</legend>
    <p id={hintId} className="clear-left max-w-prose pt-1 text-small text-muted">{hint}</p>
    <ul className="mt-2 min-w-0">
      {ACCESS.map((option, index) => <li key={option.scope} className="min-w-0">
        <label className="flex min-h-11 min-w-0 cursor-pointer items-start gap-3 py-2">
          <input type="checkbox" ref={index === 0 ? firstRef : undefined} disabled={disabled}
            checked={chosen.includes(option.scope)} aria-invalid={missing || undefined}
            aria-describedby={missing ? errorId : undefined}
            onChange={(event) => onToggle(option.scope, event.currentTarget.checked)}
            className={CONTROL} />
          <span className="min-w-0 break-words">
            <span className="block text-body text-ink">{t(option.label)}</span>
            <span className="block text-small text-muted">{t(option.hint)}</span>
            {notes[option.scope] ? <span className="block text-small text-ink">{notes[option.scope]}</span> : null}
          </span>
        </label>
      </li>)}
    </ul>
    {missing ? <p id={errorId} role="alert" className="mt-1 text-meta text-danger">{t('aiApps.access.missing')}</p> : null}
  </fieldset>;
}

/**
 * One radio option row in the same shape as a checkbox row: the whole row is the target. The label alone names the
 * option and the line on what it means describes it, so a screen reader says the name first and the explanation after.
 */
export function ChoiceOption({ name, checked, disabled, label, hint, onChoose }: {
  name: string;
  checked: boolean;
  disabled: boolean;
  label: string;
  hint?: string;
  onChoose: () => void;
}) {
  const id = useId();
  return <li className="min-w-0">
    <label className="flex min-h-11 min-w-0 cursor-pointer items-start gap-3 py-2">
      <input type="radio" name={name} checked={checked} disabled={disabled} onChange={onChoose}
        aria-labelledby={`${id}-name`} aria-describedby={hint ? `${id}-hint` : undefined} className={CONTROL} />
      <span className="min-w-0 break-words">
        <span id={`${id}-name`} className="block text-body text-ink">{label}</span>
        {hint ? <span id={`${id}-hint`} className="block text-small text-muted">{hint}</span> : null}
      </span>
    </label>
  </li>;
}

/** Both outcome hosts stay mounted, so a message is never inserted into a live region that did not exist yet. */
export function OutcomeHosts({ outcome, t }: { outcome: VersionedOutcome; t: Translate }) {
  return <>
    <div role="alert" className="empty:contents">
      {outcome?.tone === 'error' ? <Notice tone="error" role={null}>{t(outcome.key)}</Notice> : null}
    </div>
    <div role="status" aria-live="polite" aria-atomic="true" className="empty:contents">
      {outcome && outcome.tone !== 'error'
        ? <Notice tone={outcome.tone} role={null}>{t(outcome.key)}</Notice>
        : null}
    </div>
  </>;
}

/** A setting row: the switch, its name and one sentence on what it does; the switch and its name are both targets. */
export function SettingSwitch({ id, label, hint, checked, disabled, onChange }: {
  id: string;
  label: string;
  hint: string;
  checked: boolean;
  disabled: boolean;
  onChange: (checked: boolean) => void;
}) {
  return <div className="grid grid-cols-[auto_minmax(0,1fr)] items-start gap-x-3 border-t border-line pt-6">
    <Switch id={id} checked={checked} disabled={disabled} onCheckedChange={onChange} aria-describedby={`${id}-hint`} />
    <div className="min-w-0 pt-2.5">
      <label htmlFor={id} className="cursor-pointer break-words text-body font-semibold text-ink">{label}</label>
      <p id={`${id}-hint`} className="mt-1 max-w-prose text-small text-muted">{hint}</p>
    </div>
  </div>;
}
