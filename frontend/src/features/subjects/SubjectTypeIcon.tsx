import type { ReactNode } from 'react';
import { Glyph } from '../../components/ui/glyph';
import type { SubjectType } from './subjectsApi';

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
