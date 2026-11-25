import { useEffect, useId, useRef, useState, type FormEvent, type RefObject } from 'react';
import { AccountActions } from '../../app/AccountActions';
import { Heading } from '../../app/Heading';
import { Layout } from '../../app/Layout';
import { Button } from '../../components/ui/button';
import { LoadError } from '../../components/ui/load-error';
import { LoadingLine } from '../../components/ui/loading-line';
import { Notice } from '../../components/ui/notice';
import { Switch } from '../../components/ui/switch';
import { useI18n, type MessageKey, type Translate } from '../../i18n';
import { AdminSectionSwitcher } from '../admin/AdminSectionSwitcher';
import type { AiAppsAccess } from './aiAppsApi';
import { InstallationOnlyAdmins } from './InstallationStates';
import { useAiAppsSettings, type AiAppsSettingsState } from './useAiAppsSettings';

const ACCESS: { scope: AiAppsAccess; label: MessageKey; hint: MessageKey }[] = [
  { scope: 'tendo:read', label: 'aiApps.access.read', hint: 'aiApps.access.readHint' },
  { scope: 'tendo:write', label: 'aiApps.access.write', hint: 'aiApps.access.writeHint' },
];

/**
 * The installation's AI apps: whether apps may connect at all, whether accounts may use them unless an administrator
 * sets one account differently, and what they may do by default. Nothing here changes on screen before the server
 * agrees, and turning apps off pauses connected apps — it never disconnects them.
 */
export function InstallationAiAppsScreen({ login, onSignedOut, onForbidden }: {
  login: string;
  onSignedOut: () => void;
  onForbidden: () => void;
}) {
  const { t } = useI18n();
  const forbiddenHeadingRef = useRef<HTMLHeadingElement>(null);
  const refusedRef = useRef(false);
  const settings = useAiAppsSettings({
    onSignedOut,
    onForbidden: (duringSave) => { refusedRef.current = duringSave; onForbidden(); },
  });

  // A save refused for the grant takes the form, and the focus it held, with it: the refusal's heading takes over.
  useEffect(() => {
    if (settings.state !== 'forbidden' || !refusedRef.current) return;
    refusedRef.current = false;
    forbiddenHeadingRef.current?.focus();
  }, [settings.state]);

  return <Layout actions={<AccountActions login={login} onSignedOut={onSignedOut} />}>
    <section className="settle">
      <Heading className="text-h1 font-semibold text-ink">{t('install.page.title')}</Heading>
      <AdminSectionSwitcher current="aiApps" />
      {settings.state === 'forbidden'
        ? <InstallationOnlyAdmins t={t} headingRef={forbiddenHeadingRef} />
        : <AiAppsSection settings={settings} t={t} />}
    </section>
  </Layout>;
}

function AiAppsSection({ settings, t }: { settings: AiAppsSettingsState; t: Translate }) {
  return <section aria-labelledby="ai-apps-heading" className="mt-6 sm:mt-8">
    <h2 id="ai-apps-heading" className="text-h2 text-ink">{t('aiApps.title')}</h2>
    <p className="mt-1 max-w-prose text-body text-muted">{t('aiApps.body')}</p>
    {settings.state === 'loading' ? <LoadingLine className="mt-6">{t('aiApps.loading')}</LoadingLine> : null}
    {settings.state === 'error'
      ? <LoadError className="mt-6" message={t('aiApps.error.load')} retryLabel={t('app.error.retry')} onRetry={settings.retry} />
      : null}
    {settings.state === 'refused' ? <Notice tone="error" className="mt-6">{t('aiApps.error.loadRejected')}</Notice> : null}
    {settings.state === 'ready' ? <AiAppsForm settings={settings} t={t} /> : null}
  </section>;
}

