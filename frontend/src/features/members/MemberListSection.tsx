import type { MouseEvent, RefObject } from 'react';
import type { Translate } from '../../i18n';
import type { Member } from './membersApi';
import { MembersPager } from './MembersPager';

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
  return <section role="region" aria-labelledby="members-list-heading">
    <h2 id="members-list-heading" ref={headingRef} tabIndex={-1} className="font-display text-h2 outline-none">{t('members.list.title')}</h2>
    <ul aria-labelledby="members-list-heading" className="mt-1 grid gap-1.5 sm:mt-3 sm:grid-cols-2 sm:gap-2">{members.map((member) => (
      <li key={member.userId} className="flex min-h-14 items-center justify-between gap-2 rounded-xl border border-line bg-raised px-3 py-2 sm:min-h-16 sm:gap-4 sm:px-4">
        <span className="min-w-0 break-words text-row-title text-ink">{member.login}{member.userId === userId ? ` (${t('members.you')})` : ''}</span>
        <span className="shrink-0 rounded-md bg-sand px-2 py-1 text-sm text-ink">{t(member.role === 'owner' ? 'members.role.owner' : 'members.role.member')}</span>
      </li>))}</ul>
    <p className="mt-0.5 max-w-prose text-meta text-muted sm:mt-2">{t('members.roles.explained')}</p>
    {stack.length > 0 || cursor
      ? <MembersPager kind="member" stack={stack} cursor={cursor} busy={busy} onPrevious={onPrevious} onNext={onNext} t={t} />
      : null}
  </section>;
}
