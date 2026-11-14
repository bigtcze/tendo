import { vi } from 'vitest';
import type { components } from '../../../api/generated/api';

export interface RecordedRequest {
  method: string;
  path: string;
  query: URLSearchParams;
  body: string;
  headers: Headers;
  /** Browser path when the app issued the request, captured before any await so it names the requesting screen. */
  documentPath: string;
}

type Handler = (req: RecordedRequest) => Response | Promise<Response>;

export function json(status: number, body?: unknown, headers: Record<string, string> = {}): Response {
  if (status === 204) return new Response(null, { status });
  return new Response(body === undefined ? null : JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json', ...headers },
  });
}

/** Replaces only the network boundary (fetch). Routes are keyed "METHOD /path". */
export function installFakeServer(routes: Record<string, Handler | Response>) {
  const requests: RecordedRequest[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: Request) => {
      const documentPath = window.location.pathname;
      const url = new URL(input.url);
      const req: RecordedRequest = {
        method: input.method,
        path: url.pathname,
        query: url.searchParams,
        body: await input.text(),
        headers: input.headers,
        documentPath,
      };
      requests.push(req);
      const queryRoute = `${req.method} ${req.path}?${url.searchParams.toString()}`;
      const route = routes[queryRoute] ?? routes[`${req.method} ${req.path}`];
      if (!route) return json(500, { title: `unhandled ${req.method} ${req.path}` });
      return typeof route === 'function' ? route(req) : route.clone();
    }),
  );
  return { requests };
}

export const emptyHome = {
  [`GET /api/v1/households/${'0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61'}/items/summary`]: json(200, {
    evaluatedOn: '2026-01-15', total: 0, needsAttention: 0, inProgress: 0, waiting: 0, upcoming: 0, paused: 0, done: 0, archived: 0,
  }),
  [`GET /api/v1/households/${'0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61'}/items`]: json(200, { items: [], nextCursor: null }),
  [`GET /api/v1/households/${'0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61'}/subjects`]: json(200, { items: [], nextCursor: null }),
};

export const HOUSEHOLD_ID = '0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61';

export const session = (withHousehold = true): components['schemas']['Session'] => ({
  userId: '0198a2f0-0000-7000-8000-000000000001',
  login: 'anna',
  ...(withHousehold ? { defaultHouseholdId: HOUSEHOLD_ID } : {}),
  expiresAt: '2030-01-01T00:00:00Z',
  instanceAdmin: false,
  signInMethod: 'password',
});

/**
 * The change-password contract at the network boundary: a provider or pre-upgrade session is refused before anything
 * else, a policy-breaking new password is refused before a wrong current one (like the server), and success replaces
 * the account's password and answers with a freshly rotated session. The stub keeps no cookie jar, so the rotation is
 * visible only as the `Set-Cookie` header and in the returned `expiresAt`.
 */
export function passwordChange(options: {
  currentPassword: string;
  signInMethod?: components['schemas']['Session']['signInMethod'];
  /** The session the app booted with; the success body is the same person with the rotated expiry. */
  session?: components['schemas']['Session'];
  expiresAt?: string;
  /** The account throttle is already spent, so every attempt is refused with 429. */
  rateLimited?: boolean;
}) {
  const base = options.session ?? session();
  const account = {
    password: options.currentPassword,
    signInMethod: options.signInMethod ?? 'password',
    expiresAt: options.expiresAt ?? '2030-06-01T00:00:00Z',
    rotations: 0,
  };
  const route = (req: RecordedRequest): Response => {
    if (account.signInMethod !== 'password') {
      return json(403, { type: 'about:blank', title: 'Forbidden', status: 403, code: 'local_session_required' });
    }
    if (options.rateLimited) {
      return json(
        429,
        { type: 'about:blank', title: 'Too Many Requests', status: 429, code: 'rate_limited' },
        { 'Retry-After': '30' },
      );
    }
    const body = JSON.parse(req.body) as { currentPassword: string; newPassword: string };
    if (body.newPassword.length < 15 || body.newPassword.length > 128) {
      return json(422, {
        type: 'about:blank', title: 'Validation Failed', status: 422, code: 'invalid_length', field: 'newPassword',
      });
    }
    if (body.currentPassword !== account.password) {
      return json(422, {
        type: 'about:blank', title: 'Validation Failed', status: 422, code: 'incorrect', field: 'currentPassword',
      });
    }
    account.password = body.newPassword;
    account.rotations += 1;
    return json(200, { ...base, expiresAt: account.expiresAt }, {
      'Set-Cookie': `tendo_session=rotated-${account.rotations}; Path=/; HttpOnly; SameSite=Lax`,
    });
  };
  return { routes: { 'POST /api/v1/auth/password': route }, account };
}


export const household: components['schemas']['Household'] = {
  id: HOUSEHOLD_ID,
  name: 'Veselí',
  timezone: 'Europe/Prague',
  createdAt: '2026-01-01T00:00:00Z',
  viewerRole: 'admin',
};

/** The same household seen by a plain member: the navigation then offers no Admin center. */
export const memberHousehold: components['schemas']['Household'] = { ...household, viewerRole: 'member' };

export const setupStatus = (required: boolean): components['schemas']['SetupStatus'] => ({ required });
