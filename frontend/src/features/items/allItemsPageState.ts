import type { AllItemsStatus, Item, ItemSummary } from './itemsApi';
import type { Context } from './allItemsUrl';

/** Loaded rows plus request status. `loadedQuery` names the context whose rows are in `items`; rows for any other context are stale. */
export type PageState = {
  items: Item[];
  summary: ItemSummary | null;
  nextCursor: string | null;
  loading: boolean;
  loadedQuery: string | null;
  error: boolean;
  searchError: boolean;
  // Remounting the alert on each rejected submit makes a repeated identical error announce again.
  searchErrorAttempt: number;
  notice: boolean;
};

export type PageAction =
  | { type: 'start' }
  | { type: 'loaded'; query: string; summary: ItemSummary; items: Item[]; nextCursor: string | null }
  | { type: 'failed' | 'invalidQuery'; query: string }
  | { type: 'facetHouseholdMissing' }
  | { type: 'notice'; shown: boolean }
  | { type: 'searchError'; shown: boolean; announce?: boolean };

export const initialPage: PageState = {
  items: [], summary: null, nextCursor: null, loading: true, loadedQuery: null, error: false, searchError: false, searchErrorAttempt: 0, notice: false,
};

export function pageReducer(state: PageState, action: PageAction): PageState {
  switch (action.type) {
    case 'start': return { ...state, loading: true, error: false, searchError: false };
    case 'loaded': return {
      ...state, summary: action.summary, items: action.items, nextCursor: action.nextCursor, loadedQuery: action.query, error: false, loading: false,
    };
    case 'failed': return { ...state, error: true, loading: false, loadedQuery: action.query };
    case 'invalidQuery': return { ...state, searchError: true, loading: false, loadedQuery: action.query };
    case 'facetHouseholdMissing': return { ...state, error: true };
    case 'notice': return state.notice === action.shown ? state : { ...state, notice: action.shown };
    case 'searchError':
      if (state.searchError === action.shown && !action.announce) return state;
      return { ...state, searchError: action.shown, searchErrorAttempt: state.searchErrorAttempt + (action.announce ? 1 : 0) };
  }
}

/** Unsubmitted form values; they follow the context on load, popstate and Clear. */
export type Draft = { search: string; status: AllItemsStatus; subjectId: string; responsible: string; recurring: string };

export function draftFrom(context: Context): Draft {
  return {
    search: context.q ?? '',
    status: context.status,
    subjectId: context.subjectId ?? '',
    responsible: context.responsibleUserId ?? '',
    recurring: context.recurring === undefined ? '' : String(context.recurring),
  };
}
