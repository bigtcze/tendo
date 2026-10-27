import { useRef, useState } from 'react';
import { useSubjectMembers } from './useSubjectMembers';
import { useSubjectList } from './useSubjectList';
import { useSubjectsFocus, useUnmountGeneration } from './useSubjectsFocus';
import { AccountActions } from '../../app/AccountActions';
import { Layout } from '../../app/Layout';
import { SubjectsScreenContent } from './SubjectsScreenContent';
import { yourEntryText, type Editing, type Notice } from './subjectsModel';
import { useI18n } from '../../i18n';
import { getPendingSubjectCreate, type PendingSubjectCreate } from './pendingSubjectCreate';
import { useSubjectCreate } from './useSubjectCreate';
import { useSubjectActions } from './useSubjectActions';
import { useSubjectSave } from './useSubjectSave';

export function SubjectsScreen({ userId, householdId, login, onSignedOut }: {
  userId: string; householdId: string; login: string; onSignedOut: () => void;
}) {
  const { t } = useI18n();
  const listData = useSubjectList(householdId, onSignedOut);
  const {
    showArchived, list: listState, loadingMore, mutationPending, cursorStack, currentCursor, pageLoad,
    pagingFocusRef, generationRef, focusKeyRef, reload, retryLoad, updateRow, reloadPage,
    actionFailed, nextPage, previousPage,
    setShowArchived, setList, setLoadingMore, setMutationPending, setCursorStack, setCurrentCursor, setActionFailed,
  } = listData;
  const [pendingCreate, setPendingCreate] = useState<PendingSubjectCreate | undefined>(() => getPendingSubjectCreate(userId, householdId));
  const [createUncertain, setCreateUncertain] = useState(() => Boolean(getPendingSubjectCreate(userId, householdId)));
  const [adding, setAdding] = useState(() => Boolean(getPendingSubjectCreate(userId, householdId)));
  const [editing, setEditing] = useState<Editing | null>(null);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [notice, setNotice] = useState<Notice | null>();
  const archivePage = useRef<number | null>(null);
  const addForm = useRef(0);
  const busy = useRef(false);
  const noticeRef = useRef<HTMLDivElement>(null);

  const memberData = useSubjectMembers(householdId, adding, editing, onSignedOut);
  const { members, membersLoaded } = memberData;
  useUnmountGeneration(generationRef);
  useSubjectsFocus(notice, noticeRef, loadingMore, pagingFocusRef, listState, focusKeyRef);

  const switchView = (archived: boolean) => switchSubjectView(archived, {
    generationRef, setLoadingMore, setMutationPending, pagingFocusRef, setCursorStack, setCurrentCursor, focusKeyRef,
    setShowArchived, setList, setActionFailed,
  }, setAdding, setEditing, setNotice);
  const create = useSubjectCreate({
    userId, householdId, generation: generationRef, addForm, pendingCreate, setPendingCreate, loadingMore,
    mutationPending, busy, setCreateUncertain,
    setNotice, setAdding, setMembers: memberData.resetMembers, setEditing, setActionFailed,
    setLoadingMore, pagingFocus: pagingFocusRef, pageLoad,
    setCursorStack, setCurrentCursor, setList, onSignedOut,
  });
  const cancelAdding = () => { focusKeyRef.current = 'add'; create.cancelAdding(); };
  const actions = useSubjectActions({
    householdId, showArchived, loadingMore, mutationPending, adding, editing, busy, setBusyId, setMutationPending, setActionFailed,
    generation: generationRef, archivePage, pageLoad, setNotice, setEditing, setAdding,
    resetMembers: memberData.resetMembers, updateRow, reload, reloadPage, currentCursor, cursorStack, focusKey: focusKeyRef,
    setList, noticeRef, onSignedOut,
  });
  const save = useSubjectSave({
    editing, generation: generationRef, householdId, showArchived, closeEditing: actions.closeEditing,
    updateRow, setEditing, changedMeanwhile: actions.changedMeanwhile, gone: actions.gone, onSignedOut,
  }).save;
  const interactionLocked = loadingMore || mutationPending || adding || editing !== null || busyId !== null;
  const items = listState.kind === 'ready' ? listState.items : [];
  const empty = listState.kind === 'ready' && items.length === 0;
  const yourEntry = yourEntryText(editing, members, (key, values) => t(key, values));

  return <Layout actions={<AccountActions login={login} onSignedOut={onSignedOut} />}>
    <SubjectsScreenContent
      showArchived={showArchived} list={listState} notice={notice} noticeRef={noticeRef} interactionLocked={interactionLocked}
      actionFailed={actionFailed} retryLoad={retryLoad} adding={adding} empty={empty} items={items}
      addDisabled={loadingMore || mutationPending || busyId !== null}
      cursorStack={cursorStack} members={members} membersLoaded={membersLoaded} editing={editing}
      createUncertain={createUncertain} pendingCreate={pendingCreate} t={t} yourEntry={yourEntry}
      startAdding={create.startAdding} cancelAdding={cancelAdding} add={create.add} startEditing={actions.startEditing}
      changeArchived={actions.changeArchived} undoArchive={actions.undoArchive} save={save} closeEditing={actions.closeEditing}
      previousPage={(event) => previousPage(event, busy, interactionLocked)}
      nextPage={(event) => nextPage(event, busy, interactionLocked)}
      switchView={() => switchView(!showArchived)}
    />
  </Layout>;
}

function switchSubjectView(
  archived: boolean,
  list: {
    generationRef: React.MutableRefObject<number>; setLoadingMore: (value: boolean) => void;
    setMutationPending: (value: boolean) => void; pagingFocusRef: React.MutableRefObject<HTMLElement | null>;
    setCursorStack: (value: string[]) => void; setCurrentCursor: (value: string | undefined) => void;
    focusKeyRef: React.MutableRefObject<string | null>; setShowArchived: (value: boolean) => void;
    setList: (value: import('./subjectsModel').ListState) => void; setActionFailed: (value: boolean) => void;
  },
  setAdding: (value: boolean) => void,
  setEditing: (value: Editing | null) => void,
  setNotice: (value: Notice | null) => void,
) {
  list.generationRef.current += 1;
  list.setLoadingMore(false);
  list.setMutationPending(false);
  list.pagingFocusRef.current = null;
  list.setCursorStack([]);
  list.setCurrentCursor(undefined);
  list.focusKeyRef.current = null;
  list.setShowArchived(archived);
  list.setList({ kind: 'loading' });
  setAdding(false);
  setEditing(null);
  setNotice(null);
  list.setActionFailed(false);
}
