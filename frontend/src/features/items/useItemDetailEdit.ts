import { useState, type MutableRefObject } from 'react';
import type { Translate } from '../../i18n';
import type { Member } from '../members/membersApi';
import { getItem, type Subject } from './itemsApi';
import type { ItemFormValues } from './ItemForm';
import { changedEditFields } from './itemEditModel';
import { editSaveFlow, HANDLED, type SaveOutcome } from './itemEditSave';
import { loadEditChoices } from './loadEditChoices';
import type { ItemDetailBusy } from './useItemDetailBusy';
import type { ItemDetailState } from './useItemDetail';
import type { LoadGeneration } from './useLoadGeneration';

type Options = {
  householdId: string; itemId: string; onSignedOut: () => void; t: Translate;
  detail: ItemDetailState; busy: ItemDetailBusy; loads: LoadGeneration; returnEditFocusRef: MutableRefObject<boolean>;
};

/**
 * Conflict-aware item editing. Opening reads a fresh ETag plus the subject/member choices; saving
 * PATCHes only changed fields with that ETag, and a 412 rebases the draft onto the latest item.
 */
export function useItemDetailEdit({ householdId, itemId, onSignedOut, t, detail, busy, loads, returnEditFocusRef }: Options) {
  const [subjects, setSubjects] = useState<Subject[]>([]);
  const [members, setMembers] = useState<Member[]>([]);
  const { current } = loads;

  /** Never shows a stale name: the label falls back to "unknown" unless the subject is in the edit choices. */
  function showKnownSubject(subjectId: string) {
    detail.showSubject(subjects.find((subject) => subject.id === subjectId)?.name ?? t('items.subject.unknown'));
  }

  const submitEdit = editSaveFlow({ householdId, itemId, onSignedOut, detail, current, showKnownSubject, setSubjects });

  async function openEdit(token: number) {
    const result = await getItem(householdId, itemId);
    if (!current(token)) return;
    if (result.kind === 'unauthenticated') { onSignedOut(); return; }
    if (result.kind === 'notFound') { detail.setState('gone'); return; }
    if (result.kind !== 'ok') { detail.setNotice('itemDetail.actionFailed'); return; }
    const choices = await loadEditChoices(householdId, result.item, () => current(token));
    if (choices.kind === 'stale' || !current(token)) return;
    if (choices.kind === 'unauthenticated') { onSignedOut(); return; }
    if (choices.kind === 'gone') { detail.setState('gone'); return; }
    if (choices.kind === 'failed') { detail.setNotice(choices.notice); return; }
    const selected = choices.subjects.find((subject) => subject.id === result.item.subjectId);
    detail.setItem(result.item); setSubjects(choices.subjects); setMembers(choices.members);
    detail.setEditing({ etag: result.etag, baseline: result.item, conflict: false });
    if (selected) detail.showSubject(selected.name);
  }

  async function beginEdit() {
    if (detail.editLoading) return;
    const owner = busy.tryAcquire('edit-load');
    if (!owner) return;
    const token = loads.generationRef.current;
    detail.setNotice(null); detail.setEditLoading(true);
    try { await openEdit(token); } finally {
      busy.release(owner);
      if (current(token)) detail.setEditLoading(false);
    }
  }

  async function saveEdit(values: ItemFormValues): Promise<SaveOutcome> {
    const { editing, item } = detail;
    if (!editing || !item) return HANDLED;
    const body = changedEditFields(values, editing.baseline);
    if (!Object.keys(body).length) {
      returnEditFocusRef.current = true; detail.setEditing(null); return { kind: 'done' };
    }
    const owner = busy.tryAcquire('edit-save');
    if (!owner) return HANDLED;
    try { return await submitEdit(loads.generationRef.current, editing, values, body); } finally { busy.release(owner); }
  }

  function cancelEdit() { returnEditFocusRef.current = true; detail.setEditing(null); }

  return { subjects, members, beginEdit, saveEdit, cancelEdit };
}
