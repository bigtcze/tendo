import { api } from '../../lib/api';

/** Full-page navigation to the identity provider; an object so tests can observe it. */
export const browserNavigation = {
  assign(url: string) {
    window.location.assign(url);
  },
};

export type OidcStatus = { kind: 'enabled'; displayName: string } | { kind: 'disabled' } | { kind: 'error' };

export async function fetchOidcStatus(): Promise<OidcStatus> {
  try {
    const { data, response } = await api.GET('/api/v1/auth/oidc');
    if (data && response.status === 200) {
      if (data.enabled && typeof data.displayName === 'string' && data.displayName) return { kind: 'enabled', displayName: data.displayName };
      if (!data.enabled) return { kind: 'disabled' };
    }
  } catch {
    // fall through
  }
  return { kind: 'error' };
}

export type IdentityResult = { kind: 'linked' } | { kind: 'notLinked' } | { kind: 'disabled' } | { kind: 'unauthenticated' } | { kind: 'error' };

export async function fetchOidcIdentity(): Promise<IdentityResult> {
  try {
    const { data, response } = await api.GET('/api/v1/auth/oidc/identity');
    if (data && response.status === 200) return data.linked ? { kind: 'linked' } : { kind: 'notLinked' };
    if (response.status === 401) return { kind: 'unauthenticated' };
    if (response.status === 404) return { kind: 'disabled' };
  } catch {
    // fall through
  }
  return { kind: 'error' };
}

export type StartResult =
  | { kind: 'redirect'; url: string }
  | { kind: 'wrongPassword' }
  | { kind: 'unauthenticated' }
  | { kind: 'alreadySignedIn' }
  | { kind: 'disabled' }
  | { kind: 'rateLimited' }
  | { kind: 'providerUnavailable' }
  | { kind: 'unavailable' };

async function problemCode(response: Response, error: unknown): Promise<unknown> {
  if (error && typeof error === 'object' && 'code' in error) return (error as { code?: unknown }).code;
  try {
    const body: unknown = await response.clone().json();
    return body && typeof body === 'object' ? (body as { code?: unknown }).code : undefined;
  } catch {
    return undefined;
  }
}

/** Starts the provider redirect. The caller navigates the browser to the returned URL. */
export async function startOidc(input: { purpose: 'login' } | { purpose: 'link'; currentPassword: string }): Promise<StartResult> {
  try {
    const { data, error, response } = await api.POST('/api/v1/auth/oidc/start', {
      body: input,
      // Browsers set Origin themselves and ignore this value; it only satisfies the generated type.
      params: { header: { Origin: window.location.origin } },
    });
    if (data && response.status === 200) return { kind: 'redirect', url: data.authorizationUrl };
    const code = await problemCode(response, error);
    if (response.status === 401) return code === 'invalid_credentials' ? { kind: 'wrongPassword' } : { kind: 'unauthenticated' };
    if (response.status === 409) return { kind: 'alreadySignedIn' };
    if (response.status === 404) return { kind: 'disabled' };
    if (response.status === 429) return { kind: 'rateLimited' };
    if (response.status === 503 && code === 'oidc_unavailable') return { kind: 'providerUnavailable' };
  } catch {
    // fall through
  }
  return { kind: 'unavailable' };
}

/** Error codes the callback may place in the fragment; anything else maps to a generic message. */
export const oidcErrorCodes = ['cancelled', 'invalid_flow', 'identity_not_linked', 'identity_conflict', 'session_changed', 'authentication_failed', 'unavailable'] as const;
export type OidcErrorCode = (typeof oidcErrorCodes)[number] | 'unknown';
export type OidcOutcome = { kind: 'connected' } | { kind: 'error'; code: OidcErrorCode };

/** Reads the callback result fragment once. Values are fixed by the server and never reflected verbatim. */
export function parseOidcFragment(hash: string): OidcOutcome | null {
  if (hash === '#oidc=connected') return { kind: 'connected' };
  const match = /^#oidcError=(.*)$/.exec(hash);
  if (!match) return null;
  const code = (oidcErrorCodes as readonly string[]).includes(match[1]!) ? (match[1] as OidcErrorCode) : 'unknown';
  return { kind: 'error', code };
}
