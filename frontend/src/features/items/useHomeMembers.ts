import { useEffect, useRef, useState } from 'react';
import { listMembers, type Member } from '../members/membersApi';

const MAX_MEMBER_PAGES = 20;

export function useHomeMembers(householdId: string, onSignedOut: () => void) {
  const [state, setState] = useState<{
    householdId: string;
    members: Member[];
  } | null>(null);
  const generation = useRef(0);
  const mounted = useRef(false);

  useEffect(() => {
    mounted.current = true;
    const generationRef = generation;
    return () => {
      mounted.current = false;
      generationRef.current++;
    };
  }, []);

  useEffect(() => {
    const token = ++generation.current;
    const collected: Member[] = [];
    let cursor: string | undefined;
    const current = () => mounted.current && token === generation.current;
    const load = async () => {
      for (let page = 0; page < MAX_MEMBER_PAGES; page++) {
        const result = await listMembers(householdId, cursor);
        if (!current()) return;
        if (result.kind === 'unauthenticated') {
          onSignedOut();
          return;
        }
        if (result.kind !== 'ok') {
          setState(null);
          return;
        }
        collected.push(...result.items);
        if (!result.nextCursor) {
          setState({ householdId, members: collected });
          return;
        }
        cursor = result.nextCursor;
      }
      if (current()) setState(null);
    };
    void load();
    const generationRef = generation;
    return () => {
      generationRef.current++;
    };
  }, [householdId, onSignedOut]);

  const current = state?.householdId === householdId;
  return { members: current ? state.members : [], loaded: current };
}
