import { useEffect, useId, useRef, useState, type FormEvent, type RefObject } from 'react';
import { AccountActions } from '../../app/AccountActions';
import { Heading } from '../../app/Heading';
import { Layout } from '../../app/Layout';
import { NavLink } from '../../app/NavLink';
import { ADMIN_ACCOUNTS_PATH } from '../../app/routes';
import { Button } from '../../components/ui/button';
import { LoadError } from '../../components/ui/load-error';
import { LoadingLine } from '../../components/ui/loading-line';
import { Notice } from '../../components/ui/notice';
import { useI18n, type MessageKey, type Translate } from '../../i18n';
import { AdminSectionSwitcher } from '../admin/AdminSectionSwitcher';
import type { AccountAiApps } from './aiAppsApi';
import { ChoiceOption, OutcomeHosts } from './AiAppsFields';
import { AccountConnectedApps } from './AccountConnectedApps';
import { BACK_LINK, InstallationOnlyAdmins } from './InstallationStates';
import {
  accessWord, useAccountAiApps, type AccountAiAppsState, type AccountMaximum, type AccountSwitch,
} from './useAccountAiApps';
import type { VersionedFormState } from './useVersionedForm';

const SWITCHES: { value: AccountSwitch; label: MessageKey; hint?: MessageKey }[] = [
  { value: 'default', label: 'accountAiApps.use.default', hint: 'accountAiApps.use.defaultHint' },
  { value: 'on', label: 'accountAiApps.use.on' },
  { value: 'off', label: 'accountAiApps.use.off', hint: 'accountAiApps.use.offHint' },
];
const MAXIMUMS: { value: AccountMaximum; label: MessageKey; hint?: MessageKey }[] = [
  { value: 'default', label: 'accountAiApps.max.default', hint: 'accountAiApps.max.defaultHint' },
  { value: 'read', label: 'aiApps.access.read', hint: 'aiApps.access.readHint' },
  { value: 'write', label: 'aiApps.access.write', hint: 'aiApps.access.writeHint' },
  { value: 'both', label: 'accountAiApps.max.both' },
];
const NOW: Record<ReturnType<typeof accessWord>, MessageKey> = {
  none: 'accountAiApps.now.none', read: 'accountAiApps.now.read', write: 'accountAiApps.now.write', both: 'accountAiApps.now.both',
};
const OWN: Record<ReturnType<typeof accessWord>, MessageKey> = {
  none: 'accountAiApps.own.off', read: 'accountAiApps.own.read', write: 'accountAiApps.own.write', both: 'accountAiApps.own.both',
};

/**
 * One account's AI apps, reached from its row in Accounts: what apps may do for it right now, the person's own
 * choice, which only they change, and the administrator's choice for this account, which either follows the
 * installation's settings or sets this account differently. Nothing changes on screen before the server agrees, and
 * turning apps off for an account pauses its apps — it never disconnects them.
 */
