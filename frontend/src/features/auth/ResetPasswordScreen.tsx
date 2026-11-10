import { Heading } from '../../app/Heading';
import { Layout } from '../../app/Layout';
import { LOGIN_PATH } from '../../app/routes';
import { Button } from '../../components/ui/button';
import { Input } from '../../components/ui/input';
import { Label } from '../../components/ui/label';
import { useI18n } from '../../i18n';
import { resetFailureKey } from './passwordResetModel';
import { usePasswordResetRedeem } from './usePasswordResetRedeem';

function ReturnLink({ href, label }: { href: string; label: string }) {
  return <a
    href={href}
    className="mt-4 inline-flex min-h-11 items-center rounded-xl px-3 text-small font-semibold text-muted hover:bg-sand hover:text-ink focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
  >
    {label}
  </a>;
}

function InvalidLink({ signedIn }: { signedIn: boolean }) {
  const { t } = useI18n();
  return <Layout><section className="settle mx-auto w-full max-w-md">
    <p className="text-center text-[0.68rem] font-semibold uppercase tracking-[0.18em] text-muted">{t('reset.eyebrow')}</p>
    <div className="mt-2 rounded-2xl border border-line bg-raised p-5 text-center sm:p-7">
      <Heading className="text-[2.125rem] font-bold leading-[1.08] text-ink">{t('reset.invalid.title')}</Heading>
      <p className="mt-3 text-base text-muted">{t('reset.invalid.body')}</p>
      <ReturnLink
        href={signedIn ? '/' : LOGIN_PATH}
        label={t(signedIn ? 'reset.invalid.home' : 'reset.invalid.signIn')}
      />
    </div>
  </section></Layout>;
}

/** A reset link is an invitation-shaped form: the token is the authority, so the screen works signed in or out. */
export function ResetPasswordScreen({ token, signedIn, onRedeemed }: {
  token: string | null;
  signedIn: boolean;
  onRedeemed: (login: string) => void;
}) {
  const { t } = useI18n();
  const {
    password, setPassword, confirm, setConfirm, failure, pending, invalidLink,
    passwordRef, confirmRef, submit,
  } = usePasswordResetRedeem(token, onRedeemed);
  if (invalidLink) return <InvalidLink signedIn={signedIn} />;
  const lengthInvalid = failure === 'length';
  const mismatch = failure === 'mismatch';
  const message = failure ? t(resetFailureKey(failure)) : null;
  return <Layout><section className="settle mx-auto w-full max-w-md">
    <p className="text-center text-[0.68rem] font-semibold uppercase tracking-[0.18em] text-muted">{t('reset.eyebrow')}</p>
    <Heading className="mt-2 text-center text-[2.125rem] font-bold leading-[1.08] text-ink sm:text-display">{t('reset.title')}</Heading>
    <p className="mt-3 text-center text-base text-muted">{t('reset.intro')}</p>
    <div className="mt-8 rounded-2xl border border-line bg-raised p-5 sm:p-7">
      <form aria-label={t('reset.form.label')} onSubmit={(event) => void submit(event)} className="grid gap-5" noValidate>
        <div className="space-y-2">
          <Label htmlFor="reset-password">{t('reset.password')}</Label>
          <Input
            ref={passwordRef}
            id="reset-password"
            name="new-password"
            type="password"
            autoComplete="new-password"
            required
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            aria-invalid={lengthInvalid}
            aria-describedby={`reset-password-hint${lengthInvalid ? ' reset-error' : ''}`}
          />
          <p id="reset-password-hint" className="text-small text-muted">{t('reset.password.hint')}</p>
        </div>
        <div className="space-y-2">
          <Label htmlFor="reset-password-confirm">{t('reset.password.confirm')}</Label>
          <Input
            ref={confirmRef}
            id="reset-password-confirm"
            name="confirm-password"
            type="password"
            autoComplete="new-password"
            required
            value={confirm}
            onChange={(event) => setConfirm(event.target.value)}
            aria-invalid={mismatch}
            aria-describedby={mismatch ? 'reset-error' : undefined}
          />
        </div>
        <div id="reset-error" role="alert" className="empty:contents">
          {message ? <div className="rounded-xl border border-danger bg-danger-bg px-4 py-3 text-small text-danger">{message}</div> : null}
        </div>
        <Button type="submit" disabled={pending} className="w-full">{pending ? t('reset.submitting') : t('reset.submit')}</Button>
      </form>
    </div>
  </section></Layout>;
}
