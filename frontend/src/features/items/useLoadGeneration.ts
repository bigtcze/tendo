import { useCallback, useEffect, useRef } from 'react';

/**
 * Load generation for one mounted screen. Every async continuation captures a token and must
 * re-check `current(token)` right after each await; bumping `generationRef` drops older work.
 */
export function useLoadGeneration() {
  const active = useRef(false);
  const generationRef = useRef(0);
  useEffect(() => {
    active.current = true;
    return () => { active.current = false; };
  }, []);
  const current = useCallback((token: number) => active.current && generationRef.current === token, []);
  const live = useCallback(() => generationRef.current, []);
  return { generationRef, current, live };
}

export type LoadGeneration = ReturnType<typeof useLoadGeneration>;
