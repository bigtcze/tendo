import { render } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { I18nProvider } from '../../i18n';
import { session } from '../../test/fakeServer';
import { HomeScreen } from './HomeScreen';

describe('HomeScreen', () => {
  afterEach(() => { vi.restoreAllMocks(); });

  it('refuses to render outside the household provider instead of quietly falling back', () => {
    // A missing provider is a programming error, not a household state: React reports it on the console.
    vi.spyOn(console, 'error').mockImplementation(() => {});
    expect(() => render(
      <I18nProvider>
        <HomeScreen
          session={session()}
          onSignedOut={() => {}}
          onOpenPeople={() => {}}
          onOpenItem={() => {}}
          onOpenAllItems={() => {}}
        />
      </I18nProvider>,
    )).toThrow('HomeScreen must render inside the HouseholdData provider');
  });
});
