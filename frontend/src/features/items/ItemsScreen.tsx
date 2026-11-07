import { useMemo, useRef, useState, type RefObject } from 'react';
import { Button } from '../../components/ui/button';
import { Dialog } from '../../components/ui/dialog';
import { useI18n } from '../../i18n';
import { useSubjectLabels } from './useSubjectLabels';
import type { ItemGroup } from './itemsApi';
import { useAddItem, useAddReturnFocus } from './useAddItem';
import { AddItemCatalogueStatus, AddItemPanel } from './AddItemPanel';
import { groups, HOME_GROUP_ROW_CAP } from './homeGroups';
import type { Notice } from './HomeOverview';
import { HomeGroupList } from './HomeGroupList';
import { HomeOverviewNav, ItemsNoticeArea, SubjectLabelRetry } from './HomeOverview';
import { HomeEmptyState, HomeLoadState } from './HomeStates';
import { useHomeItems } from './useHomeItems';
import { useHomeMembers } from './useHomeMembers';
import { useMutationLock } from './useMutationLock';
import { useMountedRef } from './useMountedRef';
import { useCompletionActions } from './useCompletionActions';
import { useCompletionNoticeFocus, useUndoFocus } from './useCompletionFocus';
import { useLabelRetryFocus } from './useLabelRetryFocus';
import { useShowMoreFocus } from './useShowMoreFocus';

export { HOME_GROUP_ROW_CAP };

type ItemsScreenProps = {
  userId: string;
  householdId: string;
  onOpenPeople: () => void;
  onOpenItem: (itemId: string) => void;
  onOpenAllItems: (status: ItemGroup) => void;
  onSignedOut: () => void;
};

/** Wires home data, subject labels, add-item and completion flows for the household home. */
function useItemsScreen({ userId, householdId, onSignedOut }: Pick<ItemsScreenProps, 'userId' | 'householdId' | 'onSignedOut'>) {
  const { t } = useI18n();
  const mounted = useMountedRef();
  const lock = useMutationLock();
  const { busy } = lock;
  const home = useHomeItems({ householdId, mounted, onSignedOut });
  const { groupData, summary, loading, error, refresh, sectionHeadingRefs } = home;
  const homeMembers = useHomeMembers(householdId, onSignedOut);
  const noticeRef = useRef<HTMLDivElement>(null);
  const addRef = useRef<HTMLButtonElement>(null);
  const addInitialFocusRef = useRef<HTMLElement | null>(null);
  const [notice, setNotice] = useState<Notice | null>(null);
  const visibleSubjectIDs = useMemo(
    () => [...new Set(Object.values(groupData).flatMap((group) => group.items.map((item) => item.subjectId)))],
    [groupData],
  );
  const labels = useSubjectLabels(userId, householdId, visibleSubjectIDs, onSignedOut);
  const add = useAddItem({
    userId, householdId, mounted, lock, refresh, invalidateHome: home.invalidate, dataHousehold: home.dataHousehold,
    labelNames: labels.names, setNotice, onSignedOut, addRef, memberLoadGenerationRef: home.memberLoadGeneration,
  });
  const { adding, createNotice, names, pendingCreate, createUncertain } = add;
  const recovering = createUncertain && !!pendingCreate;
  // Focus effects run in declaration order and several may settle in one commit; this order is behavior.
  // The catalogue-retry effect runs inside useAddItem, before every effect below.
  const labelRetry = useLabelRetryFocus(labels, groupData, sectionHeadingRefs);
  const completion = useCompletionActions({ userId, householdId, lock, refresh, groupData, onSignedOut, noticeRef, notice, setNotice });
  useCompletionNoticeFocus(completion.focusRefs, { notice, createNotice, noticeRef });
  useAddReturnFocus(adding, add.returnFocusRef, addRef);
  useUndoFocus(completion.focusRefs, { busy, sectionHeadingRefs, noticeRef, addRef });
  useShowMoreFocus(groupData, home.focusedMoreGroupRef, home.showMoreButtons, sectionHeadingRefs);

  function cancelAddAndFocus() {
    add.cancelAddForm();
    requestAnimationFrame(() => addRef.current?.focus());
  }

  function subjectName(subjectId: string) {
    const known = names.get(subjectId);
    if (known !== undefined) return known;
    const state = labels.stateFor(subjectId);
    return t(state === 'queued' || state === 'loading' ? 'items.subjectLabels.loading' : 'items.subject.unknown');
  }

  const showHome = !loading && summary;
  const emptyHome = showHome && !error && groups.every((group) => summary[group.count] === 0) && !adding;

  return {
    t, busy, home, homeMembers, noticeRef, addRef, addInitialFocusRef, notice, visibleSubjectIDs, labels, labelRetry, add, recovering, completion,
    cancelAddAndFocus, subjectName, emptyHome,
  };
}

