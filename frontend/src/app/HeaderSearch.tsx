import { useContext, useState, type FormEvent } from 'react';
import { useI18n } from '../i18n';
import { HouseholdNavigationContext } from './HouseholdNavigation';

/**
 * Compact global search. Submitting opens All items filtered by the text, using the same URL state that the
 * All items search already reads (`?q=`). It never queries the API from the header itself. On All items the
 * page already has its own search field, so the header one is hidden there rather than offering two.
 */
// Phones hide it: the All items tab already searches, and one row must hold brand, household and account.
export function HeaderSearch() {
  const { t } = useI18n();
  const navigation = useContext(HouseholdNavigationContext);
  const [query, setQuery] = useState('');
  if (!navigation?.household || navigation.route === 'allItems') return null;

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    navigation?.actions.search(query.trim());
  }

  return (
    <form role="search" onSubmit={submit} className="relative hidden min-w-0 sm:block sm:flex-1 sm:basis-40 sm:max-w-xs">
      <label htmlFor="header-search" className="sr-only">{t('search.label')}</label>
      <svg aria-hidden="true" focusable="false" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9"
        strokeLinecap="round" className="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted">
        <circle cx="11" cy="11" r="6.5" /><path d="m20 20-4.2-4.2" />
      </svg>
      <input
        id="header-search"
        type="search"
        value={query}
        onChange={(event) => setQuery(event.target.value)}
        placeholder={t('search.placeholder')}
        className="h-11 w-full min-w-0 rounded-lg border border-field bg-paper pl-8 pr-3 text-small text-ink placeholder:text-muted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
      />
    </form>
  );
}
