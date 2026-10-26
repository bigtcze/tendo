import { useEffect, useRef, useState, type RefObject } from 'react';
import type { ItemGroup } from './itemsApi';
import { groups, type GroupData } from './homeGroups';

type Labels = { loading: boolean; hasFailures: boolean; retry: () => void };

/**
 * Pending state and outcome focus for the subject-label retry. Called by the screen after the add-item
 * hooks so this effect runs after the catalogue-retry focus effect: when both retries settle in one
 * commit, focus already placed by the catalogue outcome is kept.
 */
export function useLabelRetryFocus(
  labels: Labels,
  groupData: Record<ItemGroup, GroupData>,
  sectionHeadingRefs: RefObject<Map<ItemGroup, HTMLHeadingElement>>,
) {
  const retryRef = useRef<HTMLButtonElement>(null);
  const [pending, setPending] = useState(false);
  useEffect(() => {
    if (!pending || labels.loading) return;
    // The retry control stays mounted while its batch runs; afterwards focus follows the outcome unless the user moved it.
    if (document.activeElement === retryRef.current || document.activeElement === document.body) {
      if (labels.hasFailures) retryRef.current?.focus();
      else sectionHeadingRefs.current.get(groups.find((group) => groupData[group.id].items.length)?.id ?? 'needs')?.focus();
    }
    // eslint-disable-next-line react-hooks/set-state-in-effect -- settle the retry once its batch completes.
    setPending(false);
  }, [pending, labels.loading, labels.hasFailures, groupData, sectionHeadingRefs]);
  function retry() {
    if (pending) return;
    setPending(true);
    labels.retry();
  }
  return { retryRef, pending, retry };
}
