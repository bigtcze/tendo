import { useState, type ChangeEvent, type RefObject } from 'react';
import { Button } from '../../components/ui/button';
import { Input } from '../../components/ui/input';
import { Label } from '../../components/ui/label';
import { useI18n } from '../../i18n';
import { fieldFailure, formMessageKey } from './passwordChangeModel';
import type { Session } from './sessionApi';
import { usePasswordChange } from './usePasswordChange';

/** One password entry: the message under it is the only description, and the hint stays in front of the error. */
function PasswordField({ id, label, name, autoComplete, value, onChange, inputRef, invalid, hint, error }: {
  id: string;
  label: string;
  name: string;
  autoComplete: 'current-password' | 'new-password';
  value: string;
  onChange: (event: ChangeEvent<HTMLInputElement>) => void;
  inputRef: RefObject<HTMLInputElement | null>;
  invalid: boolean;
  hint?: string;
  error: string | null;
}) {
  const describedBy = [hint ? `${id}-hint` : null, error ? `${id}-error` : null].filter(Boolean).join(' ') || undefined;
  return (
    <div className="space-y-2">
      <Label htmlFor={id}>{label}</Label>
      <Input
        ref={inputRef}
        id={id}
        name={name}
        type="password"
        autoComplete={autoComplete}
        required
        value={value}
        onChange={onChange}
        aria-invalid={invalid}
        aria-describedby={describedBy}
      />
      {hint ? <p id={`${id}-hint`} className="text-small text-muted">{hint}</p> : null}
      {error ? <p id={`${id}-error`} className="text-small text-danger">{error}</p> : null}
    </div>
  );
}

/** The local change form. It is only mounted for a password session, so the server can honour every submit. */
function PasswordForm({ signInMethod, onChanged, onSignedOut, onRefused }: {
  signInMethod: Session['signInMethod'];
  onChanged: (session: Session) => void;
  onSignedOut: () => void;
  onRefused: () => void;
}) {
  const { t } = useI18n();
  const {
    currentPassword, setCurrentPassword, newPassword, setNewPassword, confirm, setConfirm,
    failure, success, pending, currentRef, newRef, confirmRef, submit,
  } = usePasswordChange({ signInMethod, onChanged, onSignedOut, onRefused });
  const failed = fieldFailure(failure);
  const errorFor = (field: 'current' | 'newPassword' | 'confirm') => (
    failed?.field === field ? t(failed.message) : null
  );
  const currentError = errorFor('current');
  const newError = errorFor('newPassword');
  const confirmError = errorFor('confirm');
  const messageKey = formMessageKey(failure);
  const formMessage = messageKey ? t(messageKey) : null;

  return (
    <form
      aria-label={t('account.password.form.label')}
      onSubmit={(event) => void submit(event)}
      className="mt-4 grid max-w-md gap-6"
      noValidate
    >
      <PasswordField
        id="account-password-current"
        label={t('account.password.current')}
        name="current-password"
        autoComplete="current-password"
        value={currentPassword}
        onChange={(event) => setCurrentPassword(event.target.value)}
        inputRef={currentRef}
        invalid={currentError !== null}
        error={currentError}
      />
      <PasswordField
        id="account-password-new"
        label={t('account.password.new')}
        name="new-password"
        autoComplete="new-password"
        value={newPassword}
        onChange={(event) => setNewPassword(event.target.value)}
        inputRef={newRef}
        invalid={newError !== null}
        hint={t('account.password.hint')}
        error={newError}
      />
      <PasswordField
        id="account-password-confirm"
        label={t('account.password.confirm')}
        name="confirm-password"
        autoComplete="new-password"
        value={confirm}
        onChange={(event) => setConfirm(event.target.value)}
        inputRef={confirmRef}
        invalid={confirmError !== null}
        error={confirmError}
      />
      {/* Both outcome hosts stay mounted and are display:contents while empty, so the grid keeps its rhythm and a
          message is never inserted into a live region that did not exist yet. */}
      <div role="alert" className="empty:contents">
        {formMessage ? <div className="rounded-xl border border-danger bg-danger-bg px-4 py-3 text-small text-danger">{formMessage}</div> : null}
      </div>
      <div role="status" aria-live="polite" aria-atomic="true" className="empty:contents">
        {success ? <div className="rounded-xl border border-success bg-success-bg px-4 py-3 text-small text-success">{t('account.password.success')}</div> : null}
      </div>
      <Button type="submit" disabled={pending} className="w-full sm:w-auto sm:justify-self-start">
        {pending ? t('account.password.submitting') : t('account.password.submit')}
      </Button>
    </form>
  );
}

/**
 * The password settings section. Only a session that signed in with the password may change it, so the other kinds
 * state plainly why there is no form and what to do instead: the refusal is a fact about this session, not an error.
 */
export function AccountPasswordSection({ session, onChanged, onSignedOut }: {
  session: Session;
  onChanged: (session: Session) => void;
  onSignedOut: () => void;
}) {
  const { t } = useI18n();
  const [refused, setRefused] = useState(false);
  const canChange = session.signInMethod === 'password' && !refused;
  // A refusal during submit means the session kind changed under the form, which reads like a pre-upgrade session.
  const explanation = session.signInMethod === 'oidc' && !refused
    ? t('account.password.provider')
    : t('account.password.sessionRequired');

  return (
    <section aria-labelledby="account-password-heading" className="border-t border-line pt-6">
      <h2 id="account-password-heading" className="text-h2 text-ink">{t('account.password.title')}</h2>
      <p className="mt-1 max-w-prose text-small text-muted">
        {canChange ? t('account.password.body') : explanation}
      </p>
      {canChange ? (
        <PasswordForm
          signInMethod={session.signInMethod}
          onChanged={onChanged}
          onSignedOut={onSignedOut}
          onRefused={() => setRefused(true)}
        />
      ) : null}
    </section>
  );
}
