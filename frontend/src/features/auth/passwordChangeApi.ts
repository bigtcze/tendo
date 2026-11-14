import { api } from '../../lib/api';
import type { Session } from './sessionApi';

export type ChangePasswordResult =
  | { kind: 'ok'; session: Session }
  | { kind: 'incorrect' | 'invalidLength' | 'localSessionRequired' | 'unauthenticated' | 'rateLimited' | 'unavailable' };

/**
 * The local-only password replacement. The server decides from the session cookie whether this browser may change the
 * password, so a provider or pre-upgrade session answers 403 without reading the form. A wrong current password is a
 * field answer (422) that keeps the person signed in; only a session that is already gone is a 401, which the caller
 * treats as signed out like every other request. Success rotates the cookie and answers with the fresh session.
 */
export async function changePassword(currentPassword: string, newPassword: string): Promise<ChangePasswordResult> {
  try {
    const { data, error, response } = await api.POST('/api/v1/auth/password', {
      body: { currentPassword, newPassword },
      // Browsers set Origin themselves and ignore this value; it only satisfies the generated type.
      params: { header: { Origin: window.location.origin } },
    });
    if (data && response.status === 200) return { kind: 'ok', session: data };
    if (response.status === 401) return { kind: 'unauthenticated' };
    if (response.status === 403) {
      // A foreign Origin is also a code-less 403; only the session-kind refusal has a code to recognize.
      return (error as { code?: unknown } | undefined)?.code === 'local_session_required'
        ? { kind: 'localSessionRequired' }
        : { kind: 'unavailable' };
    }
    if (response.status === 422) {
      const { field } = (error ?? {}) as { field?: unknown };
      if (field === 'currentPassword') return { kind: 'incorrect' };
      // The contract names only these two fields; anything else changed nothing either, so offer a retry.
      if (field === 'newPassword') return { kind: 'invalidLength' };
      return { kind: 'unavailable' };
    }
    if (response.status === 429) return { kind: 'rateLimited' };
  } catch {
    // Network failure falls through to the retryable state.
  }
  return { kind: 'unavailable' };
}
