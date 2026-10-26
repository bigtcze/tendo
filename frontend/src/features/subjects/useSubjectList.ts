import { useCallback, useEffect, useRef, useState } from 'react';
import { listSubjects, type Subject } from './subjectsApi';
import { replaceById, type ListState } from './subjectsModel';
import { useSubjectPaging } from './useSubjectPaging';

export function useSubjectList(householdId: string, onSignedOut: () => void) {
  const [showArchived, setShowArchived] = useState(false);
  const [list, setList] = useState<ListState>({ kind: 'loading' });
  const [attempt, setAttempt] = useState(0);
  const [loadingMore, setLoadingMore] = useState(false);
  const [mutationPending, setMutationPending] = useState(false);
  const [cursorStack, setCursorStack] = useState<string[]>([]);
  const [currentCursor, setCurrentCursor] = useState<string | undefined>();
  const [actionFailed, setActionFailed] = useState(false);
  const pageLoad = useRef(0);
  const pagingFocus = useRef<HTMLElement | null>(null);
  const generation = useRef(0);
  const focusKey = useRef<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    const token = ++generation.current;
    listSubjects(householdId, { archived: showArchived }).then((result) => {
      if (cancelled || token !== generation.current) return;
      if (result.kind === 'unauthenticated') onSignedOut();
      else if (result.kind === 'ok') {
        pageLoad.current += 1;
        setList({ kind: 'ready', items: result.items, nextCursor: result.nextCursor });
      }
      else if (result.kind === 'noHousehold') setList({ kind: 'noHousehold' });
      else setList({ kind: 'error' });
    });
    return () => { cancelled = true; };
  }, [householdId, showArchived, attempt, onSignedOut]);

  const reload = useCallback(() => {
    generation.current += 1;
    setLoadingMore(false);
    setMutationPending(false);
    pagingFocus.current = null;
    setCursorStack([]);
    setCurrentCursor(undefined);
    focusKey.current = null;
    setAttempt((n) => n + 1);
  }, []);

  function retryLoad() {
    setLoadingMore(false);
    setCursorStack([]);
    setCurrentCursor(undefined);
    setList({ kind: 'loading' });
    reload();
  }

  function updateRow(subject: Subject) {
    setList((current) => (current.kind === 'ready' ? { ...current, items: replaceById(current.items, subject) } : current));
  }

  async function reloadPage(cursor: string | undefined, stack: string[], focusId: string) {
    const token = ++generation.current;
    const result = await listSubjects(householdId, { archived: showArchived, cursor });
    if (token !== generation.current) return;
    if (result.kind === 'unauthenticated') { onSignedOut(); return; }
    if (result.kind === 'ok') {
      pageLoad.current += 1;
      const target = result.items.find((s) => s.id === focusId) ?? result.items[0];
      focusKey.current = target ? `row:${target.id}` : null;
      setCursorStack(stack);
      setCurrentCursor(cursor);
      setList({ kind: 'ready', items: result.items, nextCursor: result.nextCursor });
    } else setActionFailed(true);
  }

  const paging = useSubjectPaging({ householdId, showArchived, list, cursorStack, currentCursor, generation,
    pagingFocus, pageLoad, setLoadingMore, setActionFailed, setCursorStack, setCurrentCursor, setList, onSignedOut });
  return {
    showArchived, setShowArchived, list, setList, loadingMore, setLoadingMore, mutationPending, setMutationPending,
    cursorStack, setCursorStack, currentCursor, setCurrentCursor, pageLoad, pagingFocusRef: pagingFocus, generationRef: generation, focusKeyRef: focusKey,
    reload, retryLoad, updateRow, reloadPage, actionFailed, setActionFailed, ...paging,
  };
}
