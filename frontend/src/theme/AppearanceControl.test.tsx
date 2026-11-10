import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { I18nProvider } from '../i18n';
import { en } from '../i18n/en';
import { cs } from '../i18n/cs';
import { THEME_STORAGE_KEY } from './theme';
import { AppearanceControl } from './AppearanceControl';

function renderControl() {
  return render(<I18nProvider><main><AppearanceControl /></main></I18nProvider>);
}

describe('AppearanceControl', () => {
  beforeEach(() => {
    localStorage.clear();
    document.documentElement.removeAttribute('data-theme');
    localStorage.setItem('tendo.locale', 'en');
  });
  afterEach(() => {
    localStorage.clear();
    document.documentElement.removeAttribute('data-theme');
  });

  it('shows the three choices as a named group, with System (device) selected by default', () => {
    renderControl();
    const group = screen.getByRole('group', { name: en['account.appearance.title'] });
    expect(within(group).getAllByRole('button').map((button) => button.textContent)).toEqual([
      en['account.appearance.system'], en['account.appearance.light'], en['account.appearance.dark'],
    ]);
    expect(screen.getByRole('button', { name: en['account.appearance.system'] })).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByRole('button', { name: en['account.appearance.dark'] })).toHaveAttribute('aria-pressed', 'false');
    // Selection is not conveyed by colour alone: the chosen option shows a check glyph and a stronger surface.
    const selected = screen.getByRole('button', { name: en['account.appearance.system'] });
    const unselected = screen.getByRole('button', { name: en['account.appearance.dark'] });
    expect(selected).toHaveClass('bg-raised', 'text-ink');
    expect(selected.querySelector('[aria-hidden="true"]')).toHaveClass('opacity-100');
    expect(unselected.querySelector('[aria-hidden="true"]')).toHaveClass('opacity-0');
    expect(selected.querySelector('[aria-hidden="true"] svg')).not.toBeNull();
  });

  it('stores an explicit choice, applies it, and reflects it in aria-pressed', async () => {
    const user = userEvent.setup();
    renderControl();
    await user.click(screen.getByRole('button', { name: en['account.appearance.dark'] }));
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('dark');
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark');
    expect(screen.getByRole('button', { name: en['account.appearance.dark'] })).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByRole('button', { name: en['account.appearance.system'] })).toHaveAttribute('aria-pressed', 'false');
  });

  it('returns to the device choice without leaving a stored value behind', async () => {
    const user = userEvent.setup();
    localStorage.setItem(THEME_STORAGE_KEY, 'light');
    document.documentElement.setAttribute('data-theme', 'light');
    renderControl();
    expect(screen.getByRole('button', { name: en['account.appearance.light'] })).toHaveAttribute('aria-pressed', 'true');
    await user.click(screen.getByRole('button', { name: en['account.appearance.system'] }));
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBeNull();
    expect(document.documentElement.hasAttribute('data-theme')).toBe(false);
  });

  it('is operable by keyboard: Tab reaches each choice and Enter/Space selects it', async () => {
    const user = userEvent.setup();
    renderControl();
    await user.tab();
    expect(screen.getByRole('button', { name: en['account.appearance.system'] })).toHaveFocus();
    await user.tab();
    await user.keyboard('{Enter}');
    expect(document.documentElement.getAttribute('data-theme')).toBe('light');
    await user.tab();
    await user.keyboard(' ');
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark');
  });

  it('renders Czech labels from the Czech catalog', () => {
    localStorage.setItem('tendo.locale', 'cs');
    renderControl();
    expect(screen.getByRole('group', { name: cs['account.appearance.title'] })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: cs['account.appearance.system'] })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: cs['account.appearance.light'] })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: cs['account.appearance.dark'] })).toBeInTheDocument();
  });
});
