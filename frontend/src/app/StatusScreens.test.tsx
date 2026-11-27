import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { I18nProvider } from '../i18n';
import { cs } from '../i18n/cs';
import { en } from '../i18n/en';
import { ErrorScreen, LoadingScreen, NotFoundScreen } from './StatusScreens';

function renderInApp(node: React.ReactNode) {
  return render(<I18nProvider>{node}</I18nProvider>);
}

describe('secondary status screens', () => {
  it('not found shows the page heading and the household recovery link, with no shell eyebrow', () => {
    renderInApp(<NotFoundScreen />);
    const heading = screen.getByRole('heading', { level: 1, name: en['app.notFound.title'] });
    expect(heading).toBeVisible();
    // The shell names these screens with their heading alone: the panel opens on the heading itself.
    expect(heading.parentElement?.firstElementChild).toBe(heading);
    const home = screen.getByRole('link', { name: en['app.notFound.home'] });
    expect(home).toHaveAttribute('href', '/');
    // The way out is drawn and marked as the screen's one primary action (#459 F22), like the reset dead end.
    expect(home).toHaveAttribute('data-variant', 'primary');
    expect(home).toHaveClass('bg-accent', 'text-on-accent');
    expect(document.title).toBe(`${en['app.notFound.title']} — Tendo`);
  });

  it('signed-out not found offers sign-in through the same primary control', () => {
    const { container } = renderInApp(<NotFoundScreen signedOut />);
    const signIn = screen.getByRole('link', { name: en['app.notFound.signIn'] });
    expect(signIn).toHaveAttribute('href', '/login');
    expect(signIn).toHaveAttribute('data-variant', 'primary');
    expect(signIn).toHaveClass('bg-accent', 'text-on-accent');
    expect(container.querySelectorAll('[data-variant="primary"]')).toHaveLength(1);
  });

  // #459 review: the signed-out states belong to the sign-in family (login, reset link, OIDC continue), which centres
  // its title, sentence and one action in a narrow column; signed out, not found and the connection error were the only
  // left-aligned pages in it.
  it.each([
    ['not found', () => <NotFoundScreen signedOut />, en['app.notFound.title']],
    ['connection error', () => <ErrorScreen signedOut onRetry={() => undefined} />, en['app.error.title']],
  ])('signed-out %s is centred like the rest of the sign-in pages', (_name, node, title) => {
    const { container } = renderInApp(node());
    const heading = screen.getByRole('heading', { level: 1, name: title });
    const column = heading.parentElement!;
    expect(column).toHaveClass('mx-auto', 'w-full', 'max-w-md', 'text-center', 'text-balance');
    // The sentence wraps with text-pretty rather than being balanced, so its last line is never a single word.
    expect(heading.nextElementSibling).toHaveClass('text-pretty');
    const action = container.querySelector('[data-variant="primary"]')!;
    expect(action.parentElement).toHaveClass('flex', 'justify-center');
  });

  it('signed-in states keep the start-aligned household column', () => {
    renderInApp(<NotFoundScreen />);
    const column = screen.getByRole('heading', { level: 1 }).parentElement!;
    expect(column).toHaveClass('max-w-2xl');
    expect(column).not.toHaveClass('text-center');
  });

  // #459 review: the sentence told people to "go back", but the page offers no back action; it must only point at the
  // way out the page actually draws, which differs signed in (household) and signed out (sign in).
  it.each([
    ['en', false, en],
    ['en', true, en],
    ['cs', false, cs],
    ['cs', true, cs],
  ] as const)('%s not found (signed out: %s) names only the action it offers', (locale, signedOut, catalog) => {
    localStorage.setItem('tendo.locale', locale);
    try {
      renderInApp(<NotFoundScreen signedOut={signedOut} />);
      const body = screen.getByRole('heading', { level: 1 }).nextElementSibling!;
      expect(body.textContent).toBe(catalog[signedOut ? 'app.notFound.bodySignedOut' : 'app.notFound.body']);
      expect(body.textContent).not.toMatch(/go back|zpět/i);
    } finally {
      localStorage.removeItem('tendo.locale');
    }
  });

  it('error state is an alert-free page with a retry action and no V3 accent rule', async () => {
    const retry = vi.fn();
    const { container } = renderInApp(<ErrorScreen onRetry={retry} />);
    const heading = screen.getByRole('heading', { level: 1, name: en['app.error.title'] });
    expect(heading).toBeVisible();
    expect(heading.parentElement?.firstElementChild).toBe(heading);
    expect(container.querySelector('.border-l-2')).toBeNull();
    await userEvent.click(screen.getByRole('button', { name: en['app.error.retry'] }));
    expect(retry).toHaveBeenCalledOnce();
  });

  it('loading keeps one polite status region and the shared panel composition', () => {
    renderInApp(<LoadingScreen />);
    const main = screen.getByRole('status');
    expect(main).toHaveTextContent(en['app.loading']);
    // The status is the panel's first child: no eyebrow line precedes it.
    expect(main.parentElement?.firstElementChild).toBe(main);
    expect(within(main).queryByRole('heading')).not.toBeInTheDocument();
  });
});
