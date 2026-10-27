export type State = 'loading' | 'ready' | 'error';

export function newIdempotencyKey() {
  return Array.from(crypto.getRandomValues(new Uint8Array(16)), (byte) => byte.toString(16).padStart(2, '0')).join('');
}

/** Cursor and stack for the next page, pushing the current page's cursor. */
export function nextPage(stack: string[], current: string | undefined, next: string) {
  return { cursor: next, stack: [...stack, current ?? ''] };
}

/** Cursor and stack for the previous page; the empty cursor means the first page. */
export function previousPage(stack: string[]) {
  const rest = [...stack];
  const cursor = rest.pop() ?? '';
  return { cursor: cursor || undefined, stack: rest };
}
