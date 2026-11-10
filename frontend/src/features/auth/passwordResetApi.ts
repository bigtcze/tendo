import { api } from '../../lib/api';

export type RedeemResult =
  | { kind: 'ok'; login: string }
  | { kind: 'invalidLink' | 'invalidLength' | 'rateLimited' | 'unavailable' };

/** Public and rate-limited: every unusable link answers the same 404, whatever made it unusable. */
export async function redeemPasswordReset(token: string, password: string): Promise<RedeemResult> {
  try {
    const { data, error, response } = await api.POST('/api/v1/auth/password-reset', {
      body: { token, password },
      // Browsers set Origin themselves and ignore this value; it only satisfies the generated type.
      params: { header: { Origin: window.location.origin } },
    });
    if (data && response.status === 200) return { kind: 'ok', login: data.login };
    if (response.status === 404) return { kind: 'invalidLink' };
    if (response.status === 422 && (error as { field?: unknown } | undefined)?.field === 'password') {
      return { kind: 'invalidLength' };
    }
    if (response.status === 429) return { kind: 'rateLimited' };
  } catch {
    // fall through
  }
  return { kind: 'unavailable' };
}
