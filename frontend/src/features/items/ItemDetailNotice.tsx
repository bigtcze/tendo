import type { Ref } from 'react';
import { Button } from '../../components/ui/button';
import type { MessageKey, Translate } from '../../i18n';

/** Outcome notice after an action; after archiving it also offers Undo. */
export function ItemDetailNotice({ notice, undoArchive, busy, t, focusRef, onUndo }: {
  notice: MessageKey | null; undoArchive: boolean; busy: boolean; t: Translate; focusRef: Ref<HTMLParagraphElement>; onUndo: () => void;
}) {
  if (!notice) return null;
  return <div className="mt-4 rounded-xl bg-sand px-4 py-3">
    <p ref={focusRef} tabIndex={-1} role="status" aria-live="polite" className="outline-none">{t(notice)}</p>
    {undoArchive ? <Button type="button" variant="quiet" className="mt-1" disabled={busy} onClick={onUndo}>{t('subjects.undo')}</Button> : null}
  </div>;
}
