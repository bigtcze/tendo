import { fetchSession, type Session } from '../auth/sessionApi';

/**
 * An authoritative `403` refusal means the navigation hint is stale: re-read the session so the Admin center entry
 * follows the grant the account really holds instead of the one this tab booted with. A session that has ended goes
 * back to sign-in; a read that failed leaves the calm refusal standing, which is still the truth.
 */
export async function rereadInstanceAdminSession(handlers: { onSession: (session: Session) => void; onSignedOut: () => void }) {
  const result = await fetchSession();
  if (result.kind === 'signedIn') handlers.onSession(result.session);
  else if (result.kind === 'signedOut') handlers.onSignedOut();
}