/** Household home: grouped item overview with add-item and completion/undo flows. */
export function ItemsScreen(props: ItemsScreenProps) {
  const { userId, onOpenPeople, onOpenItem, onOpenAllItems } = props;
  const screen = useItemsScreen(props);
  const {
    t, busy, home, homeMembers, noticeRef, addRef, addInitialFocusRef, notice,
    visibleSubjectIDs, labels, labelRetry, add, recovering, completion,
  } = screen;
  const { cancelAddAndFocus, subjectName } = screen;
  const emptyHome = Boolean(screen.emptyHome);
  const homeReady = home.itemsRequestComplete && (!home.error || !!home.summary);
  const { groupData, summary, loading, error, refresh, sectionHeadingRefs } = home;
  const { adding, createNotice, names, pendingCreate, createUncertain, recoveryEditable } = add;

  return <div className="max-w-3xl min-w-0 space-y-6">
    <ItemsNoticeArea
      noticeRef={noticeRef}
      createNotice={createNotice}
      notice={notice}
      busy={busy}
      onUndo={() => void completion.undo()}
      onRetry={() => void completion.retryNotice()}
      onRetryFocus={completion.markRetryFocused}
    />
    <HomeLoadState loading={loading} error={error} onRetry={() => void refresh()} />
    {labels.loading && visibleSubjectIDs.some((id) => !names.has(id))
      ? <p role="status" className="sr-only">{t('items.subjectLabels.loading')}</p>
      : null}
    <SubjectLabelRetry hasFailures={labels.hasFailures} pending={labelRetry.pending} retryRef={labelRetry.retryRef} onRetry={labelRetry.retry} />
    {!loading && summary && !adding
      ? <HomeOverviewNav summary={summary} groupData={groupData} sectionHeadingRefs={sectionHeadingRefs} />
      : null}
    <HomeActions empty={emptyHome} ready={homeReady} adding={adding} addRef={addRef} onAdd={add.openAddForm} />
    <Dialog open={adding} onOpenChange={(open) => { if (!open) cancelAddAndFocus(); }} title={t('items.add.formLabel')} closeLabel={t('items.cancel')}
      busy={busy || recovering} sheet triggerRef={addRef} initialFocusRef={addInitialFocusRef}>
    <AddItemCatalogueStatus adding={adding} subjectLoading={add.subjectLoading} subjectState={add.subjectState}
      editingRecovery={recoveryEditable && !!pendingCreate} retryRef={add.catalogueRetryRef} onRetry={add.retryCatalogue} onCancel={cancelAddAndFocus} />
    <AddItemPanel
      adding={adding}
      busy={busy}
      subjectLoading={add.subjectLoading}
      subjectState={add.subjectState}
      recovering={recovering}
      recoveryEditable={recoveryEditable}
      createUncertain={createUncertain}
      selectable={add.activeSubjects.filter((subject) => !subject.archived)}
      recoverySubjects={add.recoverySubjects}
      members={add.members}
      initialValues={recovering ? add.pendingValues : add.retryDraft}
      noSubjectsRef={add.noSubjectsRef}
       initialFocusRef={addInitialFocusRef}
      onSubmit={add.create}
      onCancel={cancelAddAndFocus}
      onOpenPeople={() => { add.cancelForPeople(); onOpenPeople(); }}
    />
    </Dialog>
    {!loading && summary ? <HomeGroupList
      summary={summary}
      groupData={groupData}
      sectionHeadingRefs={sectionHeadingRefs}
      showMoreButtons={home.showMoreButtons}
      subjectName={subjectName}
      userId={userId}
      members={homeMembers.members}
      membersLoaded={homeMembers.loaded}
      busy={busy}
      onShowMore={(group) => void home.showMore(group)}
      onOpenAllItems={onOpenAllItems}
      onDone={(itemId) => void completion.finish(itemId)}
      onOpenItem={onOpenItem}
    /> : null}
  </div>;
}

function HomeActions({ empty, ready, adding, addRef, onAdd }: {
  empty: boolean;
  ready: boolean;
  adding: boolean;
  addRef: RefObject<HTMLButtonElement | null>;
  onAdd: () => void;
}) {
  const { t } = useI18n();
  if (!ready) return null;
  return <div className="space-y-4">
    <div className={`flex justify-start ${adding ? 'hidden' : ''}`}>
      <Button
        ref={addRef}
        data-action="completion-trigger"
        type="button"
        aria-hidden={adding || undefined}
        tabIndex={adding ? -1 : undefined}
        onClick={onAdd}
        className="w-full sm:w-auto"
      >
        {t('items.add')}
      </Button>
    </div>
    {empty ? <HomeEmptyState /> : null}
  </div>;
}
