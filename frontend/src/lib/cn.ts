import { clsx, type ClassValue } from 'clsx';
import { extendTailwindMerge } from 'tailwind-merge';

/**
 * The type-scale size tokens declared as `--text-*` in the `@theme` block of `src/index.css` (`cn.test.ts` keeps the
 * two lists equal). Plain tailwind-merge does not know them, reads `text-body` as a text colour and drops it next to
 * `text-ink`, so components would silently fall back to the inherited font size.
 */
export const typeScaleTokens = ['display', 'h1', 'h2', 'row-title', 'body', 'meta', 'label', 'control', 'small', 'micro'] as const;

const twMerge = extendTailwindMerge({ extend: { theme: { text: [...typeScaleTokens] } } });

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}
