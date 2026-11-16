import { useEffect, useRef, useState, type MouseEvent, type RefObject } from 'react';
import { AccountActions } from '../../app/AccountActions';
import { Heading } from '../../app/Heading';
import { Layout } from '../../app/Layout';
import { Button } from '../../components/ui/button';
import { CursorPager } from '../../components/ui/cursor-pager';
import { Input } from '../../components/ui/input';
import { ListSkeleton } from '../../components/ui/list-skeleton';
import { LoadError } from '../../components/ui/load-error';
import { Notice } from '../../components/ui/notice';
import { codePointLength } from '../../lib/textLength';
import { useI18n, type Translate } from '../../i18n';
import type { NoticeSpec } from '../admin/useRoleChange';
import { AdminSectionSwitcher } from '../admin/AdminSectionSwitcher';
import { DeleteAccountDialog } from './DeleteAccountDialog';
import type { DirectoryAccount } from './instanceAdminApi';
import { InstallationOnlyAdmins } from './InstallationStates';
import { useAccountDeletion } from './useAccountDeletion';
import { MAX_QUERY } from './useDirectorySearch';
import { useAdminAccountDirectory, type DirectoryList } from './useInstanceAdminList';

const QUERY_ID = 'install-directory-query';
const QUERY_ERROR_ID = `${QUERY_ID}-error`;

/**
 * Every account of the installation: the Admin center section next to the household list, for the same installation
 * grant. It is a directory, not a household: the rows are logins only, and the one action on them deletes the
 * account everywhere — changing a single household stays the household list's job.
 *
 * The list is the account directory itself, so the search is server-side and every page says which search it
 * answers. Nothing is deleted locally: the dialog asks the reader to repeat the login, the server decides, and the
 * list is then read again.
 */
export function InstallationAccountsScreen({ login, viewerId, onSignedOut, onForbidden }: {
  login: string;
  viewerId: string;
  onSignedOut: () => void;
  onForbidden: () => void;
}) {
  const { t } = useI18n();
  const queryRef = useRef<HTMLInputElement>(null);
  const forbiddenHeadingRef = useRef<HTMLHeadingElement>(null);
  /** Set by the dialog's own refusal: that calm refusal replaces the list the trigger lived in. */
  const refusedByDeletionRef = useRef(false);
  const [query, setQuery] = useState('');
  /** The search that was submitted: a new object on every press, so Search always reads again. */
  const [submitted, setSubmitted] = useState({ needle: '' });
  const [queryError, setQueryError] = useState(false);
  const [notice, setNotice] = useState<NoticeSpec | null>(null);
  const list = useAdminAccountDirectory(submitted, { onSignedOut, onForbidden });
  const deletion = useAccountDeletion({
    onSignedOut,
    // An authoritative refusal says the grant hint is stale: drop the rows and let the shell re-read the session.
    onForbidden: () => { refusedByDeletionRef.current = true; list.markForbidden(); onForbidden(); },
    onNotice: setNotice,
    onReload: () => { list.reload(); },
    landingRef: list.headingRef,
  });

  // A search the server refused is answered on the field, the same as one this screen refused before sending it —
  // every time, so pressing Search again on the same refused text puts the reader back on the field too. The refusal
  // is the submitted object itself, so this follows the search it answered and never a page or another read.
  useEffect(() => {
    if (list.refused !== null && list.refused === submitted) queryRef.current?.focus();
  }, [list.refused, submitted]);

  // The grant is refused while the dialog is open: the trigger leaves with its row, so the refusal's heading takes
  // the focus the dialog can no longer hand back.
  useEffect(() => {
    if (list.state !== 'forbidden' || !refusedByDeletionRef.current) return;
    refusedByDeletionRef.current = false;
    forbiddenHeadingRef.current?.focus();
  }, [list.state]);

  function submitSearch() {
    const needle = query.trim();
    // The server counts code points, so an over-long search is answered here instead of by a request that must fail.
    if (codePointLength(needle) > MAX_QUERY) { setQueryError(true); queryRef.current?.focus(); return; }
    setQueryError(false);
    setSubmitted({ needle });
  }

  return <Layout actions={<AccountActions login={login} onSignedOut={onSignedOut} />}>
    <section className="settle">
      <Heading className="text-h1 font-semibold text-ink">{t('install.page.title')}</Heading>
      {/* The one live host, exactly where the other installation screens keep it. */}
      <div role="status" aria-atomic="true" className="mt-6 empty:mt-0">
        {notice ? <Notice tone={notice.tone === 'success' ? 'success' : 'neutral'} role={null} className="min-w-0 break-words">{t(notice.key, notice.params)}</Notice> : null}
      </div>
      <AdminSectionSwitcher current="accounts" />
      {list.state === 'forbidden'
        ? <InstallationOnlyAdmins t={t} headingRef={forbiddenHeadingRef} />
        : <DirectorySection
          list={list}
          needle={submitted.needle}
          query={query}
          invalid={queryError || list.invalidQuery}
          queryRef={queryRef}
          viewerId={viewerId}
          deleteBusy={deletion.busy}
          onQuery={setQuery}
          onSearch={submitSearch}
          onDelete={deletion.request}
          t={t}
        />}
    </section>
    <DeleteAccountDialog deletion={deletion} />
  </Layout>;
}

