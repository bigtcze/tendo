import type { components } from '../../../../api/generated/api';
import { api } from '../../lib/api';

export type Session = components['schemas']['Session'];

export type SessionResult = { kind: 'signedIn'; session: Session } | { kind: 'signedOut' } | { kind: 'error' };

export async function fetchSession(): Promise<SessionResult> {
  try {
    const { data, response } = await api.GET('/api/v1/session');
    if (data && response.status === 200) return { kind: 'signedIn', session: data };
    if (response.status === 401) return { kind: 'signedOut' };
  } catch {
    // Network failure falls through to the error state.
  }
  return { kind: 'error' };
}

export type SetupResult = 'required' | 'complete' | 'error';

export async function fetchSetupRequired(): Promise<SetupResult> {
  try {
    const { data } = await api.GET('/api/v1/auth/setup');
    if (data) return data.required ? 'required' : 'complete';
  } catch {
    // fall through
  }
  return 'error';
}

export type LoginResult =
  | { kind: 'ok'; session: Session }
  | { kind: 'invalid' }
  | { kind: 'rateLimited' }
  | { kind: 'unavailable' };

export async function login(loginName: string, password: string): Promise<LoginResult> {
  try {
    const { data, response } = await api.POST('/api/v1/session', {
      body: { login: loginName, password },
      // Browsers set Origin themselves and ignore this value; it only satisfies the generated type.
      params: { header: { Origin: window.location.origin } },
    });
    if (data && response.status === 201) return { kind: 'ok', session: data };
    if (response.status === 401) return { kind: 'invalid' };
    if (response.status === 429) return { kind: 'rateLimited' };
  } catch {
    // fall through
  }
  return { kind: 'unavailable' };
}

export type LogoutResult = 'ok' | 'error';

export async function logout(): Promise<LogoutResult> {
  try {
    const { response } = await api.DELETE('/api/v1/session', {
      // Browsers set Origin themselves and ignore this value; it only satisfies the generated type.
      params: { header: { Origin: window.location.origin } },
    });
    if (response.status === 204 || response.status === 401) return 'ok';
  } catch {
    // fall through
  }
  return 'error';
}
