import type { Dispatch, SetStateAction } from 'react';
import { getSubject } from '../subjects/subjectsApi';
import { getItem, patchItem, type Item, type ItemField, type Subject } from './itemsApi';
import type { ItemFormValues } from './ItemForm';
import { invalidEditField, rebaseEdit, type EditPatch, type Editing } from './itemEditModel';
import type { ItemDetailState } from './useItemDetail';

export type SaveOutcome = { kind: 'handled' | 'done' | 'failed' } | { kind: 'invalid'; field: ItemField; code: string };
export const HANDLED = { kind: 'handled' } as const;

type Context = {
  householdId: string; itemId: string; onSignedOut: () => void; detail: ItemDetailState;
  current: (token: number) => boolean; showKnownSubject: (subjectId: string) => void;
  setSubjects: Dispatch<SetStateAction<Subject[]>>;
};

/** PATCH flow for a conflict-aware edit: a 412 rebases the draft onto the latest item and keeps editing. */
export function editSaveFlow({ householdId, itemId, onSignedOut, detail, current, showKnownSubject, setSubjects }: Context) {
  async function rebaseAfterConflict(token: number, values: ItemFormValues, original: Item): Promise<SaveOutcome> {
    const latest = await getItem(householdId, itemId);
    if (!current(token)) return HANDLED;
    if (latest.kind === 'unauthenticated') { onSignedOut(); return HANDLED; }
    if (latest.kind === 'notFound') { detail.setState('gone'); detail.setEditing(null); return HANDLED; }
    if (latest.kind !== 'ok') return { kind: 'failed' };
    const mine = rebaseEdit(values, original, latest.item);
    const latestSubject = await getSubject(householdId, latest.item.subjectId);
    if (!current(token)) return HANDLED;
    if (latestSubject.kind === 'unauthenticated') { onSignedOut(); return HANDLED; }
    showKnownSubject(latest.item.subjectId);
    if (latestSubject.kind === 'ok') detail.showSubject(latestSubject.subject.name);
    detail.setItem(latest.item);
    detail.setEditing({ etag: latest.etag, baseline: latest.item, conflict: true, latest: latest.item, mine });
    if (latestSubject.kind === 'ok') {
      const fresh = latestSubject.subject;
      setSubjects((known) => known.some((subject) => subject.id === fresh.id) ? known : [...known, fresh]);
    }
    return HANDLED;
  }

  async function finishSaved(token: number, saved: Item): Promise<SaveOutcome> {
    showKnownSubject(saved.subjectId);
    detail.setItem(saved); detail.setEditing(null); detail.announce('itemDetail.updated');
    const savedSubject = await getSubject(householdId, saved.subjectId);
    if (!current(token)) return HANDLED;
    if (savedSubject.kind === 'unauthenticated') { onSignedOut(); return HANDLED; }
    if (savedSubject.kind === 'ok') detail.showSubject(savedSubject.subject.name);
    return { kind: 'done' };
  }

  return async function submitEdit(token: number, editing: Editing, values: ItemFormValues, body: EditPatch): Promise<SaveOutcome> {
    const result = await patchItem(householdId, itemId, editing.etag, body);
    if (!current(token)) return HANDLED;
    if (result.kind === 'unauthenticated') { onSignedOut(); return HANDLED; }
    if (result.kind === 'notFound') { detail.setState('gone'); detail.setEditing(null); return HANDLED; }
    if (result.kind === 'changed') return rebaseAfterConflict(token, values, editing.baseline);
    if (result.kind === 'ok') return finishSaved(token, result.item);
    if (result.kind !== 'invalid') return { kind: 'failed' };
    const field = invalidEditField(result.field);
    return field ? { kind: 'invalid', field, code: result.code } : { kind: 'failed' };
  };
}
