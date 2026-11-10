import type { ReactNode } from 'react';
import { Glyph } from '../../components/ui/glyph';
import type { ItemGroup } from './itemsApi';

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

/** Archived is a listing state, so it needs its own quiet box glyph rather than a workflow icon. */
export function ArchiveIcon({ className }: { className?: string }) {
  return <Glyph className={className ?? 'size-4'}><path d="M4 7.5h16M6.5 7.5V19h11V7.5M9.5 11.5h5" /><path d="M5 4.5h14v3H5z" /></Glyph>;
}
