import { useCallback, useRef, useState } from 'react';

export function useMutationLock() {
  const mutationBusy = useRef(false);
  const [busy, setBusy] = useState(false);
  const begin = useCallback(() => {
    if (mutationBusy.current) return false;
    mutationBusy.current = true;
    setBusy(true);
    return true;
  }, []);
  const end = useCallback(() => {
    mutationBusy.current = false;
    setBusy(false);
  }, []);
  return { busy, begin, end };
}
