import type { ReactNode, RefObject } from 'react';
import { Button } from '../../components/ui/button';
import { useI18n } from '../../i18n';
import type { AllItemsStatus, ItemSummary } from './itemsApi';
import type { Draft, PageState } from './allItemsPageState';
import { countFor, statusKeys, statuses, type Context } from './allItemsUrl';
import type { AllItemsFacets } from './useAllItemsFacets';
import type { AllItemsActions } from './useAllItems';

const focusRing = 'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent';
const selectClass = `mt-1 block min-h-11 w-full min-w-0 rounded-xl border border-field bg-raised px-3 ${focusRing}`;

export function AllItemsSearch({ draft, page, actions, searchRef }: {
  draft: Draft; page: PageState; actions: AllItemsActions; searchRef: RefObject<HTMLInputElement | null>;
}) {
  const { t } = useI18n();
  return <form role="search" aria-label={t('allItems.search')} onSubmit={actions.submit} className="min-w-0">
    <label htmlFor="all-items-search" className="mb-1 block text-label font-medium">{t('allItems.search')}</label>
    <div className="flex min-w-0 gap-2">
      <input ref={searchRef} id="all-items-search" type="search" value={draft.search}
        onChange={(event) => actions.editSearch(event.target.value)}
        aria-invalid={page.searchError} aria-describedby={page.searchError ? 'all-items-search-error' : undefined}
        className={`min-h-12 min-w-0 flex-1 rounded-xl border border-field bg-raised px-3 text-control ${focusRing}`} />
      <Button type="submit">{t('allItems.searchButton')}</Button>
    </div>
    {page.searchError
      ? <p key={page.searchErrorAttempt} id="all-items-search-error" role="alert" className="mt-1 text-small text-danger">{t('allItems.searchTooLong')}</p>
      : null}
  </form>;
}

type FilterSelectProps = { label: string; value: string; onChange: (value: string) => void; children: ReactNode };

function FilterSelect({ label, value, onChange, children }: FilterSelectProps) {
  return <label className="text-sm">{label}
    <select value={value} onChange={(event) => onChange(event.target.value)} className={selectClass}>{children}</select>
  </label>;
}

function statusOption(id: AllItemsStatus, label: string, summary: ItemSummary | null) {
  return <option key={id} value={id}>{label}{summary ? ` (${countFor(id, summary)})` : ''}</option>;
}

export function AllItemsFilters({ draft, summary, facets, userId, actions, showClear }: {
  draft: Draft; summary: ItemSummary | null; facets: AllItemsFacets; userId: string; actions: AllItemsActions; showClear: boolean;
}) {
  const { t } = useI18n();
  const { subjects, members } = facets;
  const knownSubject = subjects.values.some((subject) => subject.id === draft.subjectId);
  const knownMember = draft.responsible === 'unassigned' || members.values.some((member) => member.userId === draft.responsible);
  const select = <K extends keyof Draft>(key: K, toContext: (value: string) => Partial<Context>) => (value: string) => {
    actions.setDraft({ [key]: value } as Partial<Draft>);
    actions.update(toContext(value));
  };
  const activeCount = Number(draft.status !== 'active') + Number(Boolean(draft.subjectId))
    + Number(Boolean(draft.responsible)) + Number(draft.recurring !== '');
  return <div className="space-y-3">
    <FilterSelect label={t('allItems.status.label')} value={draft.status}
      onChange={select('status', (value) => ({ status: value as AllItemsStatus }))}>
      {statuses.map((id) => statusOption(id, t(statusKeys[id]), summary))}
    </FilterSelect>
    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
      {subjects.state === 'ready' ? <FilterSelect label={t('items.subject')} value={draft.subjectId}
        onChange={select('subjectId', (value) => ({ subjectId: value || undefined }))}>
        <option value="">{t('allItems.any')}</option>
        {subjects.values.map((subject) => <option key={subject.id} value={subject.id}>
          {subject.name}{subject.archived ? ` ${t('allItems.archivedSuffix')}` : ''}
        </option>)}
        {draft.subjectId && !knownSubject ? <option value={draft.subjectId}>{t('allItems.unknown')}</option> : null}
      </FilterSelect> : null}
      {members.state === 'ready' ? <FilterSelect label={t('items.responsible.label')} value={draft.responsible}
        onChange={select('responsible', (value) => ({ responsibleUserId: value || undefined }))}>
        <option value="">{t('allItems.anyone')}</option><option value="unassigned">{t('items.responsible.none')}</option>
        {members.values.map((member) => <option key={member.userId} value={member.userId}>
          {member.login}{member.userId === userId ? ` (${t('allItems.you')})` : ''}
        </option>)}
        {draft.responsible && !knownMember ? <option value={draft.responsible}>{t('allItems.unknown')}</option> : null}
      </FilterSelect> : null}
      <FilterSelect label={t('allItems.repeat')} value={draft.recurring}
        onChange={select('recurring', (value) => ({ recurring: value === '' ? undefined : value === 'true' }))}>
        <option value="">{t('allItems.any')}</option>
        <option value="true">{t('allItems.repeating')}</option>
        <option value="false">{t('allItems.oneOff')}</option>
      </FilterSelect>
    </div>
    {activeCount > 0 && showClear ? <Button type="button" variant="quiet" onClick={actions.clearFilters}>{t('allItems.clear')}</Button> : null}
  </div>;
}
