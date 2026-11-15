import { useCallback } from 'react';
import type { MessageKey } from '../i18n';
import type { NoticeMessage } from './notices';

type LeftNoticeSource = { householdLeftNotice: NoticeMessage | null; dismissHouseholdLeft: () => void };
type EntryNoticeSource = { notice: MessageKey | null; clearNotice: () => void };

/**
 * One calm line above Home. Leaving a household wins over the sign-in entry notice because it is the fresher event,
 * and dismissing either clears whichever of the two is showing.
 */
export function useHomeNotice(left: LeftNoticeSource, entry: EntryNoticeSource): [NoticeMessage | null, () => void] {
  const { householdLeftNotice, dismissHouseholdLeft } = left;
  const { notice: entryNotice, clearNotice } = entry;
  const notice = householdLeftNotice ?? (entryNotice ? { key: entryNotice } : null);
  const dismiss = useCallback(() => {
    dismissHouseholdLeft();
    clearNotice();
  }, [clearNotice, dismissHouseholdLeft]);
  return [notice, dismiss];
}
