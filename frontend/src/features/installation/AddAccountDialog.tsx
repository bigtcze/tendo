import { useEffect, useId, useRef, type RefObject } from 'react';
import { Button } from '../../components/ui/button';
import { CursorPager } from '../../components/ui/cursor-pager';
import { Dialog } from '../../components/ui/dialog';
import { Input } from '../../components/ui/input';
import { LoadError } from '../../components/ui/load-error';
import { Notice } from '../../components/ui/notice';
import { cn } from '../../lib/cn';
import { pluralKey } from '../../lib/pluralKey';
import { useI18n } from '../../i18n';
import type { AdminAccountRole, DirectoryAccount } from './instanceAdminApi';
import type { Assignment, DirectorySearch } from './useAccountAssignment';

const RING = 'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus';
const roles: AdminAccountRole[] = ['member', 'admin'];

/**
 * Puts an existing account into this household: one login search, one chosen account, one role. The dialog is a
 * search form and a form of its own — Enter in the login field searches, and only "Add to household" adds — so a
 * reader can look around without ever being one keystroke from changing the household.
 *
 * Every outcome that keeps the dialog open points at the field it is about (the search box, or the role) and every
 * outcome that ends it is announced by the screen's own live region.
 */
export function AddAccountDialog({ assignment, householdName }: {
  assignment: Assignment;
  /** Null on a direct load of the page: the title then says “an account”, not a household name it never read. */
  householdName: string | null;
}) {
  const { t } = useI18n();
  const uid = useId();
  const inputRef = useRef<HTMLInputElement>(null);
  const { open, busy, triggerRef, queryError, refusals, chosen, cancel } = assignment;
  const title = householdName === null
    ? t('install.add.title')
    : t('install.add.titleNamed', { household: householdName });
  // The search refused what was typed, so the field that has to change takes the focus and says why. It does so for
  // every refusal, not only the first one: a reader who tried the button again is standing on the button by then.
  useEffect(() => {
    if (refusals > 0) inputRef.current?.focus();
  }, [refusals]);

  return <Dialog
    open={open}
    onOpenChange={(next) => { if (!next) cancel(); }}
    title={title}
    busy={busy}
    triggerRef={triggerRef}
    initialFocusRef={inputRef}
  >
    <div className="space-y-4">
      <p className="max-w-prose text-small text-muted">{t('install.add.intro')}</p>
      <SearchForm assignment={assignment} inputId={`${uid}-query`} inputRef={inputRef} />
      <DirectoryPane search={assignment.search} chosen={chosen} queryError={queryError} busy={busy} assignment={assignment} />
      <RoleField role={assignment.role} onChange={assignment.chooseRole} disabled={busy}
        error={assignment.failure === 'requiresAdmin' ? t('install.add.error.requiresAdmin') : null} uid={uid} />
      {assignment.failure === 'alreadyMember' && chosen
        ? <Notice tone="error">{t('install.add.error.alreadyMember', { login: chosen.login })}</Notice>
        : null}
      {assignment.failure === 'failed' ? <Notice tone="error">{t('admin.error.action')}</Notice> : null}
      <div className="flex flex-wrap justify-end gap-2">
        <Button variant="quiet" disabled={busy} onClick={cancel}>{t('admin.action.cancel')}</Button>
        <Button disabled={busy || chosen === null} onClick={() => { void assignment.submit(); }}>
          {busy ? t('install.add.adding') : t('install.add.submit')}
        </Button>
      </div>
    </div>
  </Dialog>;
}

/** The one search of the dialog: a labelled field and its button, and Enter searches like the button does. */
function SearchForm({ assignment, inputId, inputRef }: {
  assignment: Assignment;
  inputId: string;
  inputRef: RefObject<HTMLInputElement | null>;
}) {
  const { t } = useI18n();
  const errorId = `${inputId}-error`;
  return <form aria-label={t('install.add.searchForm')} onSubmit={assignment.submitSearch} className="min-w-0">
    <label htmlFor={inputId} className="block text-label font-semibold text-ink">{t('install.add.query')}</label>
    <div className="mt-1.5 flex min-w-0 gap-2">
      <Input ref={inputRef} id={inputId} type="text" autoComplete="off" value={assignment.query}
        onChange={(event) => assignment.editQuery(event.target.value)}
        aria-invalid={assignment.queryError}
        aria-describedby={assignment.queryError ? errorId : undefined}
        className="min-w-0 flex-1" />
      <Button type="submit" disabled={assignment.busy} className="shrink-0 px-4 sm:px-6">{t('install.add.search')}</Button>
    </div>
    {assignment.queryError
      ? <p id={errorId} role="alert" className="mt-1.5 text-small text-danger">{t('install.add.query.invalid')}</p>
      : null}
  </form>;
}

