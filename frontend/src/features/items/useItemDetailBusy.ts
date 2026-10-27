import { useRef, useState } from 'react';

/**
 * One mutation lock shared by edit-open, edit-save and workflow/archive actions on the item detail.
 * Each holder gets an owner token, and only that owner can release the lock.
 */
export function useItemDetailBusy() {
  const [busy, setBusy] = useState(false);
  const held = useRef(false);
  const owner = useRef<symbol | null>(null);

  function tryAcquire(label: string): symbol | null {
    if (held.current) return null;
    const token = Symbol(label);
    owner.current = token;
    held.current = true;
    setBusy(true);
    return token;
  }

  function release(token: symbol) {
    if (owner.current !== token) return;
    owner.current = null;
    held.current = false;
    setBusy(false);
  }

  return { busy, isHeld: () => held.current, tryAcquire, release };
}

export type ItemDetailBusy = ReturnType<typeof useItemDetailBusy>;
