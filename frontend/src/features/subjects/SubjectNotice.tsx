import type { MutableRefObject } from 'react';
import { Button } from '../../components/ui/button';
import { type Notice, type TransferProblem } from './subjectsModel';
import type { MessageKey } from '../../i18n';

type Translate = (key: MessageKey, values?: Record<string, string>) => string;

const transferProblemKeys: Record<TransferProblem, MessageKey> = {
  changed: 'subjects.transfer.error.changed',
  stale: 'subjects.transfer.error.stale',
  notEligible: 'subjects.transfer.error.notEligible',
  notFound: 'subjects.transfer.error.notFound',
};

function noticeText(notice: Notice, t: Translate) {
  switch (notice.kind) {
    case 'transferError':
      return t(transferProblemKeys[notice.code]);
    case 'transferred':
      return t('subjects.notice.transferred', {
        name: notice.subject.name, previous: notice.previous.name, login: notice.login,
      });
    case 'gone':
    case 'changed':
    case 'uncertain':
    case 'reused':
      return t(`subjects.notice.${notice.kind}`);
    default:
      return t(`subjects.notice.${notice.kind}`, { name: notice.subject.name });
  }
}

export function SubjectNotice({
  notice, noticeRef, interactionLocked, t, onUndo,
}: {
  notice: Notice | null | undefined;
  noticeRef: MutableRefObject<HTMLDivElement | null>;
  interactionLocked: boolean;
  t: Translate;
  onUndo: (notice: Notice & { kind: 'archived' | 'restored' }) => void;
}) {
  return <div ref={noticeRef} tabIndex={-1} role="status" className="mt-6 outline-none empty:mt-0">
    {notice ? <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1 rounded-xl bg-sand px-4 py-1 text-ink">
      <p className="py-2">{noticeText(notice, t)}</p>
      {notice.kind === 'archived' || notice.kind === 'restored' ? <Button type="button" variant="quiet" size="small"
        disabled={interactionLocked} onClick={() => onUndo(notice)}>{t('subjects.undo')}</Button> : null}
    </div> : null}
  </div>;
}
