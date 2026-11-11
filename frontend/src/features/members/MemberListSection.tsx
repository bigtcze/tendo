import type { MouseEvent, RefObject } from 'react';
import type { Translate } from '../../i18n';
import type { Member } from './membersApi';
import { CursorPager } from '../../components/ui/cursor-pager';

function initialOf(login: string): string {
  return login.trim().charAt(0).toLocaleUpperCase() || '?';
}

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
    <h2 id="members-list-heading" ref={headingRef} tabIndex={-1} className="text-h2 text-ink outline-none">{t('members.list.title')}</h2>
    <ul aria-labelledby="members-list-heading" className="mt-3 divide-y divide-line overflow-hidden rounded-2xl border border-line bg-raised">{members.map((member) => (
      <li key={member.userId} className="flex min-h-16 items-center gap-3 px-4 py-3">
        <span aria-hidden="true" data-member-avatar className="grid size-10 shrink-0 place-items-center rounded-full bg-nav-selected text-base font-semibold text-ink">{initialOf(member.login)}</span>
        <div className="flex min-w-0 flex-1 flex-col gap-1 sm:flex-row sm:flex-wrap sm:items-center sm:gap-x-3">
          <span data-member-login className="min-w-0 break-words text-row-title font-semibold text-ink">{member.login}{member.userId === userId ? <span className="font-normal text-muted"> ({t('members.you')})</span> : null}</span>
          <span className="self-start rounded-full bg-sand px-2.5 py-1 text-small font-semibold text-muted sm:ml-auto sm:shrink-0 sm:self-center">{t(member.role === 'admin' ? 'members.role.admin' : 'members.role.member')}</span>
        </div>
      </li>))}</ul>
    <p className="mt-3 max-w-prose text-small text-muted">{t('members.roles.explained')}</p>
    {stack.length > 0 || cursor
      ? <CursorPager kind="member" stack={stack} cursor={cursor} busy={busy} onPrevious={onPrevious} onNext={onNext} t={t} />
      : null}
  </section>;
}
