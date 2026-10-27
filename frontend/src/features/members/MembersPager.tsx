import type { MouseEvent } from 'react';
import { Button } from '../../components/ui/button';
import type { Translate } from '../../i18n';

/** Previous/next pager shared by the member and invitation lists; `kind` selects its data attribute. */
export function MembersPager({ kind, stack, cursor, busy, onPrevious, onNext, t }: {
  kind: 'member' | 'invitation';
  stack: string[];
  cursor: string | null;
  busy: boolean;
  onPrevious: (event: MouseEvent<HTMLButtonElement>) => void;
  onNext: (event: MouseEvent<HTMLButtonElement>) => void;
  t: Translate;
}) {
  const marker = { [`data-${kind}-page`]: true };
  return <div className="mt-3 flex flex-wrap items-center justify-between gap-2">
    {stack.length
      ? <Button {...marker} variant="quiet" disabled={busy} onClick={onPrevious}>
        {stack.length > 1 ? t('members.page.previous') : t('members.page.first')}
      </Button>
      : <span />}
    {cursor ? <Button {...marker} variant="quiet" disabled={busy} onClick={onNext}>{t('members.page.next')}</Button> : null}
    <p aria-live="polite" className="w-full text-center text-sm text-muted">{t('members.page.number', { n: String(stack.length + 1) })}</p>
  </div>;
}
