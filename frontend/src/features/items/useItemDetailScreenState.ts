import { useRef } from 'react';
import type { Translate } from '../../i18n';
import { useItemDetail } from './useItemDetail';
import { useItemDetailActions } from './useItemDetailActions';
import { useItemDetailCompletion } from './useItemDetailCompletion';
import { useItemDetailBusy } from './useItemDetailBusy';
import { useItemDetailEdit } from './useItemDetailEdit';
import { useItemDetailHistory } from './useItemDetailHistory';
import type { LoadGeneration } from './useLoadGeneration';

type Options = {
  householdId: string;
  itemId: string;
  userId: string;
  onSignedOut: () => void;
  t: Translate;
  loads: LoadGeneration;
};

export function useItemDetailScreenState({ householdId, itemId, userId, onSignedOut, t, loads }: Options) {
  const historySectionRef = useRef<HTMLElement>(null);
  const historyHeadingRef = useRef<HTMLHeadingElement>(null);
  const history = useItemDetailHistory({
    headingRef: historyHeadingRef,
    sectionRef: historySectionRef,
    householdId,
    itemId,
    onSignedOut,
    current: loads.current,
    liveToken: loads.live,
  });
  const detail = useItemDetail({ householdId, itemId, onSignedOut, loads, history });
  const busy = useItemDetailBusy();
  const returnEditFocusRef = useRef(false);
  const editButtonRef = useRef<HTMLButtonElement>(null);
  const noticeRef = useRef<HTMLParagraphElement>(null);
  const completionNoticeRef = useRef<HTMLDivElement>(null);
  const edit = useItemDetailEdit({ householdId, itemId, onSignedOut, t, detail, busy, loads, returnEditFocusRef });
  const actions = useItemDetailActions({ householdId, itemId, onSignedOut, detail, busy, loads });
  const completion = useItemDetailCompletion({ userId, householdId, itemId, onSignedOut, detail, busy, loads, noticeRef: completionNoticeRef, history });
  const focusRefs = { returnEditFocusRef, editButtonRef, noticeRef, completionNoticeRef };
  const focus = { busy, actions: completion, refs: focusRefs, detail, focusAfterMutationRef: detail.focusAfterMutationRef };

  return {
    historySectionRef,
    historyHeadingRef,
    history,
    detail,
    busy,
    returnEditFocusRef,
    editButtonRef,
    noticeRef,
    completionNoticeRef,
    edit,
    completion,
    actions: { ...actions, ...completion },
    focusRefs,
    focus,
  };
}
