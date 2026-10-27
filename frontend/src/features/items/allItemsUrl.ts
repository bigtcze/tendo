import type { MessageKey } from '../../i18n';
import type { AllItemsFilters, AllItemsStatus, ItemSummary } from './itemsApi';

/** The All items view state that lives in the URL: filters, status and the current page cursor. */
export type Context = AllItemsFilters & { status: AllItemsStatus; cursor?: string };

export const statuses: AllItemsStatus[] = ['active', 'needs', 'inProgress', 'waiting', 'upcoming', 'paused', 'done', 'archived'];
export const statusKeys: Record<AllItemsStatus, MessageKey> = {
  active: 'allItems.status.active', needs: 'items.group.needs', inProgress: 'items.group.inProgress',
  waiting: 'items.group.waiting', upcoming: 'items.group.upcoming', paused: 'items.group.paused',
  done: 'itemDetail.state.done', archived: 'allItems.status.archived',
};
export const clearedFilters: Partial<Context> = { q: undefined, status: 'active', subjectId: undefined, responsibleUserId: undefined, recurring: undefined };

export function buildQuery(context: Context): string {
  const query = new URLSearchParams();
  if (context.q) query.set('q', context.q);
  if (context.status !== 'active') query.set('status', context.status);
  if (context.subjectId) query.set('subject', context.subjectId);
  if (context.responsibleUserId) query.set('responsible', context.responsibleUserId);
  if (context.recurring !== undefined) query.set('recurring', String(context.recurring));
  if (context.cursor) query.set('cursor', context.cursor);
  return query.toString();
}

function parseRecurring(raw: string | null): boolean | undefined {
  if (raw === 'true') return true;
  return raw === 'false' ? false : undefined;
}

/** Replaces the current history entry with the context's canonical URL and the given previous-page cursor stack. */
export function writeLocation(context: Context, cursors: string[]): void {
  const query = buildQuery(context);
  window.history.replaceState({ ...(window.history.state ?? {}), allItemsCursors: cursors }, '', `/items${query ? `?${query}` : ''}`);
}

/** Reads the context from the URL and normalizes the URL in place when it was not canonical. */
export function readContext(): Context {
  const query = new URLSearchParams(window.location.search);
  const rawStatus = query.get('status');
  const context: Context = {
    q: query.get('q')?.trim() || undefined,
    status: statuses.includes(rawStatus as AllItemsStatus) ? rawStatus as AllItemsStatus : 'active',
    subjectId: query.get('subject') || undefined,
    responsibleUserId: query.get('responsible') || undefined,
    recurring: parseRecurring(query.get('recurring')),
    cursor: query.get('cursor') || undefined,
  };
  const normalized = buildQuery(context);
  if (`${window.location.pathname}${window.location.search}` !== `/items${normalized ? `?${normalized}` : ''}`) writeLocation(context, []);
  return context;
}

export function cursorStack(): string[] {
  const state = window.history.state?.allItemsCursors;
  return Array.isArray(state) ? state.filter((cursor: unknown): cursor is string => typeof cursor === 'string') : [];
}

export function activeFilters(context: Context): AllItemsFilters {
  return {
    ...(context.q ? { q: context.q } : {}),
    ...(context.subjectId ? { subjectId: context.subjectId } : {}),
    ...(context.responsibleUserId ? { responsibleUserId: context.responsibleUserId } : {}),
    ...(context.recurring !== undefined ? { recurring: context.recurring } : {}),
  };
}

export function hasFilters(context: Context): boolean {
  return Boolean(context.q || context.subjectId || context.responsibleUserId || context.recurring !== undefined);
}

export function countKey(count: number, locale: string): MessageKey {
  const category = new Intl.PluralRules(locale).select(count);
  const supported: Record<string, string[]> = { en: ['one', 'other'], cs: ['one', 'few', 'other'] };
  return `items.group.count.${supported[locale]?.includes(category) ? category : 'other'}` as MessageKey;
}

const statusCounts: Record<AllItemsStatus, (summary: ItemSummary) => number> = {
  active: (s) => s.needsAttention + s.inProgress + s.waiting + s.upcoming + s.paused,
  needs: (s) => s.needsAttention,
  inProgress: (s) => s.inProgress,
  waiting: (s) => s.waiting,
  upcoming: (s) => s.upcoming,
  paused: (s) => s.paused,
  done: (s) => s.done,
  archived: (s) => s.archived,
};

export function countFor(status: AllItemsStatus, summary: ItemSummary): number {
  return statusCounts[status](summary);
}
