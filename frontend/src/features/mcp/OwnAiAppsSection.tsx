import { useEffect, useId, useRef, useState, type FormEvent } from 'react';
import { Button } from '../../components/ui/button';
import { LoadError } from '../../components/ui/load-error';
import { LoadingLine } from '../../components/ui/loading-line';
import { Notice } from '../../components/ui/notice';
import { useI18n, type MessageKey, type Translate } from '../../i18n';
import type { AiAppsAccess, OwnAiApps } from '../installation/aiAppsApi';
import { AccessChoice, OutcomeHosts, SettingSwitch } from '../installation/AiAppsFields';
import { accessWord } from '../installation/useAccountAiApps';
import { isOwnAiAppsRelevant, useOwnAiApps, type OwnAiAppsState } from './useOwnAiApps';

const HEADING_ID = 'account-ai-apps-heading';
const NOW: Record<ReturnType<typeof accessWord>, MessageKey> = {
  none: 'ownAiApps.now.none', read: 'accountAiApps.now.read', write: 'accountAiApps.now.write', both: 'accountAiApps.now.both',
};

/**
 * The person's own AI apps on Account, before their connected apps: what their apps can do now, a switch that pauses
 * them all and what they may do, which can only be less than the administrator allows. An account that AI apps are
 * not allowed for and that never narrowed its own choice sees nothing, so instances that never use MCP see no change;
 * once shown, the section stays for the visit, so a save never takes it away from under the reader.
 */
export function OwnAiAppsSection({ onSignedOut }: { onSignedOut: () => void }) {
  const { t } = useI18n();
  const own = useOwnAiApps({ onSignedOut });
  const [shown, setShown] = useState(false);
  const relevant = own.state === 'error' || own.state === 'refused'
    || (own.state === 'ready' && own.settings !== null && isOwnAiAppsRelevant(own.settings));
  // Adjusted while rendering, not in an effect, so the section never flickers out for one frame.
  if (relevant && !shown) setShown(true);
  if (!shown && !relevant) return null;

  return <section aria-labelledby={HEADING_ID} className="mt-6 max-w-3xl border-t border-line pt-6 sm:mt-8">
    <h2 id={HEADING_ID} className="text-h2 text-ink">{t('ownAiApps.title')}</h2>
    <p className="mt-1 max-w-prose text-small text-muted">{t('ownAiApps.body')}</p>
    {/* Only after a retry: the first read shows nothing until it knows the section applies. */}
    {own.state === 'loading' ? <LoadingLine className="mt-4">{t('ownAiApps.loading')}</LoadingLine> : null}
    {own.state === 'error'
      ? <LoadError className="mt-4" message={t('ownAiApps.error.load')} retryLabel={t('app.error.retry')} onRetry={own.retry} />
      : null}
    {own.state === 'refused' ? <Notice tone="error" className="mt-4">{t('ownAiApps.error.loadRejected')}</Notice> : null}
    {own.state === 'ready' && own.settings ? <>
      <OwnNow settings={own.settings} t={t} />
      <OwnForm own={own} maximum={own.settings.maximumScopes} t={t} />
    </> : null}
  </section>;
}

/** What apps may do for the person now, and, when nothing is allowed for the account, that their choice waits. */
function OwnNow({ settings, t }: { settings: OwnAiApps; t: Translate }) {
  return <div className="mt-4 space-y-3">
    <div>
      <h3 className="text-body font-semibold text-ink">{t('ownAiApps.now.title')}</h3>
      <p data-own-ai-apps-now className="mt-1 max-w-prose text-body text-ink">{t(NOW[accessWord(settings.effectiveScopes)])}</p>
    </div>
    {settings.maximumScopes.length === 0 ? <Notice role={null}>{t('ownAiApps.notAllowed')}</Notice> : null}
  </div>;
}

function OwnForm({ own, maximum, t }: { own: OwnAiAppsState; maximum: readonly AiAppsAccess[]; t: Translate }) {
  const uid = useId();
  const firstAccessRef = useRef<HTMLInputElement>(null);
  const [refusals, setRefusals] = useState(0);
  // Every refused access choice puts the reader back on it, once the controls the save disabled are usable again.
  useEffect(() => {
    if (refusals > 0) firstAccessRef.current?.focus();
  }, [refusals]);
  const { draft, saving, outcome } = own;
  if (!draft) return null;
  // Only while something is allowed: with nothing allowed the notice above already says why nothing applies.
  const notes: Partial<Record<AiAppsAccess, string>> = maximum.length === 0 ? {} : Object.fromEntries(
    (['tendo:read', 'tendo:write'] as const).filter((scope) => !maximum.includes(scope))
      .map((scope) => [scope, t('ownAiApps.access.notAllowed')]),
  );

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (await own.save() === 'invalid') setRefusals((count) => count + 1);
  }

  return <form aria-labelledby={HEADING_ID} noValidate onSubmit={(event) => void submit(event)} className="mt-6 grid max-w-2xl gap-6">
    <SettingSwitch id={`${uid}-own`} label={t('ownAiApps.enabled')} hint={t('ownAiApps.enabledHint')}
      checked={draft.enabled} disabled={saving} onChange={own.setEnabled} />
    <AccessChoice uid={uid} legend={t('ownAiApps.access.title')} hint={t('ownAiApps.access.hint')}
      chosen={draft.scopes} missing={own.accessMissing} disabled={saving} firstRef={firstAccessRef}
      onToggle={own.toggleAccess} notes={notes} t={t} />
    <OutcomeHosts outcome={outcome} t={t} />
    <Button type="submit" disabled={saving} className="w-full sm:w-auto sm:justify-self-start">
      {saving ? t('aiApps.saving') : t('aiApps.save')}
    </Button>
  </form>;
}
