import type { ReactNode, RefObject } from 'react';
import { useId, useState } from 'react';
import { Button } from '../../components/ui/button';
import { useI18n } from '../../i18n';
import type { AllItemsStatus, ItemSummary } from './itemsApi';
import type { Draft, PageState } from './allItemsPageState';
import { countFor, statusKeys, statuses, type Context } from './allItemsUrl';
import type { AllItemsFacets } from './useAllItemsFacets';
import type { AllItemsActions } from './useAllItems';

const focusRing = 'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent';
const labelClass = 'block min-w-0 text-label font-semibold text-ink';
const controlClass = `mt-1.5 block min-h-11 w-full min-w-0 rounded-xl border border-field bg-raised px-3 text-ink ${focusRing}`;

export function AllItemsSearch({ draft, page, actions, searchRef }: {
  draft: Draft; page: PageState; actions: AllItemsActions; searchRef: RefObject<HTMLInputElement | null>;
}) {
  const { t } = useI18n();
  return <form role="search" aria-label={t('allItems.search')} onSubmit={actions.submit} className="min-w-0">
    <label htmlFor="all-items-search" className={labelClass}>{t('allItems.search')}</label>
    <div className="mt-1.5 flex min-w-0 gap-2">
      <input ref={searchRef} id="all-items-search" type="search" value={draft.search}
        onChange={(event) => actions.editSearch(event.target.value)}
        aria-invalid={page.searchError} aria-describedby={page.searchError ? 'all-items-search-error' : undefined}
        className={`min-h-11 min-w-0 flex-1 rounded-xl border border-field bg-raised px-3 text-control text-ink ${focusRing}`} />
      <Button type="submit" className="min-h-11 shrink-0 px-4 sm:px-6">{t('allItems.searchButton')}</Button>
    </div>
    {page.searchError
      ? <p key={page.searchErrorAttempt} id="all-items-search-error" role="alert" className="mt-1.5 text-small text-danger">{t('allItems.searchTooLong')}</p>
      : null}
  </form>;
}

type FilterSelectProps = { label: string; value: string; onChange: (value: string) => void; children: ReactNode };

function FilterSelect({ label, value, onChange, children }: FilterSelectProps) {
  return <label className={labelClass}>
    {label}
    <select value={value} onChange={(event) => onChange(event.target.value)} className={controlClass}>{children}</select>
  </label>;
}

function statusOption(id: AllItemsStatus, label: string, summary: ItemSummary | null) {
  return <option key={id} value={id}>{label}{summary ? ` (${countFor(id, summary)})` : ''}</option>;
}

/** Phone-only disclosure; the panel is always visible from `lg`, where the toggle is hidden. */
function FiltersDisclosure({ expanded, activeCount, panelId, t, onToggle }: {
  expanded: boolean; activeCount: number; panelId: string;
  t: (key: 'allItems.filters') => string; onToggle: () => void;
}) {
  return <div className="lg:hidden">
    <Button type="button" variant="quiet" aria-expanded={expanded} aria-controls={panelId}
      className="min-h-11 justify-start rounded-xl border border-field bg-raised px-4 text-ink"
      data-all-items-filter-disclosure onClick={onToggle}>
      {t('allItems.filters')}{activeCount ? ` (${activeCount})` : ''}
      <svg aria-hidden="true" focusable="false" viewBox="0 0 24 24" fill="none" stroke="currentColor"
        strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round" data-all-items-filter-chevron
        className={`ml-1 size-4 shrink-0 transition-transform motion-reduce:transition-none ${expanded ? 'rotate-180' : ''}`}>
        <path d="m6 9 6 6 6-6" />
      </svg>
    </Button>
  </div>;
}

export function AllItemsFilters({ draft, summary, facets, userId, actions, showClear }: {
  draft: Draft; summary: ItemSummary | null; facets: AllItemsFacets; userId: string; actions: AllItemsActions; showClear: boolean;
}) {
  const { t } = useI18n();
  const [expanded, setExpanded] = useState(false);
  const panelId = useId();
  const { subjects, members } = facets;
  const knownSubject = subjects.values.some((subject) => subject.id === draft.subjectId);
  const knownMember = draft.responsible === 'unassigned' || members.values.some((member) => member.userId === draft.responsible);
  const select = <K extends keyof Draft>(key: K, toContext: (value: string) => Partial<Context>) => (value: string) => {
    actions.setDraft({ [key]: value } as Partial<Draft>);
    actions.update(toContext(value));
  };
  const activeCount = Number(Boolean(draft.subjectId)) + Number(Boolean(draft.responsible)) + Number(draft.recurring !== '');
  const hasActiveFilters = activeCount > 0 || Boolean(draft.search.trim()) || draft.status !== 'active';
  const clear = hasActiveFilters && showClear
    ? <Button type="button" data-all-items-clear variant="quiet" size="small" onClick={actions.clearFilters}>{t('allItems.clear')}</Button>
    : null;
  const fields = <>
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
  </>;
  // Phones wrap like any flex row, so the status, the disclosure and the reset can never overlap: each
  // control drops to the next line only when its content does not fit. From lg the same children form one
  // four-column band with the reset as its last cell.
  return <div className="flex min-w-0 flex-wrap items-end gap-x-3 gap-y-2 lg:grid lg:grid-cols-[repeat(4,minmax(0,1fr))_auto]">
    <label className={`${labelClass} min-w-0 grow basis-32`}>
      {t('allItems.status.label')}
      <select value={draft.status} onChange={(event) => select('status', (value) => ({ status: value as AllItemsStatus }))(event.target.value)}
        className={controlClass}>
        {statuses.map((id) => statusOption(id, t(statusKeys[id]), summary))}
      </select>
    </label>
    <FiltersDisclosure expanded={expanded} activeCount={activeCount} panelId={panelId} t={t}
      onToggle={() => setExpanded((open) => !open)} />
    {/* The reset follows the disclosure on phones and closes the desktop band as its last cell. */}
    {clear ? <div className="ml-auto lg:col-start-5 lg:row-start-1">{clear}</div> : null}
    {/* From lg the panel flattens into the same row, so Show, the refinements and the reset scan as one band. */}
    <div id={panelId} className={`${expanded ? 'w-full grid gap-3 sm:grid-cols-2' : 'hidden'} lg:contents`}>
      {fields}
    </div>
  </div>;
}
