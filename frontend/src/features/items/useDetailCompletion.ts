import { useState } from 'react';
import type { Translate } from '../../i18n';
import { idempotencyKey } from '../../lib/idempotencyKey';
import { completeItem, getItem, undoCompletion, type Completion, type CompleteResult, type UndoResult } from './itemsApi';

function completionMessage(result: CompleteResult, t: Translate) {
  if (result.kind === 'changed') return t('items.notice.changed');
  if (result.kind === 'conflict') return t(result.code === 'item_archived' ? 'items.notice.archived' : 'items.notice.alreadyDone');
  if (result.kind === 'notFound') return t('items.notice.gone');
  if (result.kind === 'invalid') return t('items.notice.validation');
  return t('items.error.action');
}

function undoMessage(result: UndoResult, t: Translate) {
  if (result.kind === 'changed') return t('items.notice.changed');
  if (result.kind === 'conflict') return t(result.code === 'completion_not_latest' ? 'items.notice.notLatest' : 'items.notice.archived');
  if (result.kind === 'notFound') return t('items.notice.gone');
  return t('items.error.action');
}

export function useDetailCompletion(householdId: string, itemId: string, title: string, t: Translate, onSignedOut: () => void, reload: () => void) {
  const [busy, setBusy] = useState(false);
  const [completion, setCompletion] = useState<Completion | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  async function onDone() {
    if (busy) return;
    setBusy(true);
    try {
      const current = await getItem(householdId, itemId);
      if (current.kind === 'unauthenticated') { onSignedOut(); return; }
      if (current.kind !== 'ok') { setNotice(t(current.kind === 'changed' ? 'items.notice.changed' : 'items.error.action')); return; }
      const key = idempotencyKey();
      const result = await completeItem(householdId, itemId, current.etag, key);
      if (result.kind === 'unauthenticated') { onSignedOut(); return; }
      if (result.kind === 'ok') { setCompletion(result.completion); setNotice(t('items.notice.done', { title })); reload(); }
      else if (result.kind === 'error' || result.kind === 'unavailable') setNotice(t('items.notice.retry'));
      else setNotice(completionMessage(result, t));
    } finally { setBusy(false); }
  }

  async function onUndo() {
    if (!completion || busy) return;
    setBusy(true);
    try {
      const current = await getItem(householdId, itemId);
      if (current.kind === 'unauthenticated') { onSignedOut(); return; }
      if (current.kind !== 'ok') { setNotice(t('items.notice.changed')); return; }
      const result = await undoCompletion(householdId, itemId, completion.id, current.etag);
      if (result.kind === 'unauthenticated') { onSignedOut(); return; }
      if (result.kind === 'ok') { setCompletion(null); setNotice(null); reload(); }
      else if (result.kind === 'error' || result.kind === 'unavailable') setNotice(t('items.notice.retry'));
      else setNotice(undoMessage(result, t));
    } finally { setBusy(false); }
  }

  return { busy, notice, completion, onDone, onUndo };
}
