import { useEffect, useId, useRef, type RefObject } from 'react';
import { Button } from '../../components/ui/button';
import { Dialog } from '../../components/ui/dialog';
import { Input } from '../../components/ui/input';
import { Label } from '../../components/ui/label';
import { Notice } from '../../components/ui/notice';
import { Select } from '../../components/ui/select';
import { useI18n } from '../../i18n';
import { formatTimezoneLabel } from '../auth/onboardingModel';
import { AccountPicker } from './AccountPicker';
import type { HouseholdCreate } from './useHouseholdCreate';

/** Hint first, then the field's own error when it owns one, so a refusal is part of the field's description. */
function describedBy(...ids: (string | undefined)[]): string | undefined {
  const list = ids.filter(Boolean);
  return list.length ? list.join(' ') : undefined;
}

/**
 * Starts a household: a name, the timezone its dates follow, and the existing account that becomes its first
 * administrator. All three are required, so Create waits for them; the server stays the authority on what a name,
 * a timezone or an account may be, and every refusal it names a field for is said on that field.
 *
 * The account directory is the add dialog's picker: one search, one chosen account, and Enter in the field only
 * ever searches. A failure with nothing to point at keeps the dialog and everything typed in it, so trying again is
 * one press away.
 */
export function CreateHouseholdDialog({ create }: { create: HouseholdCreate }) {
  const { t } = useI18n();
  const uid = useId();
  const nameRef = useRef<HTMLInputElement>(null);
  const zoneRef = useRef<HTMLSelectElement>(null);
  const adminRef = useRef<HTMLInputElement>(null);
  const failure = create.failure;
  const fieldFailure = failure?.kind === 'field' ? failure : null;
  const nameError = fieldFailure?.field === 'name' ? t(fieldFailure.message) : null;
  const zoneError = fieldFailure?.field === 'timezone' ? t(fieldFailure.message) : null;
  const adminError = fieldFailure?.field === 'adminUserId' ? t(fieldFailure.message) : null;

  // The field the server refused takes the focus, so a keyboard reader is put where the answer is.
  useEffect(() => {
    if (!fieldFailure) return;
    if (fieldFailure.field === 'name') nameRef.current?.focus();
    else if (fieldFailure.field === 'timezone') zoneRef.current?.focus();
    else adminRef.current?.focus();
  }, [fieldFailure]);

  return <Dialog
    open={create.open}
    onOpenChange={(next) => { if (!next) create.cancel(); }}
    title={t('install.create.title')}
    busy={create.busy}
    triggerRef={create.triggerRef}
    initialFocusRef={nameRef}
  >
    <div className="space-y-4">
      <p className="max-w-prose text-small text-muted">{t('install.create.intro')}</p>
      <NameField create={create} fieldRef={nameRef} error={nameError} uid={uid} />
      <TimezoneField create={create} fieldRef={zoneRef} error={zoneError} uid={uid} />
      <AdminField create={create} fieldRef={adminRef} error={adminError} uid={uid} />
      {failure?.kind === 'error' ? <Notice tone="error">{t('install.create.error')}</Notice> : null}
      <Footer create={create} />
    </div>
  </Dialog>;
}

type FieldProps = { create: HouseholdCreate; error: string | null; uid: string };

/** The name the server will trim and count in code points; the hint says the rule before the server has to. */
function NameField({ create, error, uid, fieldRef }: FieldProps & { fieldRef: RefObject<HTMLInputElement | null> }) {
  const { t } = useI18n();
  const id = `${uid}-name`;
  const hintId = `${id}-hint`;
  const errorId = `${id}-error`;
  return <div className="space-y-2">
    <Label htmlFor={id}>{t('install.create.name')}</Label>
    <Input
      ref={fieldRef}
      id={id}
      type="text"
      autoComplete="off"
      value={create.name}
      onChange={(event) => create.editName(event.target.value)}
      aria-invalid={error !== null}
      aria-describedby={describedBy(hintId, error ? errorId : undefined)}
    />
    <p id={hintId} className="text-small text-muted">{t('install.create.name.hint')}</p>
    {error ? <p id={errorId} role="alert" className="text-small text-danger">{error}</p> : null}
  </div>;
}

/** The zone every date in the household follows; the hint repeats the chosen one in the reader's own words. */
function TimezoneField({ create, error, uid, fieldRef }: FieldProps & { fieldRef: RefObject<HTMLSelectElement | null> }) {
  const { t, locale } = useI18n();
  const id = `${uid}-zone`;
  const hintId = `${id}-hint`;
  const errorId = `${id}-error`;
  return <div className="space-y-2">
    <Label htmlFor={id}>{t('onboarding.timezone')}</Label>
    <Select
      ref={fieldRef}
      id={id}
      value={create.timezone}
      onChange={(event) => create.editTimezone(event.target.value)}
      aria-invalid={error !== null}
      aria-describedby={describedBy(hintId, error ? errorId : undefined)}
    >
      {create.zones.map((zone) => <option key={zone} value={zone}>{formatTimezoneLabel(zone, locale)}</option>)}
    </Select>
    <p id={hintId} className="text-small text-muted">
      {t('onboarding.timezone.hint', { timezone: formatTimezoneLabel(create.timezone, locale) })}
    </p>
    {error ? <p id={errorId} role="alert" className="text-small text-danger">{error}</p> : null}
  </div>;
}

/** The account directory, named for what this choice means here: the household's first administrator. */
function AdminField({ create, error, uid, fieldRef }: FieldProps & { fieldRef: RefObject<HTMLInputElement | null> }) {
  const { t } = useI18n();
  const headingId = `${uid}-admin-heading`;
  return <div role="group" aria-labelledby={headingId} className="space-y-1.5">
    <h3 id={headingId} className="text-label font-semibold text-ink">{t('install.create.admin')}</h3>
    <AccountPicker
      id={`${uid}-admin`}
      query={create.query}
      chosen={create.chosen}
      search={create.search}
      queryError={create.queryError}
      refusals={create.refusals}
      choiceError={error}
      hint={t('install.create.admin.hint')}
      inputRef={fieldRef}
      busy={create.busy}
      onQuery={create.editQuery}
      onSearch={create.searchDirectory}
      onChoose={create.choose}
      onNext={create.next}
      onPrevious={create.previous}
      onRetry={create.retry}
    />
  </div>;
}

/** Cancel leaves everything as it was; Create waits until a name, a zone and an account are all there. */
function Footer({ create }: { create: HouseholdCreate }) {
  const { t } = useI18n();
  return <div className="flex flex-wrap justify-end gap-2">
    <Button variant="quiet" disabled={create.busy} onClick={create.cancel}>{t('admin.action.cancel')}</Button>
    <Button disabled={create.busy || !create.canSubmit} onClick={() => { void create.submit(); }}>
      {create.busy ? t('install.create.creating') : t('install.create.submit')}
    </Button>
  </div>;
}
