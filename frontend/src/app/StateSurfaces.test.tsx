import { cleanup, render, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { en } from '../i18n/en';
import { I18nProvider } from '../i18n';
import { emptyHome, HOUSEHOLD_ID, household, installFakeServer, json, session } from '../test/fakeServer';
import { App } from './App';
import { ErrorScreen, LoadingScreen, NotFoundScreen } from './StatusScreens';

/**
 * V5-07 slice: the calm list on every status, empty, error and loading state (UX.md "Interaction principles",
 * v5 audit §5 direction A). A state is plain content in the screen's own column: no decorative card frame, one
 * shared heading scale, one text loading line and one visual pattern for a failed list load. Behavior stays
 * covered by the per-screen suites; these assertions lock the shared presentation.
 */

type Routes = Parameters<typeof installFakeServer>[0];

const base = `/api/v1/households/${HOUSEHOLD_ID}`;
const itemId = '0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b80';
const member = { userId: session().userId, login: 'anna', role: 'admin' as const };
const problem = (status: number) => json(status, { type: 'about:blank', title: 'Unavailable', status });
/** A request that never settles: the screen stays in its loading state for the assertion. */
const held = () => new Promise<Response>(() => undefined);

function signedIn(extra: Routes = {}): Routes {
  return {
    'GET /api/v1/session': json(200, session()),
    [`GET ${base}`]: json(200, household),
    ...emptyHome,
    [`GET ${base}/members`]: json(200, { items: [member], nextCursor: null }),
    [`GET ${base}/invitations`]: json(200, { items: [], nextCursor: null }),
    [`GET ${base}/accounts`]: json(200, { items: [member], nextCursor: null }),
    ...extra,
  };
}

function app(path: string, routes: Routes) {
  installFakeServer(routes);
  window.history.replaceState(null, '', path);
  return render(<I18nProvider><App /></I18nProvider>);
}

function renderInApp(node: React.ReactNode) {
  return render(<I18nProvider>{node}</I18nProvider>);
}

/** What a decorative card frame looks like: a rounded raised box with centred content. */
const cardFrame = /rounded-2xl|bg-raised|bg-sand|text-center|items-center|justify-center/;

/** A flat section: no card frame, no box padding, no centring. A single rule above it is allowed. */
function expectFlatSection(element: Element) {
  expect(element.className, 'card frame on a flat state').not.toMatch(cardFrame);
  expect(element.classList.contains('border'), 'a frame border on a flat state').toBe(false);
  const boxPadding = [...element.classList].filter((token) => /^(sm:)?(p|px|py)-\d/.test(token));
  expect(boxPadding, 'box padding on a flat state').toEqual([]);
}

/** Nothing outside a dialog is a rounded card: dialogs keep their own frame by design. */
function expectNoCards(scope: ParentNode) {
  const cards = [...scope.querySelectorAll('.rounded-2xl')].filter((element) => element.closest('dialog') === null);
  expect(cards).toHaveLength(0);
}

/** One text treatment for a screen that is only waiting: a plain muted body line under one status role. */
function expectTextLoadingLine(line: Element) {
  const host = line.closest('[role="status"]');
  if (!host) throw new Error(`"${line.textContent}" is not in a status region`);
  expect(host.tagName, `"${line.textContent}" is text, not a skeleton`).toBe('P');
  expect(host).toHaveClass('text-body', 'text-muted');
}

/**
 * One visual pattern for a failed list load: the message, then the Retry button under it inside the same
 * error notice, with the same button variant everywhere. The live-region owner may be this notice or a
 * separate persistent host, so the callers assert their own announced role.
 */
function expectOneLoadError(message: string, role: 'alert' | null) {
  const main = screen.getByRole('main');
  const retries = within(main).getAllByRole('button', { name: en['app.error.retry'] });
  expect(retries, 'exactly one retry action in the failed state').toHaveLength(1);
  const retry = retries[0]!;
  const notice = retry.parentElement!;
  expect(notice).toHaveTextContent(message);
  expect(notice.getAttribute('role')).toBe(role);
  expect(notice).toHaveAttribute('aria-atomic', 'true');
  expect(retry).toHaveAttribute('data-variant', 'secondary');
  expect(retry).toHaveClass('min-h-11');
  // The message comes first and Retry sits under it, never beside it in a split row.
  const first = notice.firstElementChild as HTMLElement | null;
  expect(first, 'the error message is missing').not.toBeNull();
  expect(first).not.toBe(retry);
  expect(notice.lastElementChild).toBe(retry);
  expect(notice.className).not.toMatch(/justify-between|items-center/);
  return notice;
}

beforeEach(() => {
  window.history.replaceState(null, '', '/');
  vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['en-US']);
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  localStorage.removeItem('tendo.locale');
});

