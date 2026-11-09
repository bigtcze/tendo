import type { ReactNode } from 'react';
import type { SubjectType } from '../subjects/subjectsApi';
import type { ItemGroup } from './itemsApi';

function Glyph({ children, className = 'size-5' }: { children: ReactNode; className?: string }) {
  return (
    <svg aria-hidden="true" focusable="false" viewBox="0 0 24 24" fill="none" stroke="currentColor"
      strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round" className={className}>
      {children}
    </svg>
  );
}

const subjectPaths: Record<SubjectType, ReactNode> = {
  person: <><circle cx="12" cy="8" r="3.5" /><path d="M5 20c.7-3.9 3.5-5.9 7-5.9s6.3 2 7 5.9" /></>,
  home: <><path d="M4 10.5 12 4l8 6.5" /><path d="M6 9v10h12V9" /><path d="M10 19v-5h4v5" /></>,
  vehicle: <><path d="M4 16.5v-3l2-4.5h12l2 4.5v3" /><path d="M4 13.5h16" /><circle cx="7.5" cy="17" r="1.5" /><circle cx="16.5" cy="17" r="1.5" /></>,
  pet: <><circle cx="7" cy="10" r="1.9" /><circle cx="12" cy="7.5" r="1.9" /><circle cx="17" cy="10" r="1.9" /><path d="M8.5 13.4c1-.6 2.2-.9 3.5-.9s2.5.3 3.5.9c1.4.8 2.2 2 2.2 3.4 0 1.5-1.1 2.5-2.6 2.5-.7 0-1.3-.2-1.8-.2s-1.1.2-1.8.2c-1.5 0-2.6-1-2.6-2.5 0-1.4.8-2.6 1.6-3.4Z" /></>,
  custom: <><path d="M4.5 8.5 12 4l7.5 4.5v7L12 20l-7.5-4.5z" /><path d="M4.5 8.5 12 13l7.5-4.5M12 13v7" /></>,
};

/** Real subject type only: person, home, vehicle, pet or the generic custom thing. Never an invented topic category. */
export function SubjectTypeIcon({ type }: { type: SubjectType }) {
  return <Glyph>{subjectPaths[type]}</Glyph>;
}

const groupPaths: Record<ItemGroup, ReactNode> = {
  needs: <><path d="M12 8v5" /><path d="M12 16.5h.01" /><circle cx="12" cy="12" r="8.5" /></>,
  inProgress: <><circle cx="12" cy="12" r="8.5" /><path d="M12 7.5V12l3 2" /></>,
  waiting: <><path d="M9 5.5v13M15 5.5v13" /></>,
  upcoming: <><rect x="4" y="5.5" width="16" height="14" rx="2" /><path d="M4 10h16M8 3.5v4M16 3.5v4" /></>,
  paused: <><circle cx="12" cy="12" r="8.5" /><path d="M10 9.5v5M14 9.5v5" /></>,
};

export function GroupStatusIcon({ group, className }: { group: ItemGroup; className?: string }) {
  return <Glyph className={className}>{groupPaths[group]}</Glyph>;
}

export function AlertIcon({ className }: { className?: string }) {
  return <Glyph className={className ?? 'size-4'}><path d="M12 8v5" /><path d="M12 16.5h.01" /><circle cx="12" cy="12" r="8.5" /></Glyph>;
}

export function CalendarIcon({ className }: { className?: string }) {
  return <Glyph className={className ?? 'size-4'}><rect x="4" y="5.5" width="16" height="14" rx="2" /><path d="M4 10h16M8 3.5v4M16 3.5v4" /></Glyph>;
}

export function CheckIcon({ className }: { className?: string }) {
  return <Glyph className={className ?? 'size-5'}><path d="m5 12.5 4.5 4.5L19 7" /></Glyph>;
}

export function PlusIcon({ className }: { className?: string }) {
  return <Glyph className={className ?? 'size-5'}><path d="M12 5v14M5 12h14" /></Glyph>;
}
