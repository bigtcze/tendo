import { Button } from '../../components/ui/button';
import type { Translate } from '../../i18n';

export function DetailCompletionActions({ done, busy, notice, completion, t, onDone, onUndo, className = '' }: {
  done: boolean; busy: boolean; notice: string | null; completion: boolean; t: Translate; onDone: () => void; onUndo: () => void; className?: string;
}) {
  return <div className={`detail-completion flex flex-wrap items-center gap-3 ${className}`}>
    {!done ? <Button className="detail-done" type="button" disabled={busy} onClick={onDone}>{t('items.done')}</Button> : null}
    {notice ? <div className="detail-completion-notice flex flex-wrap items-center gap-3 rounded-xl bg-sand px-4 py-3"><span role="status" aria-live="polite">{notice}</span>{completion ? <Button type="button" variant="quiet" disabled={busy} onClick={onUndo}>{t('items.undo')}</Button> : null}</div> : null}
  </div>;
}
