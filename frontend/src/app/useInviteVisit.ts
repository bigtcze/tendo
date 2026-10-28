import { useEffect, useRef, useState } from 'react';
import { INVITE_PATH } from './routes';

let inviteVisitFallbackCounter = 0;
export function createInviteVisitId(): string {
  const cryptoApi = globalThis.crypto;
  if (typeof cryptoApi?.randomUUID === 'function') return cryptoApi.randomUUID();
  if (typeof cryptoApi?.getRandomValues === 'function') {
    const bytes = cryptoApi.getRandomValues(new Uint8Array(16));
    return Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('');
  }
  inviteVisitFallbackCounter += 1;
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}-${inviteVisitFallbackCounter.toString(36)}`;
}

export function useInviteVisit() {
  const [inviteVisit, setInviteVisit] = useState<string | null>(() => {
    const initialVisit = window.history.state?.inviteVisit;
    return typeof initialVisit === 'string' ? initialVisit : null;
  });
  const inviteTokens = useRef(new Map<string, string>());
  const [inviteToken, setInviteToken] = useState<string | null>(() => {
    if (window.location.pathname !== INVITE_PATH) return null;
    const hash = window.location.hash;
    return hash.startsWith('#') ? hash.slice(1) || null : null;
  });

  useEffect(() => {
    const captureInviteVisit = () => {
      if (window.location.pathname !== INVITE_PATH) return;
      if (window.location.hash) {
        const token = window.location.hash.slice(1);
        const visit = createInviteVisitId();
        inviteTokens.current.set(visit, token);
        window.history.replaceState({ inviteVisit: visit }, '', INVITE_PATH);
        setInviteToken(token || null);
        setInviteVisit(visit);
        return;
      }
      const visit = window.history.state?.inviteVisit;
      if (typeof visit === 'string') {
        setInviteVisit(visit);
        setInviteToken(inviteTokens.current.get(visit) ?? null);
      } else {
        setInviteVisit(null);
        setInviteToken(null);
      }
    };
    if (window.location.pathname === INVITE_PATH && window.location.hash) {
      const token = window.location.hash.slice(1);
      const visit = createInviteVisitId();
      inviteTokens.current.set(visit, token);
      window.history.replaceState({ inviteVisit: visit }, '', INVITE_PATH);
    }
    window.addEventListener('hashchange', captureInviteVisit);
    window.addEventListener('popstate', captureInviteVisit);
    return () => {
      window.removeEventListener('hashchange', captureInviteVisit);
      window.removeEventListener('popstate', captureInviteVisit);
    };
  }, []);

  return { inviteVisit, inviteToken };
}
