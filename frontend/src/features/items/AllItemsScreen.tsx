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

/** Eyebrow, screen heading and supporting sentence, matching the Home and settings header band. */
function AllItemsHeader() {
  const { t } = useI18n();
  return <header className="min-w-0">
    <p className="text-[0.68rem] font-semibold uppercase tracking-[0.18em] text-muted">{t('allItems.eyebrow')}</p>
    <Heading className="mt-2 text-h1 leading-tight text-ink">{t('allItems.title')}</Heading>
    <p className="mt-2 max-w-prose text-base text-muted">{t('allItems.subtitle')}</p>
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
    <section className="settle min-w-0 space-y-6">
      <p role="status" aria-live="polite" aria-atomic="true" className="sr-only">{outcomeText(view, context, page, t, locale)}</p>
      <AllItemsHeader />
      <div className="min-w-0 rounded-2xl border border-line bg-raised p-4 sm:p-5">
        <AllItemsSearch draft={draft} page={page} actions={actions} searchRef={searchRef} />
        <div className="mt-4 border-t border-line pt-4">
          <AllItemsFilters draft={draft} summary={page.summary} facets={facets} userId={userId}
            actions={actions} showClear={view !== 'noMatch'} />
        </div>
      </div>
      <div className="min-w-0 space-y-4">
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
