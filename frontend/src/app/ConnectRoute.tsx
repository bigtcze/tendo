import { ConnectScreen } from '../features/mcp/ConnectScreen';
import type { Session } from '../features/auth/sessionApi';
import type { ConnectVisit } from './useConnectVisit';
import { ScreenTransitionContext } from './Heading';

type Props = {
  visit: ConnectVisit;
  session: Session;
  transitioned: { current: boolean };
  onSignedOut: () => void;
  onHome: () => void;
};

/**
 * The consent page for a signed-in person. It is a destination of its own: the household shell never mounts here,
 * so no Home screen can flash before the consent loads. Ending the session (expired or "Not you?") returns to sign-in.
 */
export function ConnectRoute({ visit, session, transitioned, onSignedOut, onHome }: Props) {
  return (
    <ScreenTransitionContext value={transitioned}>
      <ConnectScreen
        key={visit.visit}
        outcome={visit.outcome}
        session={session}
        onSignOut={onSignedOut}
        onSessionEnded={onSignedOut}
        onHome={onHome}
      />
    </ScreenTransitionContext>
  );
}