export function InstallationAccountAiAppsScreen({ userId, accountLogin, login, onSignedOut, onForbidden, onBack }: {
  userId: string;
  /** The login the directory row showed on the way in; a visit without one keeps the generic heading. */
  accountLogin: string | null;
  /** The viewer's own login, for the account actions. */
  login: string;
  onSignedOut: () => void;
  onForbidden: () => void;
  onBack: () => void;
}) {
  const { t } = useI18n();
  const refusalHeadingRef = useRef<HTMLHeadingElement>(null);
  // The shell re-reads the session once, whichever part of the page found the grant gone first.
  const toldShell = useRef(false);
  const forbidden = () => {
    if (toldShell.current) return;
    toldShell.current = true;
    onForbidden();
  };
  // Focus after a refusal of the settings comes from useRefusalFocus, which also covers the account being gone, so
  // the hook's during-save flag is not needed here.
  const account = useAccountAiApps({ userId, onSignedOut, onForbidden: forbidden });
  // The connected apps read on their own and can be the first to find the grant or the account gone. They say
  // whether that followed an action; it can, even while the settings above failed to load.
  const [lost, setLost] = useState<{ state: 'forbidden' | 'notFound'; duringAction: boolean } | null>(null);
  const state = lost?.state ?? account.state;
  useRefusalFocus(account.state, refusalHeadingRef);
  useEffect(() => {
    if (lost?.duringAction) refusalHeadingRef.current?.focus();
  }, [lost]);

  return <Layout actions={<AccountActions login={login} onSignedOut={onSignedOut} />}>
    <section className="settle">
      <Heading className="text-h1 font-semibold text-ink">
        <span className="break-words">{accountLogin ? t('accountAiApps.titleFor', { login: accountLogin }) : t('accountAiApps.title')}</span>
      </Heading>
      <AdminSectionSwitcher current="accounts" />
      <NavLink href={ADMIN_ACCOUNTS_PATH} onNavigate={onBack} {...BACK_LINK}>{t('accountAiApps.back')}</NavLink>
      {state === 'forbidden' ? <InstallationOnlyAdmins t={t} headingRef={refusalHeadingRef} /> : null}
      {state === 'notFound' ? <AccountGone t={t} headingRef={refusalHeadingRef} /> : null}
      {state === 'forbidden' || state === 'notFound' ? null : <>
        <AccountSection account={account} t={t} />
        <AccountConnectedApps userId={userId} onSignedOut={onSignedOut}
          onForbidden={(duringAction) => { setLost({ state: 'forbidden', duringAction }); forbidden(); }}
          onGone={(duringAction) => setLost({ state: 'notFound', duringAction })} />
      </>}
    </section>
  </Layout>;
}

/**
 * A refusal of the settings that took the page away — the grant gone, or the account deleted, during a save or a
 * re-read — puts the reader on the refusal's heading. A refusal on arrival leaves focus where the page put it.
 */
function useRefusalFocus(state: VersionedFormState, headingRef: RefObject<HTMLHeadingElement | null>) {
  const previous = useRef(state);
  useEffect(() => {
    const was = previous.current;
    previous.current = state;
    if (was === 'ready' && (state === 'forbidden' || state === 'notFound')) headingRef.current?.focus();
  }, [headingRef, state]);
}

/** The account the address names is gone; there is nothing to retry, and the way back stays above. */
function AccountGone({ t, headingRef }: { t: Translate; headingRef: RefObject<HTMLHeadingElement | null> }) {
  return <section aria-labelledby="account-ai-apps-gone" className="mt-6 sm:mt-8">
    <h2 id="account-ai-apps-gone" ref={headingRef} tabIndex={-1} className="text-h2 text-ink outline-none">
      {t('accountAiApps.gone.title')}
    </h2>
    <p className="mt-1 max-w-prose text-body text-muted">{t('accountAiApps.gone.body')}</p>
  </section>;
}

function AccountSection({ account, t }: { account: AccountAiAppsState; t: Translate }) {
  return <>
    {account.state === 'loading' ? <LoadingLine className="mt-6">{t('accountAiApps.loading')}</LoadingLine> : null}
    {account.state === 'error'
      ? <LoadError className="mt-6" message={t('accountAiApps.error.load')} retryLabel={t('app.error.retry')} onRetry={account.retry} />
      : null}
    {account.state === 'refused' ? <Notice tone="error" className="mt-6">{t('accountAiApps.error.loadRejected')}</Notice> : null}
    {account.state === 'ready' && account.policy ? <>
      <AccountFacts policy={account.policy} t={t} />
      <AccountForm account={account} t={t} />
    </> : null}
  </>;
}

/** What apps may do for the account now, and the person's own choice: facts the server reports, never edited here. */
function AccountFacts({ policy, t }: { policy: AccountAiApps; t: Translate }) {
  const own = policy.userEnabled ? accessWord(policy.userScopes) : 'none';
  return <div className="mt-6 grid max-w-2xl gap-6 sm:mt-8">
    <section aria-labelledby="account-ai-apps-now">
      <h2 id="account-ai-apps-now" className="text-h2 text-ink">{t('accountAiApps.now.title')}</h2>
      <p data-account-ai-apps-now className="mt-1 max-w-prose text-body text-ink">{t(NOW[accessWord(policy.effectiveScopes)])}</p>
      <p className="mt-1 max-w-prose text-small text-muted">{t('accountAiApps.now.hint')}</p>
    </section>
    <section aria-labelledby="account-ai-apps-own" className="border-t border-line pt-6">
      <h2 id="account-ai-apps-own" className="text-h2 text-ink">{t('accountAiApps.own.title')}</h2>
      <p data-account-ai-apps-own className="mt-1 max-w-prose text-body text-ink">{t(OWN[own])}</p>
      <p className="mt-1 max-w-prose text-small text-muted">{t('accountAiApps.own.hint')}</p>
    </section>
  </div>;
}