type DirectorySectionProps = {
  list: DirectoryList;
  /** The search these rows answer; an empty page says which one found nothing. */
  needle: string;
  query: string;
  /** The search text is unusable: the field says what to fix, and there is nothing to retry. */
  invalid: boolean;
  queryRef: RefObject<HTMLInputElement | null>;
  viewerId: string;
  /** Only the dialog's own request disables a row's action; the quiet reload never does. */
  deleteBusy: boolean;
  onQuery: (value: string) => void;
  onSearch: () => void;
  onDelete: (account: DirectoryAccount, trigger: HTMLElement) => void;
  t: Translate;
};

/** The section heading, which takes the focus a pager click leaves behind once the new page has settled. */
function DirectoryHeading({ headingRef, t }: { headingRef: RefObject<HTMLHeadingElement | null>; t: Translate }) {
  return <h2 id="install-directory-heading" ref={headingRef} tabIndex={-1} className="text-h2 text-ink outline-none">
    {t('admin.list.title')}
  </h2>;
}

/** The directory section: its heading, the login search, and the state that is in the rows' place instead. */
function DirectorySection({ list, needle, query, invalid, queryRef, viewerId, deleteBusy, onQuery, onSearch, onDelete, t }: DirectorySectionProps) {
  return <section aria-labelledby="install-directory-heading" className="mt-6 sm:mt-8">
    <DirectoryHeading headingRef={list.headingRef} t={t} />
    <p className="mt-1 max-w-prose text-small text-muted">{t('install.accounts.explained')}</p>
    <SearchField query={query} invalid={invalid} fieldRef={queryRef} onQuery={onQuery} onSearch={onSearch} />
    {list.state === 'loading' ? <ListSkeleton label={t('admin.loading')} /> : null}
    {list.state === 'error' && !list.invalidQuery
      ? <LoadError className="mt-6" message={t('install.error.accounts')} retryLabel={t('app.error.retry')} onRetry={list.retry} />
      : null}
    {list.state === 'ready' ? <Rows
      accounts={list.items}
      needle={needle}
      viewerId={viewerId}
      stack={list.stack}
      nextCursor={list.nextCursor}
      busy={list.busy}
      deleteBusy={deleteBusy}
      pageFailure={list.pageFailure !== null}
      onPrevious={list.previous}
      onNext={list.next}
      onRetryPage={list.retryPage}
      onDelete={onDelete}
      t={t}
    /> : null}
  </section>;
}

