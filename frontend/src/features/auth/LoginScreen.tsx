import { useRef, useState, type FormEvent } from 'react';
import { Button } from '../../components/ui/button';
import { Input } from '../../components/ui/input';
import { Label } from '../../components/ui/label';
import { useI18n, type MessageKey } from '../../i18n';
import { login, type Session } from './sessionApi';
import { Heading } from '../../app/Heading';

const errorKeys: Record<'invalid' | 'rateLimited' | 'unavailable', MessageKey> = {
  invalid: 'login.error.invalid',
  rateLimited: 'login.error.rateLimited',
  unavailable: 'login.error.unavailable',
};

export function LoginScreen({ onSignedIn }: { onSignedIn: (session: Session) => void }) {
  const { t } = useI18n();
  const [loginName, setLoginName] = useState('');
  const [password, setPassword] = useState('');
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<keyof typeof errorKeys | null>(null);
  const passwordRef = useRef<HTMLInputElement>(null);

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (pending) return;
    setPending(true);
    setError(null);
    const result = await login(loginName, password);
    if (result.kind === 'ok') {
      onSignedIn(result.session);
      return;
    }
    setPassword('');
    setError(result.kind);
    setPending(false);
    passwordRef.current?.focus();
  }

  return (
    <section className="settle">
      <Heading className="font-display text-3xl leading-tight sm:text-4xl">{t('login.title')}</Heading>
      <form onSubmit={submit} className="mt-8 space-y-5" noValidate>
        <div className="space-y-2">
          <Label htmlFor="login">{t('login.login')}</Label>
          <Input
            id="login"
            name="login"
            type="text"
            autoComplete="username"
            autoCapitalize="none"
            spellCheck={false}
            required
            value={loginName}
            onChange={(e) => setLoginName(e.target.value)}
            aria-invalid={error === 'invalid'}
            aria-describedby={error ? 'login-error' : undefined}
          />
        </div>
        <div className="space-y-2">
          <Label htmlFor="password">{t('login.password')}</Label>
          <Input
            id="password"
            name="password"
            type="password"
            autoComplete="current-password"
            required
            ref={passwordRef}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            aria-invalid={error === 'invalid'}
            aria-describedby={error ? 'login-error' : undefined}
          />
        </div>
        <div id="login-error" role="alert" className="text-base text-danger">
          {error ? t(errorKeys[error]) : null}
        </div>
        <Button type="submit" disabled={pending} className="w-full sm:w-auto">
          {pending ? t('login.submitting') : t('login.submit')}
        </Button>
      </form>
    </section>
  );
}
