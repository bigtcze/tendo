/**
 * The item detail is a page, not a card: one page-title type for every state of the screen, and one quiet
 * back link that stays a real target on a phone. Kept in its own module so the ready screen and the
 * loading/failed/gone states share the exact same classes without importing each other.
 */
export const DETAIL_TITLE_CLASS = 'min-w-0 text-pretty [overflow-wrap:anywhere] text-h1 font-semibold leading-tight text-ink';

export const DETAIL_BACK_LINK_CLASS = '-ml-3 inline-flex min-h-11 max-w-full items-center whitespace-normal break-words rounded-xl px-3 text-left text-muted '
  + 'hover:bg-sand hover:text-ink focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent';
