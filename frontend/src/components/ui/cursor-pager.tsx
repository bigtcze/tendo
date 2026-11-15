import type { MouseEvent } from 'react';
import { Button } from './button';
import type { Translate } from '../../i18n';

type PagerKind = 'member' | 'invitation' | 'account' | 'household' | 'directory';

/** The list a pager belongs to; its own name heads the navigation landmark. */
const LABEL: Record<PagerKind, 'members.list.title' | 'members.invitations.title' | 'admin.list.title' | 'install.title' | 'install.add.resultsLabel'> = {
  member: 'members.list.title',
  invitation: 'members.invitations.title',
  account: 'admin.list.title',
  household: 'install.title',
  directory: 'install.add.resultsLabel',
};

/** Previous/next pager shared by the member, invitation, account, household and directory lists; `kind` names it. */
export function CursorPager({ kind, stack, cursor, busy, onPrevious, onNext, t }: {
  kind: PagerKind;
  stack: string[];
  cursor: string | null;
  busy: boolean;
  onPrevious: (event: MouseEvent<HTMLButtonElement>) => void;
  onNext: (event: MouseEvent<HTMLButtonElement>) => void;
  t: Translate;
}) {
  const marker = { [`data-${kind}-page`]: true };
  const label = t(LABEL[kind]);
  return <nav aria-label={label} className="mt-3 flex flex-wrap items-center justify-between gap-2">
    {stack.length
      ? <Button {...marker} variant="quiet" disabled={busy} onClick={onPrevious}>
        {stack.length > 1 ? t('members.page.previous') : t('members.page.first')}
      </Button>
      : <span />}
    {cursor ? <Button {...marker} variant="quiet" disabled={busy} onClick={onNext}>{t('members.page.next')}</Button> : null}
    <p aria-live="polite" className="w-full text-center text-sm text-muted">{t('members.page.number', { n: String(stack.length + 1) })}</p>
  </nav>;
}
