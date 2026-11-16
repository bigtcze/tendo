import { useEffect, useId, useRef } from 'react';
import { Button } from '../../components/ui/button';
import { Dialog } from '../../components/ui/dialog';
import { Input } from '../../components/ui/input';
import { Label } from '../../components/ui/label';
import { Notice } from '../../components/ui/notice';
import { useI18n, type MessageKey } from '../../i18n';
import type { AccountDeletion, DeletionFailure } from './useAccountDeletion';

/** The sentence a refusal that changed nothing says, under the consequences and in the dialog's quiet voice. */
const FAILURES: Record<Exclude<DeletionFailure['kind'], 'mismatch'>, MessageKey> = {
  lastAdmin: 'install.accounts.delete.error.lastAdmin',
  lastInstanceAdmin: 'install.accounts.delete.error.lastInstanceAdmin',
  self: 'install.accounts.delete.error.self',
  rejected: 'install.accounts.delete.error.rejected',
  error: 'install.accounts.delete.error.failed',
};

/** The consequences, in the order a reader needs them: what stops, what stays, and that it is final. */
const CONSEQUENCES = [
  'install.accounts.delete.signIn',
  'install.accounts.delete.households',
  'install.accounts.delete.history',
  'install.accounts.delete.person',
  'install.accounts.delete.undone',
] as const;

/** The refusal that speaks about the whole action; a mismatch is never one of them, because it belongs on the field. */
function refusalKind(failure: DeletionFailure | null | undefined) {
  return failure && failure.kind !== 'mismatch' ? failure.kind : null;
}

/**
 * Asks before an installation account is deleted and says in plain words what changes everywhere at once — it is not
 * a household membership removal, so the dialog never speaks of “this household”.
 *
 * The confirmation is the login typed again: the button stays disabled until the text is exactly the login (no
 * trimming, no case folding), and the server compares it once more before anything happens. A refusal that changed
 * nothing keeps the dialog and what was typed, so fixing the cause and trying again is one press away.
 */
export function DeleteAccountDialog({ deletion }: { deletion: AccountDeletion }) {
  const { t } = useI18n();
  const uid = useId();
  const confirmRef = useRef<HTMLInputElement>(null);
  const refusalRef = useRef<HTMLSpanElement>(null);
  const login = deletion.target?.login ?? '';
  const failure = deletion.failure;
  const mismatch = failure?.kind === 'mismatch';
  const sentence = refusalKind(failure);
  // Exactly the login, character for character: the server is the authority, so the client never “helps”.
  const matched = deletion.target !== null && deletion.confirmation === login;

  // Every answer lands on the element it is about. A mismatch belongs to the field; every other refusal is a sentence
  // the reader has to hear, and it takes the focus because the control that was pressed is disabled while the request
  // runs. Each refusal is a new object, so an identical one answered twice is a new answer and refocuses too.
  useEffect(() => {
    if (!failure) return;
    if (failure.kind === 'mismatch') { confirmRef.current?.focus(); return; }
    refusalRef.current?.focus();
  }, [failure]);

  return <Dialog
    open={deletion.target !== null}
    onOpenChange={(open) => { if (!open) deletion.cancel(); }}
    title={t('install.accounts.delete.title', { login })}
    busy={deletion.busy}
    triggerRef={deletion.triggerRef}
    returnFocusRef={deletion.returnFocusRef}
    initialFocusRef={confirmRef}
  >
    <div className="space-y-4">
      <p className="max-w-prose text-body text-ink">{t('install.accounts.delete.lead')}</p>
      <ul className="max-w-prose list-disc space-y-1.5 pl-5 text-body text-muted">
        {CONSEQUENCES.map((key) => <li key={key}>{t(key)}</li>)}
      </ul>
      <div className="space-y-2">
        <Label htmlFor={`${uid}-confirm`} className="min-w-0 break-words">{t('install.accounts.delete.confirm', { login })}</Label>
        <Input
          ref={confirmRef}
          id={`${uid}-confirm`}
          type="text"
          autoComplete="off"
          autoCapitalize="none"
          autoCorrect="off"
          spellCheck={false}
          value={deletion.confirmation}
          onChange={(event) => deletion.edit(event.target.value)}
          disabled={deletion.busy}
          aria-invalid={mismatch}
          aria-describedby={mismatch ? `${uid}-mismatch` : undefined}
        />
        {mismatch
          ? <p id={`${uid}-mismatch`} role="alert" className="text-small text-danger">{t('install.accounts.delete.mismatch')}</p>
          : null}
      </div>
    </div>
    {/* Kept mounted and left out of the body spacing, so a refusal is announced by a host that was already there. */}
    <div role="status" aria-atomic="true" data-delete-refusal-host>
      {sentence
        ? <Notice tone={sentence === 'error' ? 'error' : 'neutral'} role={null} className="mt-4 min-w-0 break-words">
          <span ref={refusalRef} tabIndex={-1} data-delete-refusal className="block min-w-0 break-words outline-none">
            {t(FAILURES[sentence], { login })}
          </span>
        </Notice>
        : null}
    </div>
    <div className="mt-4 flex flex-wrap justify-end gap-2">
      <Button variant="quiet" disabled={deletion.busy} onClick={deletion.cancel}>{t('admin.action.cancel')}</Button>
      <Button disabled={deletion.busy || !matched} onClick={() => { void deletion.confirm(); }}>
        {deletion.busy ? t('install.accounts.delete.working') : t('install.accounts.delete.action')}
      </Button>
    </div>
  </Dialog>;
}
