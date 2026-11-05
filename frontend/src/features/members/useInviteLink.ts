import { useRef, useState } from 'react';
import type { Translate } from '../../i18n';

/** The one-time invitation link shown after a create, with copy and manual-copy fallback. */
export function useInviteLink(t: Translate) {
  const [link, setLink] = useState<string | null>(null);
  const [linkInvitationId, setLinkInvitationId] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const [copyMessage, setCopyMessage] = useState<string | null>(null);
  const linkRef = useRef<HTMLInputElement>(null);
  const pendingLinkFocusRef = useRef(false);

  function clearLink() { setLink(null); setLinkInvitationId(null); setCopied(false); setCopyMessage(null); }
  function showLink(token: string, invitationId: string) {
    setLink(`${window.location.origin}/invite#${token}`);
    setLinkInvitationId(invitationId);
    setCopyMessage(null);
    pendingLinkFocusRef.current = true;
  }
  async function copyLink() {
    if (!link) return;
    try {
      await navigator.clipboard.writeText(link);
      setCopied(true);
      setCopyMessage(t('members.invite.copied'));
    } catch {
      const message = t('members.invite.manualCopy');
      setCopyMessage(message);
      window.setTimeout(() => { linkRef.current?.focus(); linkRef.current?.select(); }, 0);
    }
  }

  return { link, linkInvitationId, copied, copyMessage, linkRef, pendingLinkFocusRef, clearLink, showLink, copyLink };
}
