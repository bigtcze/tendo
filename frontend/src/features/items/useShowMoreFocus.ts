import { useEffect, type RefObject } from 'react';
import type { ItemGroup } from './itemsApi';
import { HOME_GROUP_ROW_CAP, type GroupData } from './homeGroups';

export function useShowMoreFocus(
  groupData: Record<ItemGroup, GroupData>,
  focusedRef: RefObject<ItemGroup | null>,
  buttonsRef: RefObject<Map<ItemGroup, HTMLButtonElement>>,
  headingsRef: RefObject<Map<ItemGroup, HTMLHeadingElement>>,
) {
  useEffect(() => {
    const id = focusedRef.current;
    if (!id || groupData[id].loadingMore) return;
    const button = buttonsRef.current.get(id);
    if (document.activeElement !== button && document.activeElement !== document.body) { focusedRef.current = null; return; }
    const allItems = document.querySelector<HTMLAnchorElement>(`[data-group-all="${id}"]`);
    if (groupData[id].items.length >= HOME_GROUP_ROW_CAP && allItems) allItems.focus();
    else if (!button || !groupData[id].cursor) headingsRef.current.get(id)?.focus();
    else if (document.activeElement !== button) button.focus();
    focusedRef.current = null;
  }, [groupData, buttonsRef, focusedRef, headingsRef]);
}
