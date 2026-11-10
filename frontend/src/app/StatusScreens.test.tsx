import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { I18nProvider } from '../i18n';
import { en } from '../i18n/en';
import { ErrorScreen, LoadingScreen, NotFoundScreen } from './StatusScreens';

function renderInApp(node: React.ReactNode) {
  return render(<I18nProvider>{node}</I18nProvider>);
}

describe('secondary status screens', () => {
  it('not found shows an eyebrow, the page heading and the household recovery link', () => {
    renderInApp(<NotFoundScreen />);
    expect(screen.getByRole('heading', { level: 1, name: en['app.notFound.title'] })).toBeVisible();
    expect(screen.getByText(en['app.notFound.eyebrow'])).toBeVisible();
    expect(screen.getByRole('link', { name: en['app.notFound.home'] })).toHaveAttribute('href', '/');
    expect(document.title).toBe(`${en['app.notFound.title']} — Tendo`);
  });

  it('signed-out not found offers sign-in through the same primary control', () => {
    renderInApp(<NotFoundScreen signedOut />);
    expect(screen.getByRole('link', { name: en['app.notFound.signIn'] })).toHaveAttribute('href', '/login');
  });

  it('error state is an alert-free page with a retry action and no V3 accent rule', async () => {
    const retry = vi.fn();
    const { container } = renderInApp(<ErrorScreen onRetry={retry} />);
    expect(screen.getByRole('heading', { level: 1, name: en['app.error.title'] })).toBeVisible();
    expect(screen.getByText(en['app.error.eyebrow'])).toBeVisible();
    expect(container.querySelector('.border-l-2')).toBeNull();
    await userEvent.click(screen.getByRole('button', { name: en['app.error.retry'] }));
    expect(retry).toHaveBeenCalledOnce();
  });

  it('loading keeps one polite status region and the shared panel composition', () => {
    renderInApp(<LoadingScreen />);
    const main = screen.getByRole('status');
    expect(main).toHaveTextContent(en['app.loading']);
    expect(screen.getByText(en['app.loading.eyebrow'])).toBeVisible();
    expect(within(main).queryByRole('heading')).not.toBeInTheDocument();
  });
});
