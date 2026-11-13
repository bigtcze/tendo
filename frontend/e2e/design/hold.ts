import type { Page } from '@playwright/test';

/**
 * Holds one matching read so a loading state can be photographed, then lets it finish before the next state
 * navigates. Holding the request across a navigation would leave a route handler resolving on a document that no
 * longer exists, so `hold()` always settles the previous hold first and `letGo()` waits for the reply.
 */
export class HeldRequest {
  private readonly page: Page;
  private readonly pattern: string;
  private release: (() => void) | null = null;
  private finished: Promise<void> = Promise.resolve();

  constructor(page: Page, pattern: string) {
    this.page = page;
    this.pattern = pattern;
  }

  /** Releases any previous hold, installs a fresh one, and returns while the next matching read stays open. */
  async hold(): Promise<void> {
    await this.letGo();
    let release!: () => void;
    const gate = new Promise<void>((resolve) => { release = resolve; });
    let finished!: () => void;
    this.finished = new Promise<void>((resolve) => { finished = resolve; });
    await this.page.route(this.pattern, async (route) => {
      if (route.request().method() !== 'GET') return route.fallback();
      try {
        await gate;
        return await route.fallback();
      } finally {
        finished();
      }
    });
    this.release = release;
  }

  /** Lets the held read complete, waits for the screen to receive it, and removes the route. */
  async letGo(): Promise<void> {
    const release = this.release;
    if (!release) return;
    this.release = null;
    release();
    await this.finished;
    await this.page.unroute(this.pattern);
  }
}
