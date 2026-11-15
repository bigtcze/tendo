import { fetchSession, type Session } from '../auth/sessionApi';

/**
 * An authoritative `403` refusal means the navigation hint is stale: re-read the session so the Admin center entry
 * follows the grant the account really holds instead of the one this tab booted with. A session that has ended goes
 * back to sign-in, and a read that failed leaves the calm refusal standing, which is still the truth.
 *
 * The re-read is only adopted for the account this shell is showing. A session for someone else means the sign-in
 * underneath the tab changed — possible in another tab — and folding it in would leave the last screen of the old
 * account under the new one; the app boots again from the server instead.
 */
export async function rereadInstanceAdminSession(handlers: {
  viewerId: string;
  onSession: (session: Session) => void;
  onSignedOut: () => void;
  onReboot: () => void;
}) {
  const result = await fetchSession();
  if (result.kind === 'signedOut') { handlers.onSignedOut(); return; }
  if (result.kind !== 'signedIn') return;
  if (result.session.userId === handlers.viewerId) handlers.onSession(result.session);
  else handlers.onReboot();
}
