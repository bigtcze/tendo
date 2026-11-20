import type { components } from '../../../../api/generated/api';
import { api } from '../../lib/api';

export type McpAuthorizationRequest = components['schemas']['McpAuthorizationRequest'];
export type ConnectDecision = 'approve' | 'deny';

/** Every failure the consent screen distinguishes; anything unclassified is `unavailable`. */
export type ConnectFailure = 'notFound' | 'unauthenticated' | 'rateLimited' | 'unavailable';

export type FetchConnectResult = { kind: 'ok'; request: McpAuthorizationRequest } | { kind: ConnectFailure };
export type DecideConnectResult = { kind: 'redirect'; redirectTo: string } | { kind: ConnectFailure };

function failureFor(status: number): ConnectFailure {
  if (status === 404) return 'notFound';
  if (status === 401) return 'unauthenticated';
  if (status === 429) return 'rateLimited';
  return 'unavailable';
}

export async function fetchConnectRequest(handle: string): Promise<FetchConnectResult> {
  try {
    const { data, response } = await api.GET('/api/v1/mcp/authorization-requests/{requestHandle}', {
      params: { path: { requestHandle: handle } },
    });
    if (data && response.status === 200) return { kind: 'ok', request: data };
    return { kind: failureFor(response.status) };
  } catch {
    return { kind: 'unavailable' };
  }
}

export async function decideConnectRequest(handle: string, decision: ConnectDecision): Promise<DecideConnectResult> {
  try {
    const { data, response } = await api.POST('/api/v1/mcp/authorization-requests/{requestHandle}/decision', {
      params: {
        path: { requestHandle: handle },
        // Browsers set Origin themselves and ignore this value; it only satisfies the generated type.
        header: { Origin: window.location.origin },
      },
      body: { decision },
    });
    if (data && response.status === 200) return { kind: 'redirect', redirectTo: data.redirectTo };
    return { kind: failureFor(response.status) };
  } catch {
    return { kind: 'unavailable' };
  }
}

const REJECTED_SCHEMES = ['javascript:', 'data:', 'vbscript:', 'file:', 'blob:'];
const LOOPBACK_HOSTS = ['127.0.0.1', '[::1]', 'localhost'];

function parseUrl(value: string): URL | null {
  try { return new URL(value); } catch { return null; }
}

function sameEndpoint(registered: URL, target: URL): boolean {
  if (registered.protocol !== target.protocol || registered.pathname !== target.pathname) return false;
  if (registered.protocol === 'http:' && LOOPBACK_HOSTS.includes(registered.hostname)) {
    // RFC 8252 §7.3: a loopback redirect may use any port, but its host must be the same literal.
    return registered.hostname === target.hostname;
  }
  return registered.hostname === target.hostname && registered.port === target.port;
}

/**
 * The server appends the authorization response to the registered URI. The browser must only leave for that same
 * endpoint with the registered query intact, so a wrong redirect target never becomes a navigation.
 */
export function isSafeRedirect(redirectUri: string, redirectTo: string): boolean {
  const registered = parseUrl(redirectUri);
  const target = parseUrl(redirectTo);
  if (!registered || !target) return false;
  if (REJECTED_SCHEMES.includes(registered.protocol) || REJECTED_SCHEMES.includes(target.protocol)) return false;
  if (!sameEndpoint(registered, target)) return false;
  for (const [name, value] of registered.searchParams) {
    if (!target.searchParams.getAll(name).includes(value)) return false;
  }
  return true;
}

export type RedirectDescription =
  | { kind: 'web'; host: string }
  | { kind: 'loopback'; host: string }
  | { kind: 'scheme'; scheme: string }
  | { kind: 'unknown' };

/** What the consent screen names as the destination. Hosts stay in their punycode form, never decoded. */
export function describeRedirect(redirectUri: string): RedirectDescription {
  const url = parseUrl(redirectUri);
  if (!url) return { kind: 'unknown' };
  if (url.protocol === 'https:') return { kind: 'web', host: url.hostname };
  if (url.protocol === 'http:' && LOOPBACK_HOSTS.includes(url.hostname)) return { kind: 'loopback', host: url.host };
  // A private-use scheme such as com.example.app: reverse-DNS, no authority.
  if (/^[a-z][a-z0-9+.-]*:$/.test(url.protocol) && url.protocol !== 'http:' && !REJECTED_SCHEMES.includes(url.protocol)) {
    const scheme = url.protocol.slice(0, -1);
    if (scheme.includes('.')) return { kind: 'scheme', scheme };
  }
  return { kind: 'unknown' };
}
