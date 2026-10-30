import { act, renderHook } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { createOwner, login } from './sessionApi';
import { useOnboardingForm } from './useOnboardingForm';

vi.mock('./sessionApi', () => ({
  createOwner: vi.fn(),
  login: vi.fn(),
}));

const mockedCreateOwner = vi.mocked(createOwner);
const mockedLogin = vi.mocked(login);

function deferred<T>() {
  const resolvers: Array<(value: T) => void> = [];
  const promise = new Promise<T>((complete) => { resolvers.push(complete); });
  return {
    promise,
    resolve(value: T) {
      const complete = resolvers[0];
      if (complete) complete(value);
    },
  };
}

describe('useOnboardingForm', () => {
  beforeEach(() => {
    vi.resetAllMocks();
  });

  it('guards against duplicate submissions before the request resolves', async () => {
    let release!: (result: { kind: 'rateLimited' }) => void;
    mockedCreateOwner.mockReturnValue(new Promise((resolve) => { release = resolve; }));
    const { result } = renderHook(() => useOnboardingForm({
      onSignedIn: vi.fn(),
      onSetupComplete: vi.fn(),
    }));
    act(() => {
      result.current.setSetupCode('token');
      result.current.setHouseholdName('Home');
      result.current.setLoginName('login');
      result.current.setPassword('password');
    });
    const event = { preventDefault: vi.fn() } as never;

    let first!: Promise<void>;
    let second!: Promise<void>;
    act(() => {
      first = result.current.submit(event);
      second = result.current.submit(event);
    });
    expect(mockedCreateOwner).toHaveBeenCalledOnce();
    await act(async () => { release({ kind: 'rateLimited' }); await Promise.all([first, second]); });
  });

  it('does not continue setup after unmount while createOwner is pending', async () => {
    const setup = deferred<{ kind: 'ok' }>();
    mockedCreateOwner.mockReturnValue(setup.promise);
    const onSignedIn = vi.fn();
    const onSetupComplete = vi.fn();
    const input = document.createElement('input');
    input.id = 'setup-code';
    document.body.append(input);
    const focus = vi.spyOn(input, 'focus');
    const select = vi.spyOn(input, 'select');
    const { result, unmount } = renderHook(() => useOnboardingForm({ onSignedIn, onSetupComplete }));
    act(() => {
      result.current.setSetupCode('token');
      result.current.setHouseholdName('Home');
      result.current.setLoginName('login');
      result.current.setPassword('password');
    });
    const submissions: Promise<void>[] = [];
    act(() => { submissions.push(result.current.submit({ preventDefault: vi.fn() } as never)); });
    const submission = submissions[0];
    if (!submission) throw new Error('submission was not started');

    unmount();
    await act(async () => { setup.resolve({ kind: 'ok' }); await submission; });

    expect(mockedLogin).not.toHaveBeenCalled();
    expect(onSignedIn).not.toHaveBeenCalled();
    expect(onSetupComplete).not.toHaveBeenCalled();
    expect(focus).not.toHaveBeenCalled();
    expect(select).not.toHaveBeenCalled();
    input.remove();
  });

  it('does not handle a login result after unmount', async () => {
    const signIn = deferred<{ kind: 'unavailable' }>();
    mockedCreateOwner.mockResolvedValue({ kind: 'ok' });
    mockedLogin.mockReturnValue(signIn.promise);
    const onSignedIn = vi.fn();
    const onSetupComplete = vi.fn();
    const input = document.createElement('input');
    input.id = 'setup-code';
    document.body.append(input);
    const focus = vi.spyOn(input, 'focus');
    const select = vi.spyOn(input, 'select');
    const { result, unmount } = renderHook(() => useOnboardingForm({ onSignedIn, onSetupComplete }));
    act(() => {
      result.current.setSetupCode('token');
      result.current.setHouseholdName('Home');
      result.current.setLoginName('login');
      result.current.setPassword('password');
    });
    const submissions: Promise<void>[] = [];
    act(() => { submissions.push(result.current.submit({ preventDefault: vi.fn() } as never)); });
    const submission = submissions[0];
    if (!submission) throw new Error('submission was not started');
    await act(async () => { await Promise.resolve(); });
    expect(mockedLogin).toHaveBeenCalledOnce();

    unmount();
    await act(async () => { signIn.resolve({ kind: 'unavailable' }); await submission; });

    expect(onSignedIn).not.toHaveBeenCalled();
    expect(onSetupComplete).not.toHaveBeenCalled();
    expect(focus).not.toHaveBeenCalled();
    expect(select).not.toHaveBeenCalled();
    input.remove();
  });

  it('focuses and selects setup code when the server rejects the token', async () => {
    const input = document.createElement('input');
    input.id = 'setup-code';
    document.body.append(input);
    const select = vi.spyOn(input, 'select');
    mockedCreateOwner.mockResolvedValue({ kind: 'badToken' });
    const { result } = renderHook(() => useOnboardingForm({
      onSignedIn: vi.fn(),
      onSetupComplete: vi.fn(),
    }));
    act(() => {
      result.current.setSetupCode('  token  ');
      result.current.setHouseholdName('Home');
      result.current.setLoginName('login');
      result.current.setPassword('password');
    });

    await act(async () => {
      await result.current.submit({ preventDefault: vi.fn() } as never);
    });

    expect(input).toHaveFocus();
    expect(select).toHaveBeenCalledOnce();
    input.remove();
  });
});
