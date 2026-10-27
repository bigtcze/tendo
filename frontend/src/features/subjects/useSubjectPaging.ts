import type { Dispatch, MutableRefObject, SetStateAction } from 'react';
import { listSubjects } from './subjectsApi';
import type { ListState } from './subjectsModel';

export function useSubjectPaging({ householdId, showArchived, list, cursorStack, currentCursor, generation, pagingFocus,
  pageLoad, setLoadingMore, setActionFailed, setCursorStack, setCurrentCursor, setList, onSignedOut }: {
  householdId: string; showArchived: boolean; list: ListState; cursorStack: string[]; currentCursor: string | undefined;
  generation: MutableRefObject<number>; pagingFocus: MutableRefObject<HTMLElement | null>; pageLoad: MutableRefObject<number>;
  setLoadingMore: Dispatch<SetStateAction<boolean>>; setActionFailed: Dispatch<SetStateAction<boolean>>;
  setCursorStack: Dispatch<SetStateAction<string[]>>; setCurrentCursor: Dispatch<SetStateAction<string | undefined>>;
  setList: Dispatch<SetStateAction<ListState>>; onSignedOut: () => void;
}) {
  async function goToPage(cursor: string | undefined, stack: string[], activated: HTMLElement, busy: MutableRefObject<boolean>, interactionLocked: boolean) {
    if (busy.current || list.kind !== 'ready' || interactionLocked) return;
    const started = ++generation.current;
    pagingFocus.current = activated;
    setLoadingMore(true);
    setActionFailed(false);
    const result = await listSubjects(householdId, { archived: showArchived, cursor });
    if (started !== generation.current) return;
    setLoadingMore(false);
    if (result.kind === 'unauthenticated') { pagingFocus.current = null; onSignedOut(); return; }
    if (result.kind === 'ok') {
      pageLoad.current += 1;
      setCursorStack(stack);
      setCurrentCursor(cursor);
      setList({ kind: 'ready', items: result.items, nextCursor: result.nextCursor });
    } else { pagingFocus.current = null; setActionFailed(true); }
  }
  function nextPage(event: React.MouseEvent<HTMLButtonElement>, busy: MutableRefObject<boolean>, interactionLocked: boolean) {
    if (list.kind !== 'ready' || !list.nextCursor) return;
    void goToPage(list.nextCursor, [...cursorStack, currentCursor ?? ''], event.currentTarget, busy, interactionLocked);
  }
  function previousPage(event: React.MouseEvent<HTMLButtonElement>, busy: MutableRefObject<boolean>, interactionLocked: boolean) {
    const stack = [...cursorStack];
    const cursor = stack.pop() ?? '';
    void goToPage(cursor || undefined, stack, event.currentTarget, busy, interactionLocked);
  }
  return { nextPage, previousPage };
}
