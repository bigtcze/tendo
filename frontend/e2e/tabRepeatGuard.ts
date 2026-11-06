export type TabRepeatResult = 'new' | 'segment' | 'trap';

/** Allows at most `cap` additional Tab presses that remain on a native date input. */
export function createTabRepeatGuard(cap: number) {
  if (!Number.isInteger(cap) || cap < 0) throw new RangeError('cap must be a non-negative integer');
  let lastId: string | undefined;
  let additionalDatePresses = 0;

  return {
    observe(id: string, isNativeDate: boolean): TabRepeatResult {
      if (id !== lastId) {
        lastId = id;
        additionalDatePresses = 0;
        return 'new';
      }
      if (!isNativeDate) return 'trap';
      additionalDatePresses += 1;
      return additionalDatePresses <= cap ? 'segment' : 'trap';
    },
  };
}
