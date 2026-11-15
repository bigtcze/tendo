import { useEffect, useId, useRef, type RefObject } from 'react';
import { Button } from '../../components/ui/button';
import { CursorPager } from '../../components/ui/cursor-pager';
import { Input } from '../../components/ui/input';
import { LoadError } from '../../components/ui/load-error';
import { cn } from '../../lib/cn';
import { pluralKey } from '../../lib/pluralKey';
import { useI18n } from '../../i18n';
import type { DirectoryAccount } from './instanceAdminApi';
import type { DirectorySearch } from './useDirectorySearch';

const RING = 'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus';

type AccountPickerProps = {
  /** Prefix for the field, error and group ids; two pickers on one page never share a name. */
  id: string;
  query: string;
  chosen: DirectoryAccount | null;
  search: DirectorySearch;
  /** The server refused the search text itself: the field says so and takes the focus. */
  queryError: boolean;
  /** Counts search refusals, so every one of them puts the reader back on the field, a repeated one included. */
  refusals: number;
  /** An answer about the chosen account, said under the field that chooses it. */
  choiceError?: string | null;
  /** A hint that stays under the field and is part of its accessible description. */
  hint?: string | null;
  /** The caller's ref to the field, so a dialog can make it the first thing the reader lands on. */
  inputRef?: RefObject<HTMLInputElement | null>;
  busy: boolean;
  onQuery: (value: string) => void;
  onSearch: () => void;
  onChoose: (account: DirectoryAccount) => void;
  onNext: () => void;
  onPrevious: () => void;
  onRetry: () => void;
};

/**
 * The account directory picker shared by both installation admin dialogs: one login search, one chosen account.
 * The dialog is a search form and a form of its own — Enter searches and never chooses — so a reader can look
 * around without ever being one keystroke from changing anything.
 *
 * Every answer about the search or the chosen account points at the field it is about: the field takes the focus
 * and carries the description. Labels and counts are the directory's own words, reused from the add dialog.
 */
export function AccountPicker({
  id, query, chosen, search, queryError, refusals, choiceError = null, hint = null, inputRef: callerRef, busy,
  onQuery, onSearch, onChoose, onNext, onPrevious, onRetry,
}: AccountPickerProps) {
  const { t } = useI18n();
  const inputId = `${id}-query`;
  const hintId = hint ? `${id}-hint` : undefined;
  const errorId = `${id}-error`;
  const ownRef = useRef<HTMLInputElement>(null);
  const inputRef = callerRef ?? ownRef;
  // Both an unusable search and a refused account land on the same field: it is where the reader has to answer.
  const answered = queryError || choiceError !== null;
  useEffect(() => {
    if (refusals > 0 || choiceError !== null) inputRef.current?.focus();
  }, [refusals, choiceError, inputRef]);

  return <div className="min-w-0">
    <label htmlFor={inputId} className="block text-label font-semibold text-ink">{t('install.add.query')}</label>
    <form aria-label={t('install.add.searchForm')} onSubmit={(event) => { event.preventDefault(); onSearch(); }} className="mt-1.5 min-w-0">
      <div className="flex min-w-0 gap-2">
        <Input ref={inputRef} id={inputId} type="text" autoComplete="off" value={query}
          onChange={(event) => onQuery(event.target.value)}
          aria-invalid={answered}
          aria-describedby={[answered ? errorId : undefined, hintId].filter(Boolean).join(' ') || undefined}
          className="min-w-0 flex-1" />
        <Button type="submit" disabled={busy} className="shrink-0 px-4 sm:px-6">{t('install.add.search')}</Button>
      </div>
    </form>
    {hint ? <p id={hintId} className="mt-1.5 text-small text-muted">{hint}</p> : null}
    {answered ? <p id={errorId} role="alert" className="mt-1.5 text-small text-danger">{queryError ? t('install.add.query.invalid') : choiceError}</p> : null}
    <DirectoryPane search={search} chosen={chosen} busy={busy} onChoose={onChoose} onNext={onNext} onPrevious={onPrevious} onRetry={onRetry} />
  </div>;
}

/**
 * What the search answered: the count of this page as a polite live line, then those accounts as one radio group, or
 * the state that is there instead. Accounts already in a household are not filtered out, because the server, not
 * this pane, decides what a choice means.
 *
 * The directory is a keyset list, so the count line says what this page holds and never claims to cover every match;
 * the pager is what tells a reader that there is more.
 */
function DirectoryPane({ search, chosen, busy, onChoose, onNext, onPrevious, onRetry }: {
  search: DirectorySearch;
  chosen: DirectoryAccount | null;
  busy: boolean;
  onChoose: (account: DirectoryAccount) => void;
  onNext: () => void;
  onPrevious: () => void;
  onRetry: () => void;
}) {
  const { t, locale } = useI18n();
  const count = search.items.length;
  const searching = search.state === 'searching';
  return <div className="mt-4 min-w-0">
    <p aria-live="polite" aria-atomic="true" className="text-meta text-muted">
      {count ? t(pluralKey('install.add.count', count, locale), { count: String(count) }) : ''}
    </p>
    {searching && !count ? <p className="mt-1 text-small text-muted">{t('install.add.searching')}</p> : null}
    {count
      ? <AccountChoice items={search.items} chosen={chosen} disabled={busy || searching} onChoose={onChoose} />
      : null}
    {search.state === 'ready' && !count
      ? <p className="mt-1 text-small text-muted">
        {search.needle ? t('install.add.empty', { q: search.needle }) : t('install.add.emptyAll')}
      </p>
      : null}
    {search.state === 'failed'
      ? <LoadError className="mt-2" message={t('install.error.accounts')} retryLabel={t('app.error.retry')} onRetry={onRetry} />
      : null}
    {search.state !== 'failed' && (search.stack.length > 0 || search.nextCursor)
      ? <CursorPager kind="directory" stack={search.stack} cursor={search.nextCursor} busy={busy || search.busy}
        onPrevious={onPrevious} onNext={onNext} t={t} />
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
