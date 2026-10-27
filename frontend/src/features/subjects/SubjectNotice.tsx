import type { MutableRefObject } from 'react';
import { Button } from '../../components/ui/button';
import type { Notice } from './subjectsModel';
import type { MessageKey } from '../../i18n';

export function SubjectNotice({
  notice, noticeRef, interactionLocked, t, onUndo,
}: {
  notice: Notice | null | undefined;
  noticeRef: MutableRefObject<HTMLDivElement | null>;
  interactionLocked: boolean;
  t: (key: MessageKey, values?: Record<string, string>) => string;
  onUndo: (notice: Notice & { kind: 'archived' | 'restored' }) => void;
}) {
  return <div ref={noticeRef} tabIndex={-1} role="status" className="mt-6 outline-none empty:mt-0">
    {notice ? <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1 rounded-xl bg-sand px-4 py-1 text-ink">
      <p className="py-2">
        {notice.kind === 'gone' || notice.kind === 'changed' || notice.kind === 'uncertain' || notice.kind === 'reused'
          ? t(`subjects.notice.${notice.kind}`)
          : t(`subjects.notice.${notice.kind}`, { name: notice.subject.name })}
      </p>
      {notice.kind === 'archived' || notice.kind === 'restored' ? <Button type="button" variant="quiet" size="small"
        disabled={interactionLocked} onClick={() => onUndo(notice)}>{t('subjects.undo')}</Button> : null}
    </div> : null}
  </div>;
}