/**
 * What the search answered: the count of this page as a polite live line, then those accounts as one radio group, or
 * the state that is there instead. Accounts already in this household are not filtered out, because the server, not
 * this pane, decides what an add means.
 *
 * The directory is a keyset list, so the count line says what this page holds and never claims to cover every match;
 * the pager is what tells a reader that there is more.
 */
function DirectoryPane({ search, chosen, queryError, busy, assignment }: {
  search: DirectorySearch;
  chosen: DirectoryAccount | null;
  queryError: boolean;
  busy: boolean;
  assignment: Assignment;
}) {
  const { t, locale } = useI18n();
  const count = search.items.length;
  const searching = search.state === 'searching';
  return <div className="min-w-0">
    <p aria-live="polite" aria-atomic="true" className="text-meta text-muted">
      {count ? t(pluralKey('install.add.count', count, locale), { count: String(count) }) : ''}
    </p>
    {searching && !count ? <p className="mt-1 text-small text-muted">{t('install.add.searching')}</p> : null}
    {count
      ? <AccountChoice items={search.items} chosen={chosen} disabled={busy || searching} onChoose={assignment.choose} />
      : null}
    {search.state === 'ready' && !count && !queryError
      ? <p className="mt-1 text-small text-muted">
        {search.needle ? t('install.add.empty', { q: search.needle }) : t('install.add.emptyAll')}
      </p>
      : null}
    {search.state === 'failed'
      ? <LoadError className="mt-2" message={t('install.error.accounts')} retryLabel={t('app.error.retry')} onRetry={assignment.retry} />
      : null}
    {search.state !== 'failed' && (search.stack.length > 0 || search.nextCursor)
      ? <CursorPager kind="directory" stack={search.stack} cursor={search.nextCursor} busy={busy || search.busy}
        onPrevious={assignment.previous} onNext={assignment.next} t={t} />
      : null}
  </div>;
}

/**
 * One radio per account, the label's whole row the target, so a long login wraps instead of clipping. The radios wait
 * while a page is on its way: a choice can only be made from rows that are really the answer to a search.
 */
function AccountChoice({ items, chosen, disabled, onChoose }: {
  items: DirectoryAccount[];
  chosen: DirectoryAccount | null;
  disabled: boolean;
  onChoose: (account: DirectoryAccount) => void;
}) {
  const { t } = useI18n();
  const name = useId();
  return <fieldset className="mt-1 min-w-0">
    <legend className="text-label font-semibold text-ink">{t('install.add.results')}</legend>
    <ul className="mt-1 min-w-0 divide-y divide-line border-y border-line">
      {items.map((account) => <li key={account.userId} className="min-w-0">
        <label className="flex min-h-11 min-w-0 cursor-pointer items-center gap-3 py-2">
          <input type="radio" name={name} value={account.userId} disabled={disabled}
            checked={chosen?.userId === account.userId} onChange={() => onChoose(account)}
            className={cn('size-5 shrink-0 accent-accent', RING)} />
          <span className="min-w-0 break-words text-body text-ink">{account.login}</span>
        </label>
      </li>)}
    </ul>
  </fieldset>;
}

/** The role the account gets; the household's own wording explains what each one can do. */
function RoleField({ role, onChange, disabled, error, uid }: {
  role: AdminAccountRole;
  onChange: (role: AdminAccountRole) => void;
  disabled: boolean;
  error: string | null;
  uid: string;
}) {
  const { t } = useI18n();
  const errorId = `${uid}-role-error`;
  return <fieldset role="radiogroup" className="min-w-0 space-y-2"
    aria-invalid={Boolean(error)} aria-describedby={error ? errorId : undefined}>
    <legend className="text-label font-semibold text-ink">{t('install.add.role')}</legend>
    <div className="flex flex-wrap gap-2 pt-1">
      {roles.map((option) => <label key={option} className="relative">
        <input type="radio" name={`${uid}-role`} value={option} checked={role === option} disabled={disabled}
          onChange={() => onChange(option)} className="peer absolute inset-0 size-full cursor-pointer opacity-0" />
        <span className={cn(
          'inline-flex min-h-11 items-center rounded-xl border border-field bg-raised px-4 text-base text-ink transition-colors',
          'peer-hover:bg-sand peer-checked:border-accent peer-checked:bg-accent peer-checked:text-on-accent',
          'peer-focus-visible:outline-2 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-focus',
          'peer-disabled:cursor-not-allowed peer-disabled:opacity-60',
        )}>{t(option === 'admin' ? 'members.role.admin' : 'members.role.member')}</span>
      </label>)}
    </div>
    <p className="text-meta text-muted">{t('members.roles.explained')}</p>
    {error ? <p id={errorId} role="alert" className="text-small text-danger">{error}</p> : null}
  </fieldset>;
}
