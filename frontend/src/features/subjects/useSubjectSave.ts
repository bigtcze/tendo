import type { Dispatch, MutableRefObject, SetStateAction } from 'react';
import { getSubject, patchSubject, type PatchResult, type Subject } from './subjectsApi';
import type { SubjectValues, SubmitOutcome } from './SubjectForm';
import { subjectChanges, type Editing } from './subjectsModel';

export function useSubjectSave(ctx: {
  editing: Editing | null; generation: MutableRefObject<number>; householdId: string; showArchived: boolean;
  closeEditing: (id: string) => void; updateRow: (subject: Subject) => void; setEditing: Dispatch<SetStateAction<Editing | null>>;
  changedMeanwhile: () => void; gone: () => void; onSignedOut: () => void;
}) {
  async function save(values: SubjectValues): Promise<SubmitOutcome> {
    if (!ctx.editing) return { kind: 'handled' };
    const started = ctx.generation.current;
    const { subject, etag } = ctx.editing;
    const changes = subjectChanges(values, subject);
    if (Object.keys(changes).length === 0) {
      ctx.closeEditing(subject.id);
      return { kind: 'done' };
    }
    const result: PatchResult = await patchSubject(ctx.householdId, subject.id, etag, changes);
    if (result.kind === 'unauthenticated') {
      ctx.onSignedOut();
      return { kind: 'handled' };
    }
    if (started !== ctx.generation.current) return { kind: 'handled' };
    switch (result.kind) {
      case 'ok':
        ctx.updateRow(result.subject);
        ctx.closeEditing(subject.id);
        return { kind: 'done' };
      case 'conflict':
        return resolveConflict(ctx, values, subject, started);
      case 'notFound':
        ctx.gone();
        return { kind: 'handled' };
      case 'invalid':
        return result;
      default:
        return { kind: 'failed' };
    }
  }
  return { save };
}

async function resolveConflict(
  ctx: Parameters<typeof useSubjectSave>[0], values: SubjectValues, subject: Subject, started: number,
): Promise<SubmitOutcome> {
  const latest = await getSubject(ctx.householdId, subject.id);
  if (latest.kind === 'unauthenticated') {
    ctx.onSignedOut();
    return { kind: 'handled' };
  }
  if (started !== ctx.generation.current) return { kind: 'handled' };
  if (latest.kind === 'ok') {
    if (latest.subject.archived !== ctx.showArchived) {
      ctx.changedMeanwhile();
      return { kind: 'handled' };
    }
    ctx.updateRow(latest.subject);
    ctx.setEditing({ subject: latest.subject, etag: latest.etag, conflict: true, mine: values });
    return { kind: 'handled' };
  }
  if (latest.kind === 'notFound') {
    ctx.gone();
    return { kind: 'handled' };
  }
  return { kind: 'failed' };
}
