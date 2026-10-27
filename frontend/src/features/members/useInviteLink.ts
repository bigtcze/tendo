import { useRef, useState } from 'react';
import type { Translate } from '../../i18n';

/** The one-time invitation link shown after a create, with copy and manual-copy fallback. */
export function useInviteLink(t: Translate, setNotice: (notice: string | null) => void) {
  const [link, setLink] = useState<string | null>(null);
  const [linkInvitationId, setLinkInvitationId] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const linkRef = useRef<HTMLInputElement>(null);
  const pendingLinkFocusRef = useRef(false);

  function clearLink() { setLink(null); setLinkInvitationId(null); setCopied(false); }
  function showLink(token: string, invitationId: string) {
    setLink(`${window.location.origin}/invite#${token}`);
    setLinkInvitationId(invitationId);
    pendingLinkFocusRef.current = true;
  }
  async function copyLink() {
    if (!link) return;
    try { await navigator.clipboard.writeText(link); setCopied(true); setNotice(t('members.invite.copied')); }
    catch { setNotice(t('members.invite.manualCopy')); window.setTimeout(() => { linkRef.current?.focus(); linkRef.current?.select(); }, 0); }
  }

  return { link, linkInvitationId, copied, linkRef, pendingLinkFocusRef, clearLink, showLink, copyLink };
}
