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
  return <Layout actions={<AccountActions login={login} onSignedOut={onSignedOut} />}>
    <section className="settle space-y-6">
      <p role="status" aria-live="polite" aria-atomic="true" className="sr-only">{outcomeText(view, context, page, t, locale)}</p>
      <Heading className="font-display text-2xl leading-tight sm:text-h1">{t('allItems.title')}</Heading>
      <div className="grid min-w-0 grid-cols-1 gap-2 lg:grid-cols-[minmax(15rem,18rem)_minmax(0,1fr)] lg:items-start lg:gap-8">
        <aside className="min-w-0 space-y-3 lg:sticky lg:top-6">
          <AllItemsSearch draft={draft} page={page} actions={actions} searchRef={searchRef} />
          <AllItemsFilters draft={draft} summary={page.summary} facets={facets} userId={userId}
            actions={actions} showClear={view !== 'noMatch'} />
        </aside>
        <div className="min-w-0 space-y-4">
          {page.notice ? <Notice>{t('allItems.stale')}</Notice> : null}
          {page.loading ? <p role="status" className="text-muted">{t('items.loading')}</p> : null}
          {page.error ? <Notice tone="error" role={null} className="space-y-3"><p>{t('app.error.body')}</p>
            <Button data-all-items-retry type="button" variant="secondary" onClick={actions.retry}>{t('app.error.retry')}</Button></Notice> : null}
          <AllItemsResults view={view} context={context} page={page} subjectNames={subjectNames} members={facets.members.values}
            actions={actions} resultRef={resultRef} onBack={onBack} onOpenItem={onOpenItem} />
        </div>
      </div>
    </section>
  </Layout>;
}
