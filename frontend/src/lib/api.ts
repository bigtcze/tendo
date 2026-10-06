import createClient from 'openapi-fetch';
import type { paths } from '../../../api/generated/api';

// Resolve fetch at call time so the network boundary can be replaced in tests.
export const api = createClient<paths>({
  baseUrl: '',
  credentials: 'same-origin',
  fetch: (request) => globalThis.fetch(request),
});