describe('text loading lines', () => {
  it('the boot loading screen is the same muted line, in the screen column rather than a panel', () => {
    const { container } = renderInApp(<LoadingScreen />);
    const status = screen.getByRole('status');
    expect(status.tagName).toBe('P');
    expect(status).toHaveClass('text-body', 'text-muted');
    expect(status).toHaveTextContent(en['app.loading']);
    expect(status.parentElement?.tagName).toBe('SECTION');
    expect(status.parentElement).toHaveClass('mx-auto', 'w-full', 'max-w-2xl');
    expect(container.querySelector('.rounded-2xl')).toBeNull();
  });

  it('a loading item is the same muted line', async () => {
    app(`/items/${itemId}`, signedIn({ [`GET ${base}/items/${itemId}`]: held }));
    expectTextLoadingLine(await screen.findByText(en['items.loading']));
  });

  it('a loading item list is the same muted line', async () => {
    app('/items', signedIn({ [`GET ${base}/items`]: held }));
    expectTextLoadingLine(await screen.findByText(en['items.loading']));
  });

  it('a loading people list is the same muted line', async () => {
    app('/people', signedIn({ [`GET ${base}/subjects`]: held }));
    await screen.findAllByText(en['subjects.loading']);
    for (const line of screen.getAllByText(en['subjects.loading'])) expectTextLoadingLine(line);
  });

  it('a loading Members list is the same muted line', async () => {
    app('/members', signedIn({ [`GET ${base}/members`]: held }));
    await screen.findAllByText(en['members.loading']);
    for (const line of screen.getAllByText(en['members.loading'])) expectTextLoadingLine(line);
  });

  it('the Members role probe waits on the same muted line', async () => {
    // The list answers without the viewer, so the role is still unknown: the screen keeps one line for it.
    app('/members', signedIn({ [`GET ${base}/members`]: json(200, { items: [{ userId: 'other', login: 'petr', role: 'member' }], nextCursor: null }) }));
    await screen.findAllByText(en['members.loading']);
    for (const line of screen.getAllByText(en['members.loading'])) expectTextLoadingLine(line);
  });
});

describe('load errors', () => {
  it('a failed Home load is one notice with the message and Retry under it', async () => {
    app('/', signedIn({ [`GET ${base}/items/summary`]: problem(503) }));
    await screen.findAllByText(en['app.error.body']);
    expectOneLoadError(en['app.error.body'], 'alert');
  });

  it('a failed All items load is one notice with the message and Retry under it', async () => {
    app('/items', signedIn({ [`GET ${base}/items/summary`]: problem(503) }));
    await screen.findAllByText(en['app.error.body']);
    const notice = expectOneLoadError(en['app.error.body'], null);
    // The screen announces through its own persistent polite host; the visible notice stays presentational.
    const host = screen.getByRole('main').querySelector('p[role="status"][aria-atomic="true"]');
    expect(host).toHaveTextContent(en['app.error.body']);
    expect(within(notice).getByRole('button', { name: en['app.error.retry'] })).toHaveAttribute('data-all-items-retry');
  });

  it('a failed Members load is one notice with the message and Retry under it', async () => {
    app('/members', signedIn({ [`GET ${base}/members`]: problem(503) }));
    await screen.findAllByText(en['members.error.load']);
    expectOneLoadError(en['members.error.load'], 'alert');
  });

  it('a failed Admin load is one notice with the message and Retry under it', async () => {
    app('/admin', signedIn({ [`GET ${base}/accounts`]: problem(503) }));
    await screen.findAllByText(en['admin.error.load']);
    expectOneLoadError(en['admin.error.load'], 'alert');
  });

  it('a failed People load is one notice with the message and Retry under it', async () => {
    app('/people', signedIn({ [`GET ${base}/subjects`]: problem(503) }));
    await screen.findAllByText(en['app.error.body']);
    expectOneLoadError(en['app.error.body'], null);
    // Announced by the persistent polite host, which stays mounted across the retry.
    const host = document.querySelector<HTMLElement>('main p.sr-only[aria-live="polite"][aria-atomic="true"]');
    expect(host).not.toBeNull();
    expect(host).toHaveTextContent(`${en['app.error.title']} ${en['app.error.body']}`);
  });
});

