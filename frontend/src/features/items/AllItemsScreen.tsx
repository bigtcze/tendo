import { useEffect } from 'react';
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

export function AllItemsScreen({ householdId, login, userId, onBack, onSignedOut, onOpenItem, onArchivedViewChange }: {
  householdId: string;
  login: string;
  userId: string;
  onBack: () => void;
  onSignedOut: () => void;
  onOpenItem: (itemId: string, returnTo: string) => void;
  onArchivedViewChange?: (archived: boolean) => void;
}) {
  const { t, locale } = useI18n();
  const { context, draft, page, actions, resultRef, searchRef, onNoHousehold } = useAllItems(householdId, onSignedOut);
  // The Archive link's selected state follows what the filters really show, including edits made on this screen.
  const archived = context.status === 'archived';
  useEffect(() => { onArchivedViewChange?.(archived); }, [archived, onArchivedViewChange]);
  const facets = useAllItemsFacets(householdId, onSignedOut, onNoHousehold);
  const view = resultsView(context, page, buildQuery(context));
  const subjectNames = new Map(facets.subjects.values.map((subject) => [subject.id, subject.name]));
  const subjectTypes = new Map(facets.subjects.values.map((subject) => [subject.id, subject.type]));
  return <Layout actions={<AccountActions login={login} onSignedOut={onSignedOut} />}>
    <section className="settle min-w-0 space-y-6 max-sm:space-y-4">
      <p role="status" aria-live="polite" aria-atomic="true" className="sr-only">{outcomeText(view, context, page, t, locale)}</p>
      <header className="min-w-0">
        <Heading className="text-h1 font-semibold text-ink">{t('allItems.title')}</Heading>
      </header>
      <div className="min-w-0 rounded-2xl border border-line bg-raised p-4 sm:p-5">
        <AllItemsSearch draft={draft} page={page} actions={actions} searchRef={searchRef} />
        {/* On phones the single card needs no divider between the search and the filters; removing it keeps the results higher. */}
        <div className="mt-4 border-t border-line pt-4 max-sm:mt-3 max-sm:border-t-0 max-sm:pt-0">
          <AllItemsFilters draft={draft} summary={page.summary} facets={facets} userId={userId}
            actions={actions} showClear={view !== 'noMatch'} />
        </div>
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
