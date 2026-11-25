import { useMemo, useRef } from 'react';
import { useAppendedRowFocus } from '../../app/useAppendedRowFocus';
import { useShowMoreList, type ShowMoreList } from '../../app/useShowMoreList';
import { Button } from '../../components/ui/button';
import { LoadError } from '../../components/ui/load-error';
import { LoadingLine } from '../../components/ui/loading-line';
import { useI18n, type MessageKey, type Translate } from '../../i18n';
import { listAiAppsChanges, type AiAppsChange, type AiAppsChangesPage } from './aiAppsApi';
import { accessWord, type AccessWord } from './useAccountAiApps';

const HEADING_ID = 'ai-apps-changes-heading';

type Failed = Exclude<AiAppsChangesPage, { kind: 'ok' }>;

/**
 * Every change to AI apps, newest first, read by cursor. A session that ended signs out; a lost installation grant
 * is handed to the screen, which says so in place of the whole page. Any other failure keeps the rows and retries.
 */
export function useAiAppsChanges({ onSignedOut, onForbidden }: {
  onSignedOut: () => void;
  /** `duringAction` says the refusal followed something the reader did, so focus has somewhere to go. */
  onForbidden: (duringAction: boolean) => void;
}): ShowMoreList<AiAppsChange> {
  // The options are read when used, so these closures always see the screen's current handlers.
  return useShowMoreList<AiAppsChange, Failed>({
    read: listAiAppsChanges,
    handOff: (failure, duringAction) => {
      if (failure.kind === 'unauthenticated') { onSignedOut(); return true; }
      if (failure.kind === 'forbidden') { onForbidden(duringAction); return true; }
      return false;
    },
  });
}

/** What apps may do, as words that finish “The most apps may do: …”; `null` hands it back to the default. */
const MOST: Record<AccessWord | 'default', MessageKey> = {
  default: 'aiApps.changes.access.default',
  none: 'aiApps.changes.access.default',
  read: 'aiApps.changes.access.read',
  write: 'aiApps.changes.access.write',
  both: 'aiApps.changes.access.both',
};
const OWN: Record<AccessWord, MessageKey> = {
  none: 'accountAiApps.own.off',
  read: 'accountAiApps.own.read',
  write: 'accountAiApps.own.write',
  both: 'accountAiApps.own.both',
};

function most(change: AiAppsChange, t: Translate): string {
  const word = change.scopes === null ? 'default' : accessWord(change.scopes);
  return t('aiApps.changes.most', { access: t(MOST[word]) });
}

/** Who the change was for, in the reader's words: everyone, one account, or a person's own choice. */
function changeTitle(change: AiAppsChange, t: Translate): string {
  if (change.policy === 'installation') return t('aiApps.changes.everyone');
  const login = change.targetLogin;
  if (change.policy === 'account') return login ? t('accountAiApps.titleFor', { login }) : t('aiApps.changes.accountDeleted');
  return login ? t('aiApps.changes.own', { login }) : t('aiApps.changes.ownDeleted');
}

/** What the setting became, never which fields changed: each row is the whole setting as it stood after the change. */
function changeBody(change: AiAppsChange, t: Translate): string {
  if (change.policy === 'installation') {
    return [
      t(change.enabled ? 'aiApps.changes.installation.on' : 'aiApps.changes.installation.off'),
      t(change.defaultAccountEnabled ? 'aiApps.changes.accounts.on' : 'aiApps.changes.accounts.off'),
      most(change, t),
    ].join(' ');
  }
  if (change.policy === 'account') {
    let use: MessageKey = 'aiApps.changes.account.default';
    if (change.enabled !== null) use = change.enabled ? 'aiApps.changes.account.on' : 'aiApps.changes.account.off';
    return `${t(use)} ${most(change, t)}`;
  }
  return t(change.enabled ? OWN[accessWord(change.scopes ?? [])] : 'accountAiApps.own.off');
}

function ChangeRow({ change, format, t }: { change: AiAppsChange; format: Intl.DateTimeFormat; t: Translate }) {
  const when = format.format(new Date(change.occurredAt));
  // Focusable only by script: the first row Show more added when the button that had focus went away.
  return <li tabIndex={-1}
    className="min-w-0 py-4 [overflow-wrap:anywhere] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus">
    <p className="text-body font-semibold text-ink">{changeTitle(change, t)}</p>
    <p className="mt-1 max-w-prose text-body text-ink">{changeBody(change, t)}</p>
    <p className="mt-1 text-small text-muted">
      {change.actorLogin ? t('aiApps.changes.by', { login: change.actorLogin, when }) : t('aiApps.changes.byDeleted', { when })}
    </p>
  </li>;
}

/**
 * The installation's record of AI app changes, for its administrators: who changed what, and when. Rows say what a
 * setting became in the same words as the settings, never ids or access codes.
 */
export function AiAppsChanges({ changes }: { changes: ShowMoreList<AiAppsChange> }) {
  const { t, locale } = useI18n();
  const format = useMemo(() => new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' }), [locale]);
  const listRef = useRef<HTMLUListElement>(null);
  const markMore = useAppendedRowFocus(listRef, changes.items.length, changes.loadingMore);
  const more = () => { markMore(changes.items.length); void changes.loadMore(); };
  const empty = changes.state === 'ready' && changes.items.length === 0 && !changes.refreshFailed;

  return <section aria-labelledby={HEADING_ID} className="mt-8 grid max-w-2xl gap-1 border-t border-line pt-6">
    <h2 id={HEADING_ID} className="text-h2 text-ink">{t('aiApps.changes.title')}</h2>
    <p className="max-w-prose text-body text-muted">{t('aiApps.changes.body')}</p>
    {changes.state === 'loading' ? <LoadingLine className="mt-4">{t('aiApps.changes.loading')}</LoadingLine> : null}
    {changes.state === 'error' || changes.refreshFailed
      ? <LoadError className="mt-4" message={t('aiApps.changes.error.load')} retryLabel={t('app.error.retry')} onRetry={changes.reload} />
      : null}
    {empty ? <p className="mt-4 max-w-prose text-body text-muted">{t('aiApps.changes.empty')}</p> : null}
    {changes.items.length > 0
      ? <ul ref={listRef} aria-labelledby={HEADING_ID} className="mt-2 divide-y divide-line">
        {changes.items.map((change) => <ChangeRow key={change.id} change={change} format={format} t={t} />)}
      </ul>
      : null}
    {changes.moreFailed
      ? <LoadError className="mt-2" message={t('aiApps.changes.error.more')} retryLabel={t('app.error.retry')} onRetry={more} />
      : null}
    {changes.nextCursor
      ? <Button variant="secondary" className="mt-2 w-full sm:w-auto sm:justify-self-start aria-disabled:cursor-not-allowed aria-disabled:opacity-60" aria-disabled={changes.loadingMore}
        onClick={more}>
        {t('aiApps.changes.more')}
      </Button>
      : null}
  </section>;
}