function AccountForm({ account, t }: { account: AccountAiAppsState; t: Translate }) {
  const uid = useId();
  const maximumRef = useRef<HTMLFieldSetElement>(null);
  // A refused maximum puts the reader on its chosen option, where the message is, once the form is enabled again.
  const [refusals, setRefusals] = useState(0);
  useEffect(() => {
    if (refusals > 0) maximumRef.current?.querySelector<HTMLInputElement>('input:checked')?.focus();
  }, [refusals]);
  const { draft, saving, outcome } = account;
  if (!draft) return null;

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (await account.save() === 'invalid') setRefusals((count) => count + 1);
  }

  return <form aria-labelledby="account-ai-apps-settings" noValidate onSubmit={(event) => void submit(event)}
    className="mt-6 grid max-w-2xl gap-6 border-t border-line pt-6">
    <div>
      <h2 id="account-ai-apps-settings" className="text-h2 text-ink">{t('accountAiApps.settings.title')}</h2>
      <p className="mt-1 max-w-prose text-body text-muted">{t('accountAiApps.settings.body')}</p>
    </div>
    <RadioChoice name={`${uid}-use`} legend={t('accountAiApps.use.title')} options={SWITCHES} chosen={draft.enabled}
      disabled={saving} onChoose={account.setSwitch} t={t} />
    <RadioChoice name={`${uid}-max`} legend={t('accountAiApps.max.title')} hint={t('accountAiApps.max.hint')}
      options={MAXIMUMS} chosen={draft.maximum} disabled={saving} onChoose={account.setMaximum} fieldsetRef={maximumRef}
      error={account.maximumRefused ? t('accountAiApps.max.refused') : null} t={t} />
    <OutcomeHosts outcome={outcome} t={t} />
    <Button type="submit" disabled={saving} className="w-full sm:w-auto sm:justify-self-start">
      {saving ? t('aiApps.saving') : t('aiApps.save')}
    </Button>
  </form>;
}

/** One radio choice: a legend, an optional line on what it decides, the options and a refusal said under them. */
function RadioChoice<V extends string>({ name, legend, hint, options, chosen, disabled, onChoose, error = null, fieldsetRef, t }: {
  name: string;
  legend: string;
  hint?: string;
  options: { value: V; label: MessageKey; hint?: MessageKey }[];
  chosen: V;
  disabled: boolean;
  onChoose: (value: V) => void;
  error?: string | null;
  fieldsetRef?: RefObject<HTMLFieldSetElement | null>;
  t: Translate;
}) {
  const hintId = `${name}-hint`;
  const errorId = `${name}-error`;
  const described = [hint ? hintId : null, error ? errorId : null].filter(Boolean).join(' ') || undefined;
  return <fieldset ref={fieldsetRef} className="min-w-0" aria-describedby={described} aria-invalid={error ? true : undefined}>
    <legend className="float-left w-full text-body font-semibold text-ink">{legend}</legend>
    {hint ? <p id={hintId} className="clear-left max-w-prose pt-1 text-small text-muted">{hint}</p> : null}
    <ul className="clear-left mt-2 min-w-0">
      {options.map((option) => <ChoiceOption key={option.value} name={name} checked={chosen === option.value}
        disabled={disabled} label={t(option.label)} hint={option.hint ? t(option.hint) : undefined}
        onChoose={() => onChoose(option.value)} />)}
    </ul>
    {error ? <p id={errorId} role="alert" className="mt-1 text-small text-danger">{error}</p> : null}
  </fieldset>;
}