/** One login search: Enter in the field searches, and an unusable search is said under the field it belongs to. */
function SearchField({ query, invalid, fieldRef, onQuery, onSearch }: {
  query: string;
  invalid: boolean;
  fieldRef: RefObject<HTMLInputElement | null>;
  onQuery: (value: string) => void;
  onSearch: () => void;
}) {
  const { t } = useI18n();
  return <form aria-label={t('install.accounts.searchForm')}
    onSubmit={(event) => { event.preventDefault(); onSearch(); }} className="mt-4">
    <label htmlFor={QUERY_ID} className="block text-label font-semibold text-ink">{t('install.add.query')}</label>
    <div className="mt-1.5 flex min-w-0 gap-2">
      <Input ref={fieldRef} id={QUERY_ID} type="text" autoComplete="off" value={query}
        onChange={(event) => onQuery(event.target.value)}
        aria-invalid={invalid}
        aria-describedby={invalid ? QUERY_ERROR_ID : undefined}
        className="min-w-0 flex-1" />
      <Button type="submit" className="shrink-0 px-4 sm:px-6">{t('install.add.search')}</Button>
    </div>
    {invalid
      ? <p id={QUERY_ERROR_ID} role="alert" className="mt-1.5 text-small text-danger">{t('install.accounts.query.invalid')}</p>
      : null}
  </form>;
}

/** The directory rows and their pager; a page failure keeps the rows on screen and offers that page again. */
function Rows({ accounts, needle, viewerId, stack, nextCursor, busy, deleteBusy, pageFailure, onPrevious, onNext, onRetryPage, onDelete, t }: {
  accounts: DirectoryAccount[];
  needle: string;
  viewerId: string;
  stack: string[];
  nextCursor: string | null;
  /** A page is on its way: the pager waits for it. */
  busy: boolean;
  deleteBusy: boolean;
  pageFailure: boolean;
  onPrevious: (event: MouseEvent<HTMLButtonElement>) => void;
  onNext: (event: MouseEvent<HTMLButtonElement>) => void;
  onRetryPage: () => void;
  onDelete: (account: DirectoryAccount, trigger: HTMLElement) => void;
  t: Translate;
}) {
  return <>
    {accounts.length
      ? <ul aria-labelledby="install-directory-heading" className="mt-3 divide-y divide-line border-t border-line">
        {accounts.map((account) => <AccountRow
          key={account.userId}
          account={account}
          self={account.userId === viewerId}
          busy={deleteBusy}
          onDelete={onDelete}
          t={t}
        />)}
      </ul>
      : <p className="mt-3 min-w-0 break-words border-t border-line pt-3 text-body text-muted">
        {needle ? t('install.accounts.emptySearch', { q: needle }) : t('install.accounts.empty')}
      </p>}
    {stack.length > 0 || nextCursor
      ? <CursorPager kind="account" stack={stack} cursor={nextCursor} busy={busy} onPrevious={onPrevious} onNext={onNext} t={t} />
      : null}
    {pageFailure
      ? <LoadError className="mt-8" message={t('install.error.accounts')} retryLabel={t('app.error.retry')} onRetry={onRetryPage} />
      : null}
  </>;
}

/**
 * One flat account row: the login, which wraps rather than clipping, and the one action. The viewer's own row says
 * so and offers no action at all — no account deletes itself, and the server refuses it too.
 */
function AccountRow({ account, self, busy, onDelete, t }: {
  account: DirectoryAccount;
  self: boolean;
  busy: boolean;
  onDelete: (account: DirectoryAccount, trigger: HTMLElement) => void;
  t: Translate;
}) {
  return <li data-install-directory-id={account.userId} className="flex flex-wrap items-center gap-x-3 gap-y-2 py-3">
    <p className="min-w-0 flex-1 basis-40 break-words text-row-title font-semibold text-ink">
      <span data-install-login>{account.login}</span>
      {self ? <span className="font-normal text-muted"> ({t('members.you')})</span> : null}
    </p>
    {self ? null : <Button
      variant="secondary"
      size="small"
      disabled={busy}
      aria-label={t('install.accounts.delete.label', { login: account.login })}
      onClick={(event) => onDelete(account, event.currentTarget)}
    >
      {t('install.accounts.delete.action')}
    </Button>}
  </li>;
}
