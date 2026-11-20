import { ResetPasswordScreen } from '../features/auth/ResetPasswordScreen';
import { ScreenTransitionContext } from './Heading';

type Props = {
  token: string | null;
  signedIn: boolean;
  transitioned: { current: boolean };
  onRedeemed: (login: string) => void;
};

/**
 * A reset link is its own destination for everyone, signed in or not: the link is the authority, not the current
 * session, and success always ends at sign-in with the new password.
 */
export function ResetRoute({ token, signedIn, transitioned, onRedeemed }: Props) {
  return (
    <ScreenTransitionContext value={transitioned}>
      <ResetPasswordScreen key={token ?? 'no-token'} token={token} signedIn={signedIn} onRedeemed={onRedeemed} />
    </ScreenTransitionContext>
  );
}