function AiAppsForm({ settings, t }: { settings: AiAppsSettingsState; t: Translate }) {
  const uid = useId();
  const firstAccessRef = useRef<HTMLInputElement>(null);
  const [refusals, setRefusals] = useState(0);
  const { draft, saving, outcome } = settings;
  // Every refused access choice puts the reader back on it, once the controls the save disabled are usable again.
  useEffect(() => {
    if (refusals > 0) firstAccessRef.current?.focus();
  }, [refusals]);
  if (!draft || !settings.settings) return null;

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (await settings.save() === 'invalid') setRefusals((count) => count + 1);
  }

  return <form aria-labelledby="ai-apps-heading" noValidate onSubmit={(event) => void submit(event)} className="mt-6 grid max-w-2xl gap-6">
    {settings.settings.operatorEnabled ? null : <Notice role={null}>{t('aiApps.serverOff')}</Notice>}
    <SettingSwitch id={`${uid}-installation`} label={t('aiApps.enabled')} hint={t('aiApps.enabledHint')}
      checked={draft.enabled} disabled={saving} onChange={(enabled) => settings.update({ enabled })} />
    <SettingSwitch id={`${uid}-accounts`} label={t('aiApps.accounts')} hint={t('aiApps.accountsHint')}
      checked={draft.defaultAccountEnabled} disabled={saving}
      onChange={(defaultAccountEnabled) => settings.update({ defaultAccountEnabled })} />
    <AccessChoice uid={uid} chosen={draft.defaultMaxScopes} missing={settings.accessMissing} disabled={saving}
      firstRef={firstAccessRef} onToggle={settings.toggleAccess} t={t} />
    {/* Both outcome hosts stay mounted, so a message is never inserted into a live region that did not exist yet. */}
    <div role="alert" className="empty:contents">
      {outcome?.tone === 'error' ? <Notice tone="error" role={null}>{t(outcome.key)}</Notice> : null}
    </div>
    <div role="status" aria-live="polite" aria-atomic="true" className="empty:contents">
      {outcome && outcome.tone !== 'error'
        ? <Notice tone={outcome.tone} role={null}>{t(outcome.key)}</Notice>
        : null}
    </div>
    <Button type="submit" disabled={saving} className="w-full sm:w-auto sm:justify-self-start">
      {saving ? t('aiApps.saving') : t('aiApps.save')}
    </Button>
  </form>;
}

/** A setting row: the switch, its name and one sentence on what it does; the switch and its name are both targets. */
function SettingSwitch({ id, label, hint, checked, disabled, onChange }: {
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

function AccessChoice({ uid, chosen, missing, disabled, firstRef, onToggle, t }: {
  uid: string;
  chosen: AiAppsAccess[];
  missing: boolean;
  disabled: boolean;
  firstRef: RefObject<HTMLInputElement | null>;
  onToggle: (scope: AiAppsAccess, on: boolean) => void;
  t: Translate;
}) {
  const hintId = `${uid}-access-hint`;
  const errorId = `${uid}-access-error`;
  return <fieldset className="min-w-0 border-t border-line pt-6" aria-describedby={missing ? `${hintId} ${errorId}` : hintId}>
    <legend className="float-left w-full text-body font-semibold text-ink">{t('aiApps.access.title')}</legend>
    <p id={hintId} className="clear-left max-w-prose pt-1 text-small text-muted">{t('aiApps.access.hint')}</p>
    <ul className="mt-2 min-w-0">
      {ACCESS.map((option, index) => <li key={option.scope} className="min-w-0">
        <label className="flex min-h-11 min-w-0 cursor-pointer items-start gap-3 py-2">
          <input type="checkbox" ref={index === 0 ? firstRef : undefined} disabled={disabled}
            checked={chosen.includes(option.scope)} aria-invalid={missing || undefined}
            aria-describedby={missing ? errorId : undefined}
            onChange={(event) => onToggle(option.scope, event.currentTarget.checked)}
            className="mt-0.5 size-5 shrink-0 accent-accent focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus" />
          <span className="min-w-0 break-words">
            <span className="block text-body text-ink">{t(option.label)}</span>
            <span className="block text-small text-muted">{t(option.hint)}</span>
          </span>
        </label>
      </li>)}
    </ul>
    {missing ? <p id={errorId} role="alert" className="mt-1 text-meta text-danger">{t('aiApps.access.missing')}</p> : null}
  </fieldset>;
}
