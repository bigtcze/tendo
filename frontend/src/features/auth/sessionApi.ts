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

export type SetupField = components['schemas']['ValidationProblem']['field'];

export type SetupInput = components['schemas']['SetupRequest'] & { token: string };

export type CreateOwnerResult =
  | { kind: 'ok' }
  | { kind: 'badToken' }
  | { kind: 'invalidField'; field: SetupField }
  | { kind: 'alreadySetUp' }
  | { kind: 'setupDisabled' }
  | { kind: 'rateLimited' }
  | { kind: 'unavailable' };

const setupFields: readonly string[] = ['login', 'password', 'householdName', 'timezone'];

async function problemBody(response: Response): Promise<{ code?: unknown; field?: unknown }> {
  try {
    const body: unknown = await response.json();
    return typeof body === 'object' && body !== null ? body : {};
  } catch {
    return {};
  }
}

export async function createOwner(input: SetupInput): Promise<CreateOwnerResult> {
  try {
    const { token, ...body } = input;
    // The setup token header is a security scheme, so the generated types don't list it.
    // Browsers set Origin themselves and ignore this value; it only satisfies the generated type.
    const header = { Origin: window.location.origin, 'X-Tendo-Setup-Token': token };
    const { response, error } = await api.PUT('/api/v1/auth/setup', {
      body,
      params: { header },
    });
    if (response.status === 201) return { kind: 'ok' };
    if (response.status === 401) return { kind: 'badToken' };
    if (response.status === 409) return { kind: 'alreadySetUp' };
    if (response.status === 429) return { kind: 'rateLimited' };
    if (response.status === 422) {
      const { field } = (error ?? {}) as { field?: unknown };
      if (typeof field === 'string' && setupFields.includes(field)) {
        return { kind: 'invalidField', field: field as SetupField };
      }
    }
    if (response.status === 503) {
      const { code } = (error ?? (await problemBody(response))) as { code?: unknown };
      if (code === 'setup_unavailable') return { kind: 'setupDisabled' };
    }
  } catch {
    // fall through
  }
  return { kind: 'unavailable' };
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
