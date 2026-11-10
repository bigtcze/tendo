import type { Dispatch, MutableRefObject, SetStateAction } from 'react';
import type { Member } from '../members/membersApi';
import { getMemberPerson, subjectVersion, transferMemberPerson, type Subject } from './subjectsApi';
import type { Notice, TransferProblem } from './subjectsModel';

export type TransferOutcome = 'done' | 'failed' | TransferProblem;
/** The chosen member's current person, previewed before the explicit confirmation. */
export type MemberPersonPreview = { kind: 'ok'; name: string } | { kind: 'missing' } | { kind: 'failed' };

export type MemberPersonTransferApi = {
  preview: (userId: string) => Promise<MemberPersonPreview>;
  transfer: (subject: Subject, etag: string, member: Member) => Promise<TransferOutcome>;
};

/**
 * Explicit reassociation of an existing unlinked person with a household member. This is never a merge:
 * the server keeps the previous person as an ordinary record with its items, and only the link moves.
 */
export function useMemberPersonTransfer(ctx: {
  householdId: string;
  generation: MutableRefObject<number>;
  closeEditing: (id: string) => void;
  updateRow: (subject: Subject) => void;
  setNotice: Dispatch<SetStateAction<Notice | null | undefined>>;
  reload: () => void;
  onSignedOut: () => void;
}): MemberPersonTransferApi {
  async function preview(userId: string): Promise<MemberPersonPreview> {
    const current = await getMemberPerson(ctx.householdId, userId);
    if (current.kind === 'ok') return { kind: 'ok', name: current.person.name };
    if (current.kind === 'unauthenticated') ctx.onSignedOut();
    return { kind: current.kind === 'notFound' ? 'missing' : 'failed' };
  }

  /** Stale views cannot be repaired from here: close the editor, say why, and re-read the list. */
  function refuse(subjectId: string, code: TransferProblem): TransferOutcome {
    ctx.closeEditing(subjectId);
    ctx.setNotice({ kind: 'transferError', code });
    ctx.reload();
    return code;
  }

  async function transfer(subject: Subject, etag: string, member: Member): Promise<TransferOutcome> {
    const started = ctx.generation.current;
    const subjectVersionNumber = subjectVersion(etag);
    if (subjectVersionNumber === null) return 'failed';
    const current = await getMemberPerson(ctx.householdId, member.userId);
    if (current.kind === 'unauthenticated') {
      ctx.onSignedOut();
      return 'done';
    }
    if (current.kind === 'error') return 'failed';
    if (current.kind === 'notFound') return refuse(subject.id, 'notFound');
    const result = await transferMemberPerson(ctx.householdId, member.userId, {
      subjectId: subject.id,
      subjectVersion: subjectVersionNumber,
      currentSubjectId: current.person.id,
      currentSubjectVersion: current.version,
    });
    if (result.kind === 'unauthenticated') {
      ctx.onSignedOut();
      return 'done';
    }
    if (started !== ctx.generation.current) return 'done';
    if (result.kind === 'ok') {
      ctx.updateRow(result.person);
      ctx.updateRow(result.previous);
      ctx.setNotice({ kind: 'transferred', subject: result.person, previous: result.previous, login: member.login });
      ctx.closeEditing(subject.id);
      return 'done';
    }
    if (result.kind === 'error') return 'failed';
    return refuse(subject.id, result.kind);
  }

  return { preview, transfer };
}
