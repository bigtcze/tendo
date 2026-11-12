import { AccountActions } from '../../app/AccountActions';
import { Heading } from '../../app/Heading';
import { Layout } from '../../app/Layout';
import { Button } from '../../components/ui/button';
import { Notice } from '../../components/ui/notice';
import { useI18n } from '../../i18n';
import { AllItemsFilters, AllItemsSearch } from './AllItemsControls';
import { AllItemsResults, outcomeText, resultsView } from './AllItemsResults';
import { buildQuery } from './allItemsUrl';
import { useAllItems } from './useAllItems';
import { useAllItemsFacets } from './useAllItemsFacets';

/** The screen name and its one supporting sentence, in the same measured column as the list. */
function AllItemsHeader() {
  const { t } = useI18n();
  return <header className="min-w-0">
    <Heading className="text-h1 font-semibold text-ink">{t('allItems.title')}</Heading>
    <p className="mt-1.5 max-w-prose text-body text-muted">{t('allItems.subtitle')}</p>
  </header>;
}

export function AllItemsScreen({ householdId, login, userId, onBack, onSignedOut, onOpenItem }: {
  householdId: string;
  login: string;
  userId: string;
  onBack: () => void;
  onSignedOut: () => void;
  onOpenItem: (itemId: string, returnTo: string) => void;
}) {
  const { t, locale } = useI18n();
  const { context, draft, page, actions, resultRef, searchRef, onNoHousehold } = useAllItems(householdId, onSignedOut);
  const facets = useAllItemsFacets(householdId, onSignedOut, onNoHousehold);
  const view = resultsView(context, page, buildQuery(context));
  const subjectNames = new Map(facets.subjects.values.map((subject) => [subject.id, subject.name]));
  const subjectTypes = new Map(facets.subjects.values.map((subject) => [subject.id, subject.type]));
  return <Layout actions={<AccountActions login={login} onSignedOut={onSignedOut} />}>
    <section className="settle measure min-w-0 space-y-6 max-sm:space-y-5">
      <p role="status" aria-live="polite" aria-atomic="true" className="sr-only">{outcomeText(view, context, page, t, locale)}</p>
      <AllItemsHeader />
      <div className="min-w-0 space-y-4 border-b border-line pb-5">
        <AllItemsSearch draft={draft} page={page} actions={actions} searchRef={searchRef} />
        <AllItemsFilters draft={draft} summary={page.summary} facets={facets} userId={userId}
          actions={actions} showClear={view !== 'noMatch'} />
      </div>
      <div className="min-w-0 space-y-4 max-sm:space-y-3">
        {page.notice ? <Notice>{t('allItems.stale')}</Notice> : null}
        {page.loading ? <p role="status" className="text-small text-muted">{t('items.loading')}</p> : null}
        {page.error ? <Notice tone="error" role={null} className="space-y-3"><p>{t('app.error.body')}</p>
          <Button data-all-items-retry type="button" variant="secondary" onClick={actions.retry}>{t('app.error.retry')}</Button></Notice> : null}
        <AllItemsResults view={view} context={context} page={page} subjectNames={subjectNames} subjectTypes={subjectTypes}
          members={facets.members.values} actions={actions} resultRef={resultRef} onBack={onBack} onOpenItem={onOpenItem} />
      </div>
    </section>
  </Layout>;
}
