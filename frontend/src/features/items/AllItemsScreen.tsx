import { AccountActions } from '../../app/AccountActions';
import { Heading } from '../../app/Heading';
import { Layout } from '../../app/Layout';
import { Button } from '../../components/ui/button';
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
    <section className="settle space-y-5">
      <p role="status" aria-live="polite" aria-atomic="true" className="sr-only">{outcomeText(view, context, page, t, locale)}</p>
      <Heading className="font-display text-h1 leading-tight">{t('allItems.title')}</Heading>
      <AllItemsSearch draft={draft} page={page} actions={actions} searchRef={searchRef} />
      <AllItemsFilters draft={draft} summary={page.summary} facets={facets} userId={userId} actions={actions} />
      {page.notice ? <p role="status" aria-live="polite" className="rounded-xl bg-sand px-4 py-2">{t('allItems.stale')}</p> : null}
      {page.loading ? <p role="status" className="text-muted">{t('items.loading')}</p> : null}
      {page.error ? <div><p className="text-muted">{t('app.error.body')}</p>
        <Button type="button" variant="quiet" onClick={actions.retry}>{t('app.error.retry')}</Button></div> : null}
      <AllItemsResults view={view} context={context} page={page} subjectNames={subjectNames} members={facets.members.values}
        actions={actions} resultRef={resultRef} onBack={onBack} onOpenItem={onOpenItem} />
    </section>
  </Layout>;
}