describe('empty states', () => {
  it('the empty Home is a plain section under the page title, not a card', async () => {
    app('/', signedIn());
    const heading = await screen.findByRole('heading', { level: 2, name: en['home.empty.title'] });
    expect(heading).toHaveClass('text-h2', 'text-ink');
    const section = heading.parentElement!;
    expect(section.tagName).toBe('SECTION');
    expectFlatSection(section);
    expectNoCards(section);
    expect(heading.nextElementSibling).toHaveClass('text-body', 'text-muted', 'max-w-prose');
  });

  it('an empty All items is the same plain section', async () => {
    app('/items', signedIn());
    const heading = await screen.findByRole('heading', { level: 2, name: en['allItems.none'] });
    expect(heading).toHaveClass('text-h2', 'text-ink');
    const section = heading.parentElement!;
    expectFlatSection(section);
    // Nothing to explain yet: the section carries one way to start, on the action row under the title.
    const action = heading.nextElementSibling as HTMLElement;
    expect(action).toHaveClass('mt-4');
    expect(action.querySelector('a')).toHaveAttribute('href', '/');
  });

  it('an All items search with no match is the same plain section', async () => {
    app('/items?q=zzz', signedIn());
    const heading = await screen.findByRole('heading', { level: 2, name: en['allItems.noMatch'] });
    expect(heading).toHaveClass('text-h2', 'text-ink');
    const section = heading.parentElement!;
    expectFlatSection(section);
    // The one Clear action stays in that section, on the action row under the message.
    expect(section.querySelector('[data-all-items-clear]')).not.toBeNull();
  });

  it('an empty People screen is the same plain section', async () => {
    app('/people', signedIn({ [`GET ${base}/subjects`]: json(200, { items: [], nextCursor: null }) }));
    const heading = await screen.findByRole('heading', { level: 2, name: en['subjects.empty.title'] });
    expect(heading).toHaveClass('text-h2', 'text-ink');
    expectFlatSection(heading.parentElement!);
  });
});

describe('status screens', () => {
  it('not found is plain content in the screen column, not a panel', () => {
    const { container } = renderInApp(<NotFoundScreen />);
    const heading = screen.getByRole('heading', { level: 1, name: en['app.notFound.title'] });
    expect(heading).toHaveClass('text-h1', 'font-semibold', 'text-ink');
    const column = heading.parentElement!;
    expect(column.tagName).toBe('SECTION');
    expect(column).toHaveClass('settle', 'mx-auto', 'w-full', 'max-w-2xl');
    expect(column.firstElementChild).toBe(heading);
    expect(container.querySelector('.rounded-2xl')).toBeNull();
    expect(container.querySelector('.bg-raised')).toBeNull();
    // The recovery action stays a 44 px target directly in the column.
    expect(screen.getByRole('link', { name: en['app.notFound.home'] })).toHaveClass('min-h-11');
  });

  it('signed-out not found keeps the top-aligned column and its sign-in action', () => {
    renderInApp(<NotFoundScreen signedOut />);
    const column = screen.getByRole('heading', { level: 1 }).parentElement!;
    expect(column).toHaveClass('settle', 'mx-auto', 'w-full', 'max-w-2xl', 'lg:mt-10');
    expect(column).not.toHaveClass('min-h-[55vh]');
    expect(screen.getByRole('link', { name: en['app.notFound.signIn'] })).toHaveAttribute('href', '/login');
  });

  it('the error screen is plain content in the column with its Retry action', () => {
    const retry = vi.fn();
    const { container } = renderInApp(<ErrorScreen onRetry={retry} />);
    const heading = screen.getByRole('heading', { level: 1, name: en['app.error.title'] });
    expect(heading).toHaveClass('text-h1');
    expect(heading.parentElement?.tagName).toBe('SECTION');
    expect(heading.parentElement?.firstElementChild).toBe(heading);
    expect(container.querySelector('.rounded-2xl')).toBeNull();
    expect(screen.getByRole('button', { name: en['app.error.retry'] })).toHaveClass('min-h-11');
  });
});

describe('no-household states', () => {
  it('the signed-in no-household screen is flat sections separated by rules', async () => {
    app('/', { 'GET /api/v1/session': json(200, session(false)) });
    const how = await screen.findByRole('heading', { level: 2, name: en['home.noHousehold.how.title'] });
    expect(how).toHaveClass('text-h2', 'text-ink');
    const howBlock = how.parentElement!;
    expectFlatSection(howBlock);
    expect(howBlock).toHaveClass('border-t', 'border-line');
    expect(screen.getByText(en['home.noHousehold.how.body'])).toHaveClass('text-body', 'text-muted', 'max-w-prose');
    const account = screen.getByRole('region', { name: en['home.noHousehold.discard.title'] });
    expectFlatSection(account);
    expect(account).toHaveClass('border-t', 'border-line');
    expect(within(account as HTMLElement).getByRole('button', { name: en['home.noHousehold.discard.action'] })).toBeVisible();
    expectNoCards(screen.getByRole('main'));
  });

  it('People without a household shows a plain section and keeps the announcement host', async () => {
    app('/people', signedIn({ [`GET ${base}/subjects`]: problem(404) }));
    const heading = await screen.findByRole('heading', { level: 2, name: en['home.noHousehold.title'] });
    expect(heading).toHaveClass('text-h2', 'text-ink');
    const block = heading.parentElement!;
    expectFlatSection(block);
    expect(heading.nextElementSibling).toHaveClass('text-body', 'text-muted', 'max-w-prose');
    // The same persistent polite host stays mounted, and no retry loop is offered for a missing household.
    expect(document.querySelector('main p.sr-only[aria-live="polite"][aria-atomic="true"]')).toHaveTextContent('');
    expect(screen.queryByRole('button', { name: en['app.error.retry'] })).not.toBeInTheDocument();
    expectNoCards(block);
  });
});
