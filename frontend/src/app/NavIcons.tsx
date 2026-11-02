import type { ReactNode } from 'react';
import type { HouseholdRoute } from './HouseholdNavigation';

/** Decorative navigation glyphs: one consistent 20px outline set; the link text always names the destination. */
function Glyph({ children }: { children: ReactNode }) {
  return (
    <svg aria-hidden="true" focusable="false" viewBox="0 0 24 24" fill="none" stroke="currentColor"
      strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round" className="size-5 shrink-0">
      {children}
    </svg>
  );
}

const paths: Record<Exclude<HouseholdRoute, 'item'>, ReactNode> = {
  home: <><path d="M4 10.5 12 4l8 6.5" /><path d="M6 9v10h12V9" /><path d="M10 19v-5h4v5" /></>,
  allItems: <><path d="M9 6h11M9 12h11M9 18h11" /><circle cx="4.5" cy="6" r="1" /><circle cx="4.5" cy="12" r="1" /><circle cx="4.5" cy="18" r="1" /></>,
  people: <>
    <circle cx="9" cy="8" r="3.5" />
    <path d="M2.5 20c.6-3.6 3.2-5.5 6.5-5.5s5.9 1.9 6.5 5.5" />
    <path d="M16 4.8a3.5 3.5 0 0 1 0 6.4M18 14.8c1.9.8 3.1 2.5 3.5 5.2" />
  </>,
  members: <><path d="M3 20V9.5L12 4l9 5.5V20" /><circle cx="12" cy="11.5" r="2.5" /><path d="M7.5 20c.5-2.8 2.2-4.2 4.5-4.2s4 1.4 4.5 4.2" /></>,
  account: <><circle cx="12" cy="8" r="4" /><path d="M4 20.5c.8-4 4-6.2 8-6.2s7.2 2.2 8 6.2" /></>,
};

export function NavIcon({ route }: { route: Exclude<HouseholdRoute, 'item'> }) {
  return <Glyph>{paths[route]}</Glyph>;
}
