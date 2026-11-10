import type { MouseEvent, RefObject } from 'react';
import type { Translate } from '../../i18n';
import { CursorPager } from '../../components/ui/cursor-pager';
import { AdminAccountRow } from './AdminAccountRow';
import type { Account } from './adminApi';

export function AdminAccountList({ accounts, viewerId, busy, headingRef, stack, nextCursor, onPrevious, onNext, onChange, t }: {
  accounts: Account[];
  viewerId: string;
  busy: boolean;
  headingRef: RefObject<HTMLHeadingElement | null>;
  stack: string[];
  nextCursor: string | null;
  onPrevious: (event: MouseEvent<HTMLButtonElement>) => void;
  onNext: (event: MouseEvent<HTMLButtonElement>) => void;
  onChange: (account: Account, trigger: HTMLElement) => void;
  t: Translate;
}) {
  return <section role="region" aria-labelledby="admin-accounts-heading" className="mt-6 sm:mt-8">
    <h2 id="admin-accounts-heading" ref={headingRef} tabIndex={-1} className="text-h2 text-ink outline-none">{t('admin.list.title')}</h2>
    {accounts.length
      ? <ul aria-labelledby="admin-accounts-heading" className="mt-3 divide-y divide-line overflow-hidden rounded-2xl border border-line bg-raised">
        {accounts.map((account) => (
          <AdminAccountRow key={account.userId} account={account} viewerId={viewerId} busy={busy} onChange={onChange} t={t} />
        ))}
      </ul>
      : <p className="mt-3 rounded-2xl border border-line bg-raised px-4 py-5 text-body text-muted">{t('admin.list.empty')}</p>}
    <p className="mt-3 max-w-prose text-small text-muted">{t('admin.list.explained')}</p>
    {stack.length > 0 || nextCursor
      ? <CursorPager kind="account" stack={stack} cursor={nextCursor} busy={busy} onPrevious={onPrevious} onNext={onNext} t={t} />
      : null}
  </section>;
}
