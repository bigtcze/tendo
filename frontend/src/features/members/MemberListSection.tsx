import type { MouseEvent, RefObject } from 'react';
import type { Translate } from '../../i18n';
import type { Member } from './membersApi';
import { CursorPager } from '../../components/ui/cursor-pager';

/**
 * The household's people as one flat divided list: the login (with "you") and its role as plain muted text, so no
 * row carries an avatar tile or a pill. The explanation under the list stays because it says what the roles mean.
 */
export function MemberListSection({ members, userId, headingRef, stack, cursor, busy, onPrevious, onNext, t }: {
  members: Member[];
  userId: string;
  headingRef: RefObject<HTMLHeadingElement | null>;
  stack: string[];
  cursor: string | null;
  busy: boolean;
  onPrevious: (event: MouseEvent<HTMLButtonElement>) => void;
  onNext: (event: MouseEvent<HTMLButtonElement>) => void;
  t: Translate;
}) {
  return <section role="region" aria-labelledby="members-list-heading" className="min-w-0">
    <h2 id="members-list-heading" ref={headingRef} tabIndex={-1} className="text-h2 text-ink outline-none">{t('members.list.title')}</h2>
    <ul aria-labelledby="members-list-heading" className="mt-2 divide-y divide-line border-t border-line">{members.map((member) => (
      <li key={member.userId} className="flex min-w-0 flex-wrap items-baseline gap-x-3 gap-y-0.5 py-3">
        <span data-member-login className="min-w-0 break-words text-row-title font-semibold text-ink">{member.login}{member.userId === userId ? <span className="font-normal text-muted"> ({t('members.you')})</span> : null}</span>
        <span data-member-role className="text-meta text-muted">{t(member.role === 'admin' ? 'members.role.admin' : 'members.role.member')}</span>
      </li>))}</ul>
    <p className="mt-3 max-w-prose text-small text-muted">{t('members.roles.explained')}</p>
    {stack.length > 0 || cursor
      ? <CursorPager kind="member" stack={stack} cursor={cursor} busy={busy} onPrevious={onPrevious} onNext={onNext} t={t} />
      : null}
  </section>;
}
