import { useId, useRef } from 'react';
import { Button } from '../../components/ui/button';
import { Dialog } from '../../components/ui/dialog';
import { Notice } from '../../components/ui/notice';
import { cn } from '../../lib/cn';
import { useI18n } from '../../i18n';
import type { AdminAccountRole } from './instanceAdminApi';
import { AccountPicker } from './AccountPicker';
import type { Assignment } from './useAccountAssignment';

const roles: AdminAccountRole[] = ['member', 'admin'];

/**
 * Puts an existing account into this household: one login search, one chosen account, one role. The dialog is a
 * search form and a form of its own — Enter in the login field searches, and only "Add to household" adds — so a
 * reader can look around without ever being one keystroke from changing the household.
 *
 * Every outcome that keeps the dialog open points at the field it is about (the search box, or the role) and every
 * outcome that ends it is announced by the screen's own live region.
 */
export function AddAccountDialog({ assignment, householdName }: {
  assignment: Assignment;
  /** Null on a direct load of the page: the title then says “an account”, not a household name it never read. */
  householdName: string | null;
}) {
  const { t } = useI18n();
  const uid = useId();
  const inputRef = useRef<HTMLInputElement>(null);
  const { open, busy, triggerRef, queryError, refusals, chosen, cancel } = assignment;
  const title = householdName === null
    ? t('install.add.title')
    : t('install.add.titleNamed', { household: householdName });

  return <Dialog
    open={open}
    onOpenChange={(next) => { if (!next) cancel(); }}
    title={title}
    busy={busy}
    triggerRef={triggerRef}
    initialFocusRef={inputRef}
  >
    <div className="space-y-4">
      <p className="max-w-prose text-small text-muted">{t('install.add.intro')}</p>
      <AccountPicker
        id={`${uid}-account`}
        query={assignment.query}
        chosen={chosen}
        search={assignment.search}
        queryError={queryError}
        refusals={refusals}
        inputRef={inputRef}
        busy={busy}
        onQuery={assignment.editQuery}
        onSearch={assignment.searchDirectory}
        onChoose={assignment.choose}
        onNext={assignment.next}
        onPrevious={assignment.previous}
        onRetry={assignment.retry}
      />
      <RoleField role={assignment.role} onChange={assignment.chooseRole} disabled={busy}
        error={assignment.failure === 'requiresAdmin' ? t('install.add.error.requiresAdmin') : null} uid={uid} />
      {assignment.failure === 'alreadyMember' && chosen
        ? <Notice tone="error">{t('install.add.error.alreadyMember', { login: chosen.login })}</Notice>
        : null}
      {assignment.failure === 'failed' ? <Notice tone="error">{t('admin.error.action')}</Notice> : null}
      <div className="flex flex-wrap justify-end gap-2">
        <Button variant="quiet" disabled={busy} onClick={cancel}>{t('admin.action.cancel')}</Button>
        <Button disabled={busy || chosen === null} onClick={() => { void assignment.submit(); }}>
          {busy ? t('install.add.adding') : t('install.add.submit')}
        </Button>
      </div>
    </div>
  </Dialog>;
}

/** The role the account gets; the household's own wording explains what each one can do. */
function RoleField({ role, onChange, disabled, error, uid }: {
  role: AdminAccountRole;
  onChange: (role: AdminAccountRole) => void;
  disabled: boolean;
  error: string | null;
  uid: string;
}) {
  const { t } = useI18n();
  const errorId = `${uid}-role-error`;
  return <fieldset role="radiogroup" className="min-w-0 space-y-2"
    aria-invalid={Boolean(error)} aria-describedby={error ? errorId : undefined}>
    <legend className="text-label font-semibold text-ink">{t('install.add.role')}</legend>
    <div className="flex flex-wrap gap-2 pt-1">
      {roles.map((option) => <label key={option} className="relative">
        <input type="radio" name={`${uid}-role`} value={option} checked={role === option} disabled={disabled}
          onChange={() => onChange(option)} className="peer absolute inset-0 size-full cursor-pointer opacity-0" />
        <span className={cn(
          'inline-flex min-h-11 items-center rounded-xl border border-field bg-raised px-4 text-base text-ink transition-colors',
          'peer-hover:bg-sand peer-checked:border-accent peer-checked:bg-accent peer-checked:text-on-accent',
          'peer-focus-visible:outline-2 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-focus',
          'peer-disabled:cursor-not-allowed peer-disabled:opacity-60',
        )}>{t(option === 'admin' ? 'members.role.admin' : 'members.role.member')}</span>
      </label>)}
    </div>
    <p className="text-meta text-muted">{t('members.roles.explained')}</p>
    {error ? <p id={errorId} role="alert" className="text-small text-danger">{error}</p> : null}
  </fieldset>;
}
