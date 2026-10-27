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
  return <section>
    <h2 ref={headingRef} tabIndex={-1} className="font-display text-2xl outline-none">{t('members.list.title')}</h2>
    <ul className="mt-3 divide-y divide-line rounded-2xl border border-line bg-white/70">{members.map((member) => (
      <li key={member.userId} className="flex flex-wrap items-center justify-between gap-2 px-4 py-3">
        <span className="min-w-0 break-all">{member.login}{member.userId === userId ? ` (${t('members.you')})` : ''}</span>
        <span className="text-sm text-muted">{t(member.role === 'owner' ? 'members.role.owner' : 'members.role.member')}</span>
      </li>))}</ul>
    {stack.length > 0 || cursor
      ? <MembersPager kind="member" stack={stack} cursor={cursor} busy={busy} onPrevious={onPrevious} onNext={onNext} t={t} />
      : null}
  </section>;
}
