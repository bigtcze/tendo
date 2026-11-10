import type { components } from '../../../../api/generated/api';
import { api } from '../../lib/api';
import { problemCode } from './oidcApi';
import type { Session } from './sessionApi';

export type OidcPendingStatus = components['schemas']['OIDCPendingStatus'];
export type OidcInvitationOutcome = components['schemas']['OIDCInvitationOutcome'];

export type PendingStatusResult =
  | { kind: 'ok'; status: OidcPendingStatus }
  | { kind: 'expired' }
  | { kind: 'alreadySignedIn' }
  | { kind: 'rateLimited' }
  | { kind: 'unavailable' };

/** Reads the continuation behind the pending cookie without consuming it. */
export async function fetchOidcPending(): Promise<PendingStatusResult> {
  try {
    const { data, response } = await api.GET('/api/v1/auth/oidc/pending');
    if (data && response.status === 200) return { kind: 'ok', status: data };
    if (response.status === 404) return { kind: 'expired' };
    if (response.status === 409) return { kind: 'alreadySignedIn' };
    if (response.status === 429) return { kind: 'rateLimited' };
  } catch {
    // Network failure falls through to the unavailable state.
  }
  return { kind: 'unavailable' };
}

export type PendingAccountResult =
  | { kind: 'ok'; session: Session; invitation: OidcInvitationOutcome }
  | { kind: 'expired' }
  | { kind: 'conflict' }
  | { kind: 'alreadySignedIn' }
  | { kind: 'rateLimited' }
  | { kind: 'unavailable' };

/** Continues a first login as a new Tendo account (no password, no household unless invited). */
export async function createOidcPendingAccount(): Promise<PendingAccountResult> {
  try {
    const { data, error, response } = await api.POST('/api/v1/auth/oidc/pending/account', {
      // Browsers set Origin themselves and ignore this value; it only satisfies the generated type.
      params: { header: { Origin: window.location.origin } },
    });
    if (data && response.status === 201) {
      return { kind: 'ok', session: data.session, invitation: data.invitation };
    }
    const code = await problemCode(response, error);
    if (response.status === 404) return { kind: 'expired' };
    if (response.status === 409) return code === 'already_signed_in' ? { kind: 'alreadySignedIn' } : { kind: 'conflict' };
    if (response.status === 429) return { kind: 'rateLimited' };
  } catch {
    // fall through
  }
  return { kind: 'unavailable' };
}

export type PendingLinkResult =
  | { kind: 'ok'; session: Session; invitation: OidcInvitationOutcome; emailSaved: boolean }
  | { kind: 'invalidCredentials' }
  | { kind: 'expired' }
  | { kind: 'conflict' }
  | { kind: 'alreadySignedIn' }
  | { kind: 'rateLimited' }
  | { kind: 'unavailable' };

/** Binds the pending sign-in to an existing Tendo account after checking its username and password. */
export async function confirmOidcPendingLink(login: string, password: string): Promise<PendingLinkResult> {
  try {
    const { data, error, response } = await api.POST('/api/v1/auth/oidc/pending/link', {
      body: { login, password },
      // Browsers set Origin themselves and ignore this value; it only satisfies the generated type.
      params: { header: { Origin: window.location.origin } },
    });
    if (data && response.status === 201) {
      return { kind: 'ok', session: data.session, invitation: data.invitation, emailSaved: data.emailSaved };
    }
    const code = await problemCode(response, error);
    if (response.status === 401) return { kind: 'invalidCredentials' };
    if (response.status === 404) return { kind: 'expired' };
    if (response.status === 409) return code === 'already_signed_in' ? { kind: 'alreadySignedIn' } : { kind: 'conflict' };
    if (response.status === 429) return { kind: 'rateLimited' };
  } catch {
    // fall through
  }
  return { kind: 'unavailable' };
}

export type PendingCancelResult = 'ok' | 'rateLimited' | 'unavailable';

/** Drops the continuation and its cookie; idempotent, so an absent continuation also counts as done. */
export async function cancelOidcPending(): Promise<PendingCancelResult> {
  try {
    const { response } = await api.DELETE('/api/v1/auth/oidc/pending', {
      // Browsers set Origin themselves and ignore this value; it only satisfies the generated type.
      params: { header: { Origin: window.location.origin } },
    });
    if (response.status === 204 || response.status === 404) return 'ok';
    if (response.status === 429) return 'rateLimited';
  } catch {
    // fall through
  }
  return 'unavailable';
}

export type DiscardResult =
  | { kind: 'ok' }
  | { kind: 'notDiscardable' }
  | { kind: 'unauthenticated' }
  | { kind: 'disabled' }
  | { kind: 'rateLimited' }
  | { kind: 'unavailable' };

/** Removes an untouched first-login account and leaves a fresh continuation for the local-password flow. */
export async function discardOidcAccount(): Promise<DiscardResult> {
  try {
    const { response } = await api.POST('/api/v1/auth/oidc/account/discard', {
      // Browsers set Origin themselves and ignore this value; it only satisfies the generated type.
      params: { header: { Origin: window.location.origin } },
    });
    if (response.status === 200) return { kind: 'ok' };
    if (response.status === 401) return { kind: 'unauthenticated' };
    if (response.status === 404) return { kind: 'disabled' };
    if (response.status === 409) return { kind: 'notDiscardable' };
    if (response.status === 429) return { kind: 'rateLimited' };
  } catch {
    // fall through
  }
  return { kind: 'unavailable' };
}
