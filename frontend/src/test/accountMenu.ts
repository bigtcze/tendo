import { expect } from 'vitest';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

/** Opens the top bar's account menu the way a reader does and returns its panel. */
export async function openAccountMenu(label: RegExp = /^(Account|Účet): /) {
  // The boot screen has a banner of its own, so wait for the trigger itself rather than the first banner.
  const trigger = await screen.findByRole('button', { name: label });
  expect(trigger.closest('header')).not.toBeNull();
  if (trigger.getAttribute('aria-expanded') !== 'true') await userEvent.click(trigger);
  const panel = document.getElementById(trigger.getAttribute('aria-controls')!)!;
  await waitFor(() => expect(panel).toBeVisible());
  return panel;
}

/** The menu's Sign out button, found only after opening the menu. */
export async function findSignOut(name = 'Sign out') {
  return within(await openAccountMenu()).getByRole('button', { name });
}
