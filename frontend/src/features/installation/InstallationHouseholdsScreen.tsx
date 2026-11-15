import type { MouseEvent, RefObject } from 'react';
import { AccountActions } from '../../app/AccountActions';
import { Heading } from '../../app/Heading';
import { Layout } from '../../app/Layout';
import { NavLink } from '../../app/NavLink';
import { CursorPager } from '../../components/ui/cursor-pager';
import { ListSkeleton } from '../../components/ui/list-skeleton';
import { LoadError } from '../../components/ui/load-error';
import { useI18n, type Translate } from '../../i18n';
import { AdminSectionSwitcher } from '../admin/AdminSectionSwitcher';
import type { Session } from '../auth/sessionApi';
import type { AdminHousehold } from './instanceAdminApi';
import { InstallationOnlyAdmins } from './InstallationStates';
import { rereadInstanceAdminSession } from './installationSession';
import { useAdminHouseholdList } from './useInstanceAdminList';

const ROW_LINK = [
  'inline-flex min-h-11 max-w-full items-center text-row-title font-semibold text-ink underline-offset-4 hover:underline',
  'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus',
].join(' ');

function formatCreated(iso: string, locale: string): string {
  return new Intl.DateTimeFormat(locale, { dateStyle: 'medium' }).format(new Date(iso));
}

/**
 * The installation-wide household list: the Admin center of an account that holds the installation grant. It
 * shows only what a household is (name, timezone, when it was created) and never its contents, so opening one is
 * a plain drill-down, not a household switch.
 */
export function InstallationHouseholdsScreen({ login, onSignedOut, onSessionChanged, onOpenHousehold }: {
  login: string;
  onSignedOut: () => void;
  onSessionChanged: (session: Session) => void;
  onOpenHousehold: (householdId: string, name: string) => void;
}) {
  const { t, locale } = useI18n();
  const list = useAdminHouseholdList({
    onSignedOut,
    onForbidden: () => { void rereadInstanceAdminSession({ onSession: onSessionChanged, onSignedOut }); },
  });

  return <Layout actions={<AccountActions login={login} onSignedOut={onSignedOut} />}>
    <section className="settle">
      <Heading className="text-h1 font-semibold text-ink">{t('install.account.title')}</Heading>
      <AdminSectionSwitcher current="households" />
      {list.state === 'loading' ? <ListSkeleton label={t('install.loading')} /> : null}
      {/* A household list is never missing; an unexpected 404 still gets the message that can be retried. */}
      {list.state === 'error' || list.state === 'notFound'
        ? <LoadError className="mt-8" message={t('install.error.households')} retryLabel={t('app.error.retry')} onRetry={list.retry} />
        : null}
      {list.state === 'forbidden' ? <InstallationOnlyAdmins t={t} /> : null}
      {list.state === 'ready' ? <Households
        households={list.items}
        locale={locale}
        stack={list.stack}
        nextCursor={list.nextCursor}
        busy={list.busy}
        headingRef={list.headingRef}
        onPrevious={list.previous}
        onNext={list.next}
        pageFailure={list.pageFailure !== null}
        onRetryPage={list.retryPage}
        onOpen={onOpenHousehold}
        t={t}
      /> : null}
    </section>
  </Layout>;
}

function Households({ households, locale, stack, nextCursor, busy, headingRef, onPrevious, onNext, pageFailure, onRetryPage, onOpen, t }: {
  households: AdminHousehold[];
  locale: string;
  stack: string[];
  nextCursor: string | null;
  busy: boolean;
  headingRef: RefObject<HTMLHeadingElement | null>;
  onPrevious: (event: MouseEvent<HTMLButtonElement>) => void;
  onNext: (event: MouseEvent<HTMLButtonElement>) => void;
  pageFailure: boolean;
  onRetryPage: () => void;
  onOpen: (householdId: string, name: string) => void;
  t: Translate;
}) {
  return <>
    <section aria-labelledby="install-households-heading" className="mt-6 sm:mt-8">
      <h2 id="install-households-heading" ref={headingRef} tabIndex={-1} className="text-h2 text-ink outline-none">
        {t('install.title')}
      </h2>
      <p className="mt-1 max-w-prose text-small text-muted">{t('install.explained')}</p>
      {households.length
        ? <ul aria-labelledby="install-households-heading" className="mt-3 divide-y divide-line border-t border-line">
          {households.map((household) => (
            <HouseholdRow key={household.id} household={household} locale={locale} onOpen={onOpen} t={t} />
          ))}
        </ul>
        : <p className="mt-3 border-t border-line pt-3 text-body text-muted">{t('install.empty')}</p>}
      {stack.length > 0 || nextCursor
        ? <CursorPager kind="household" stack={stack} cursor={nextCursor} busy={busy} onPrevious={onPrevious} onNext={onNext} t={t} />
        : null}
    </section>
    {pageFailure
      ? <LoadError className="mt-8" message={t('install.error.households')} retryLabel={t('app.error.retry')} onRetry={onRetryPage} />
      : null}
  </>;
}

/** One flat household row: the name opens the household, then one muted line stating where and when it began. */
function HouseholdRow({ household, locale, onOpen, t }: {
  household: AdminHousehold;
  locale: string;
  onOpen: (householdId: string, name: string) => void;
  t: Translate;
}) {
  return <li data-install-household-id={household.id} className="py-3">
    <NavLink href={`/admin/households/${household.id}`} onNavigate={() => onOpen(household.id, household.name)} className={ROW_LINK}>
      <span className="break-words">{household.name}</span>
    </NavLink>
    <p data-install-household-meta className="mt-0.5 text-meta leading-snug text-muted">
      <span>{household.timezone}</span>
      {' '}<span aria-hidden="true" className="select-none">·</span>{' '}
      <span>{t('install.created', { date: formatCreated(household.createdAt, locale) })}</span>
    </p>
  </li>;
}
