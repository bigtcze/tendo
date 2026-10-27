import { useCallback, useEffect, useRef, useState } from 'react';
import type { MessageKey } from '../../i18n';
import { getSubject } from '../subjects/subjectsApi';
import type { Member } from '../members/membersApi';
import { getItem, type Item } from './itemsApi';
import { collectMembers } from './collectMembers';
import type { Editing } from './itemEditModel';
import type { ItemDetailHistoryState } from './useItemDetailHistory';
import type { LoadGeneration } from './useLoadGeneration';

export type DetailLoadState = 'loading' | 'error' | 'gone' | 'ready';
const ITEM_ID = /^[0-9a-f-]{36}$/i;

type Options = {
  householdId: string; itemId: string; onSignedOut: () => void;
  loads: LoadGeneration; history: Pick<ItemDetailHistoryState, 'load' | 'reset'>;
};

/**
 * Item detail state: the item, its subject label, members for the assignee line, notices and the
 * edit session. `load(token)` reloads everything for one generation; the mount effect starts it.
 */
export function useItemDetail({ householdId, itemId, onSignedOut, loads, history }: Options) {
  const [state, setState] = useState<DetailLoadState>('loading');
  const [item, setItem] = useState<Item | null>(null);
  const [subjectName, setSubjectName] = useState('');
  const [subjectOverride, setSubjectOverride] = useState<string | null>(null);
  const [members, setMembers] = useState<Member[]>([]);
  const [notice, setNotice] = useState<MessageKey | null>(null);
  const [undoArchive, setUndoArchive] = useState(false);
  const [editing, setEditing] = useState<Editing | null>(null);
  const [editLoading, setEditLoading] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const focusAfterMutationRef = useRef(false);
  const { current, generationRef } = loads;
  const { load: loadHistory, reset: resetHistory } = history;

  const loadMembers = useCallback(async (token: number) => {
    const collected = await collectMembers(householdId, () => current(token));
    if (collected.kind === 'stale' || !current(token)) return;
    if (collected.kind === 'unauthenticated') { onSignedOut(); return; }
    setMembers(collected.kind === 'ok' ? collected.members : []);
  }, [householdId, onSignedOut, current]);

  const load = useCallback(async (token: number, preserveArchiveNotice = false) => {
    if (!ITEM_ID.test(itemId)) { if (current(token)) setState('gone'); return; }
    resetHistory();
    setState('loading'); setEditing(null); setEditLoading(false);
    if (!preserveArchiveNotice) { setNotice(null); setUndoArchive(false); }
    const result = await getItem(householdId, itemId);
    if (!current(token)) return;
    if (result.kind === 'unauthenticated') { onSignedOut(); return; }
    if (result.kind === 'notFound') { setState('gone'); return; }
    if (result.kind !== 'ok') { setState('error'); return; }
    setItem(result.item); setState('ready'); setSubjectOverride(null); setSubjectName(''); setMembers([]);
    const subject = await getSubject(householdId, result.item.subjectId);
    if (!current(token)) return;
    if (subject.kind === 'unauthenticated') { onSignedOut(); return; }
    setSubjectName(subject.kind === 'ok' ? subject.subject.name : '');
    void loadMembers(token);
    void loadHistory(token);
  }, [householdId, itemId, loadHistory, loadMembers, onSignedOut, current, resetHistory]);

  useEffect(() => {
    const token = ++generationRef.current;
    void Promise.resolve().then(() => load(token));
    return () => { if (generationRef.current === token) generationRef.current = token + 1; };
  }, [load, attempt, generationRef]);

  /** Shows a subject name that wins over the one loaded with the item, until the next load. */
  function showSubject(name: string) { setSubjectName(name); setSubjectOverride(name); }
  /** A notice that takes focus once it renders. */
  function announce(next: MessageKey) { setNotice(next); focusAfterMutationRef.current = true; }

  return {
    state, item, subjectName, subjectOverride, members, notice, undoArchive, editing, editLoading, focusAfterMutationRef,
    load, retry: () => setAttempt((n) => n + 1), setState, setItem, setNotice, announce, setUndoArchive, setEditing, setEditLoading, showSubject,
  };
}

export type ItemDetailState = ReturnType<typeof useItemDetail>;
