import { changeItem, type ItemPatch } from './itemsApi';
import type { ItemDetailBusy } from './useItemDetailBusy';
import type { ItemDetailState } from './useItemDetail';
import type { LoadGeneration } from './useLoadGeneration';

export type WorkflowState = 'open' | 'in_progress' | 'waiting' | 'paused';
type DetailChange = { workflowState: WorkflowState } | { archived: boolean };

type Options = { householdId: string; itemId: string; onSignedOut: () => void; detail: ItemDetailState; busy: ItemDetailBusy; loads: LoadGeneration };

/** Workflow (start/wait/pause/resume) and archive/restore/Undo actions; each reads a fresh ETag, then PATCHes. */
export function useItemDetailActions({ householdId, itemId, onSignedOut, detail, busy, loads }: Options) {
  const { current, generationRef } = loads;

  /** Applies every outcome except 412, which needs a reload. */
  function settle(result: Exclude<ItemPatch, { kind: 'changed' }>, change: DetailChange) {
    if (result.kind === 'unauthenticated') { onSignedOut(); return; }
    if (result.kind === 'notFound') { detail.setState('gone'); return; }
    if (result.kind !== 'ok') { detail.announce(result.kind === 'invalid' ? 'itemDetail.invalid' : 'itemDetail.actionFailed'); return; }
    detail.setItem(result.item);
    if (!('archived' in change)) { detail.announce('itemDetail.updated'); return; }
    detail.announce(change.archived ? 'itemDetail.archivedNotice' : 'itemDetail.restored');
    detail.setUndoArchive(change.archived);
  }

  async function change(body: DetailChange) {
    const owner = busy.tryAcquire('workflow-mutation');
    if (!owner) return;
    // Undo keeps the archive notice (and its Undo button) visible until the outcome replaces it.
    const keepUndo = 'archived' in body && detail.undoArchive;
    if (!keepUndo) detail.setNotice(null);
    const token = generationRef.current;
    try {
      const result = await changeItem(householdId, itemId, body);
      if (!current(token)) return;
      if (result.kind === 'changed') {
        // Changed concurrently: reload the latest item under a new generation, then say so.
        const next = ++generationRef.current;
        await detail.load(next, keepUndo);
        if (current(next)) detail.announce('itemDetail.changed');
        return;
      }
      settle(result, body);
    } finally {
      busy.release(owner);
    }
  }

  return {
    setWorkflow: (workflowState: WorkflowState) => void change({ workflowState }),
    archive: () => void change({ archived: true }),
    restore: () => void change({ archived: false }),
  };
}
