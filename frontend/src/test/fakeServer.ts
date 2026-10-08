import { vi } from 'vitest';
import type { components } from '../../../api/generated/api';

export interface RecordedRequest {
  method: string;
  path: string;
  query: URLSearchParams;
  body: string;
  headers: Headers;
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
      const url = new URL(input.url);
      const req: RecordedRequest = {
        method: input.method,
        path: url.pathname,
        query: url.searchParams,
        body: await input.text(),
        headers: input.headers,
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
  [`GET /api/v1/households/${'0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61'}/items`]: json(200, { items: [], nextCursor: null }),
  [`GET /api/v1/households/${'0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61'}/subjects`]: json(200, { items: [], nextCursor: null }),
};

export const HOUSEHOLD_ID = '0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61';

export const session = (withHousehold = true): components['schemas']['Session'] => ({
  userId: '0198a2f0-0000-7000-8000-000000000001',
  login: 'anna',
  ...(withHousehold ? { defaultHouseholdId: HOUSEHOLD_ID } : {}),
  expiresAt: '2030-01-01T00:00:00Z',
});

export const household: components['schemas']['Household'] = {
  id: HOUSEHOLD_ID,
  name: 'Veselí',
  timezone: 'Europe/Prague',
  createdAt: '2026-01-01T00:00:00Z',
};

export const setupStatus = (required: boolean): components['schemas']['SetupStatus'] => ({ required });
