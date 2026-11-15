import type { MouseEvent, RefObject } from 'react';
import { AccountActions } from '../../app/AccountActions';
import { Heading } from '../../app/Heading';
import { Layout } from '../../app/Layout';
import { NavLink } from '../../app/NavLink';
import { CursorPager } from '../../components/ui/cursor-pager';
import { ListSkeleton } from '../../components/ui/list-skeleton';
import { LoadError } from '../../components/ui/load-error';
import { useI18n, type Translate } from '../../i18n';
import { signInMethod } from '../admin/adminApi';
import { AdminSectionSwitcher } from '../admin/AdminSectionSwitcher';
import type { Session } from '../auth/sessionApi';
import type { AdminHouseholdAccount } from './instanceAdminApi';
import { InstallationGone, InstallationOnlyAdmins, BACK_LINK } from './InstallationStates';
import { rereadInstanceAdminSession } from './installationSession';
import { useAdminHouseholdAccounts } from './useInstanceAdminList';

/**
 * One household of the installation, read-only: who belongs to it, which role they hold and how they sign in.
 * Nothing here changes a membership yet, and the screen never reads the household's own data or opens it, so the
 * installation grant stays a view of names and roles rather than a way into another household.
 *
 * `name` is only what the list already showed on the way in; a direct load or a reload shows the generic heading
 * instead of reading the list again to find it.
 */
export function InstallationHouseholdScreen({ householdId, name, login, onSignedOut, onSessionChanged, onBack }: {
  householdId: string;
  name: string | null;
  login: string;
  onSignedOut: () => void;
  onSessionChanged: (session: Session) => void;
  onBack: () => void;
}) {
  const { t } = useI18n();
  const list = useAdminHouseholdAccounts(householdId, {
    onSignedOut,
    onForbidden: () => { void rereadInstanceAdminSession({ onSession: onSessionChanged, onSignedOut }); },
  });

  return <Layout actions={<AccountActions login={login} onSignedOut={onSignedOut} />}>
    <section className="settle">
      <Heading className="text-h1 font-semibold text-ink">
        <span className="break-words">{name ?? t('install.household.title')}</span>
      </Heading>
      <AdminSectionSwitcher current="households" />
      <NavLink href="/admin/households" onNavigate={onBack} className={BACK_LINK}>{t('install.household.back')}</NavLink>
      {list.state === 'loading' ? <ListSkeleton label={t('admin.loading')} /> : null}
      {list.state === 'error'
        ? <LoadError className="mt-8" message={t('install.error.accounts')} retryLabel={t('app.error.retry')} onRetry={list.retry} />
        : null}
      {list.state === 'notFound' ? <InstallationGone t={t} /> : null}
      {list.state === 'forbidden' ? <InstallationOnlyAdmins t={t} /> : null}
      {list.state === 'ready' ? <Accounts
        accounts={list.items}
        stack={list.stack}
        nextCursor={list.nextCursor}
        busy={list.busy}
        headingRef={list.headingRef}
        onPrevious={list.previous}
        onNext={list.next}
        pageFailure={list.pageFailure !== null}
        onRetryPage={list.retryPage}
        t={t}
      /> : null}
    </section>
  </Layout>;
}

/** The read-only member list: the same flat divided rows and pager the household Admin center uses. */
function Accounts({ accounts, stack, nextCursor, busy, headingRef, onPrevious, onNext, pageFailure, onRetryPage, t }: {
  accounts: AdminHouseholdAccount[];
  stack: string[];
  nextCursor: string | null;
  busy: boolean;
  headingRef: RefObject<HTMLHeadingElement | null>;
  onPrevious: (event: MouseEvent<HTMLButtonElement>) => void;
  onNext: (event: MouseEvent<HTMLButtonElement>) => void;
  pageFailure: boolean;
  onRetryPage: () => void;
  t: Translate;
}) {
  return <>
    <section aria-labelledby="install-accounts-heading" className="mt-6 sm:mt-8">
      <h2 id="install-accounts-heading" ref={headingRef} tabIndex={-1} className="text-h2 text-ink outline-none">
        {t('admin.list.title')}
      </h2>
      <p className="mt-1 max-w-prose text-small text-muted">{t('install.household.explained')}</p>
      {accounts.length
        ? <ul aria-labelledby="install-accounts-heading" className="mt-3 divide-y divide-line border-t border-line">
          {accounts.map((account) => <AccountRow key={account.userId} account={account} t={t} />)}
        </ul>
        : <p className="mt-3 border-t border-line pt-3 text-body text-muted">{t('admin.list.empty')}</p>}
      {stack.length > 0 || nextCursor
        ? <CursorPager kind="account" stack={stack} cursor={nextCursor} busy={busy} onPrevious={onPrevious} onNext={onNext} t={t} />
        : null}
    </section>
    {pageFailure
      ? <LoadError className="mt-8" message={t('install.error.accounts')} retryLabel={t('app.error.retry')} onRetry={onRetryPage} />
      : null}
  </>;
}

/** One flat account row, like the household Admin center's: login, then the role and the sign-in method as text. */
function AccountRow({ account, t }: { account: AdminHouseholdAccount; t: Translate }) {
  return <li data-install-account-id={account.userId} className="py-3">
    <p className="min-w-0 break-words text-row-title font-semibold text-ink">
      <span data-install-login>{account.login}</span>
    </p>
    <p data-install-meta className="mt-0.5 text-meta leading-snug text-muted">
      <span data-install-role>{t(account.role === 'admin' ? 'members.role.admin' : 'members.role.member')}</span>
      {' '}<span aria-hidden="true" className="select-none">·</span>{' '}
      <span>{t(`admin.signIn.${signInMethod(account)}`)}</span>
    </p>
  </li>;
}
