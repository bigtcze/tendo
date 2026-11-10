import type { MessageKey } from '../i18n';
import { LOGIN_PATH } from './routes';

/** The callback lands on `/login#oidc=continue` when the provider sign-in needs one more step. */
export const OIDC_CONTINUE_HASH = '#oidc=continue';

export function isOidcContinueFragment(pathname: string, hash: string): boolean {
  return pathname === LOGIN_PATH && hash === OIDC_CONTINUE_HASH;
}

const invitationNotices = {
  invitation_accepted: 'home.notice.invitationAccepted',
  invitation_invalid: 'home.notice.invitationInvalid',
} as const satisfies Record<string, MessageKey>;

/**
 * The signed-in callback lands on `/#oidcNotice=...` after using a carried invitation.
 * The fragment is written by the server from a fixed set, so unknown values are ignored.
 */
export function oidcNoticeFromFragment(pathname: string, hash: string): MessageKey | null {
  if (pathname !== '/') return null;
  const match = /^#oidcNotice=(invitation_accepted|invitation_invalid)$/.exec(hash);
  if (!match) return null;
  return invitationNotices[match[1] as keyof typeof invitationNotices];
}
