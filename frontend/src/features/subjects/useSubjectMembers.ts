import { useEffect, useRef, useState } from 'react';
import { listMembers, type Member } from '../members/membersApi';

const MAX_MEMBER_PAGES = 20;

export function useSubjectMembers(
  householdId: string,
  adding: boolean,
  editing: { subject: { id: string }; etag: string } | null,
  onSignedOut: () => void,
) {
  const [members, setMembers] = useState<Member[]>([]);
  const [membersLoaded, setMembersLoaded] = useState(false);
  const memberGeneration = useRef(0);

  useEffect(() => {
    const generationRef = memberGeneration;
    const token = ++generationRef.current;
    void (async () => {
      const collected: Member[] = [];
      let cursor: string | undefined;
      for (let page = 0; page < MAX_MEMBER_PAGES; page++) {
        const result = await listMembers(householdId, cursor);
        if (token !== generationRef.current) return;
        if (result.kind === 'unauthenticated') { onSignedOut(); return; }
        if (result.kind !== 'ok') { setMembers([]); setMembersLoaded(false); return; }
        collected.push(...result.items);
        if (!result.nextCursor) { setMembers(collected); setMembersLoaded(true); return; }
        cursor = result.nextCursor;
      }
      if (token === generationRef.current) { setMembers([]); setMembersLoaded(false); }
    })();
    return () => { if (generationRef.current === token) generationRef.current++; };
  }, [householdId, adding, editing?.subject.id, editing?.etag, onSignedOut]);

  function resetMembers() {
    setMembers([]);
    setMembersLoaded(false);
  }

  return { members, membersLoaded, resetMembers };
}
