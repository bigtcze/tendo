import type { Page } from '@playwright/test';

interface Deferred {
  promise: Promise<void>;
  resolve: () => void;
}

/** A promise plus its resolver, without a definite-assignment assertion on a `let`. */
function deferred(): Deferred {
  const handle: { resolve?: () => void } = {};
  const promise = new Promise<void>((resolve) => { handle.resolve = resolve; });
  return { promise, resolve: () => { handle.resolve?.(); } };
}

/** A handed-back route must not hang the capture matrix; ten seconds is far longer than a local read. */
const HAND_BACK_TIMEOUT_MS = 10_000;

/**
 * Holds one matching read so a loading state can be photographed, then lets it finish before the next state
 * navigates. Holding a request across a navigation would leave a route handler resolving on a document that no
 * longer exists, so `hold()` always settles the previous hold first.
 */
export class HeldRequest {
  private readonly page: Page;
  private readonly pattern: string;
  private release: (() => void) | null = null;
  private handBack: Promise<void> = Promise.resolve();

  constructor(page: Page, pattern: string) {
    this.page = page;
    this.pattern = pattern;
  }

  /** Releases any previous hold, installs a fresh one, and returns while the next matching read stays open. */
  async hold(): Promise<void> {
    await this.letGo();
    const gate = deferred();
    const handedBack = deferred();
    this.handBack = handedBack.promise;
    await this.page.route(this.pattern, async (route) => {
      if (route.request().method() !== 'GET') return route.fallback();
      try {
        await gate.promise;
        return await route.fallback();
      } finally {
        handedBack.resolve();
      }
    });
    this.release = gate.resolve;
  }

  /**
   * Releases the held read and waits, bounded, until the route handler has handed the request back and the route is
   * removed. This does not wait for the screen to render the reply: only the caller's own assertion knows what that
   * screen should look like.
   */
  async letGo(): Promise<void> {
    const release = this.release;
    if (!release) return;
    this.release = null;
    release();
    let timeout: ReturnType<typeof setTimeout> | undefined;
    const bounded = new Promise<boolean>((resolve) => {
      timeout = setTimeout(() => resolve(false), HAND_BACK_TIMEOUT_MS);
    });
    const handedBack = await Promise.race([this.handBack.then(() => true), bounded]);
    clearTimeout(timeout);
    if (!handedBack) {
      throw new Error(`${this.pattern} was not handed back within ${HAND_BACK_TIMEOUT_MS} ms after release`);
    }
    await this.page.unroute(this.pattern);
  }
}
