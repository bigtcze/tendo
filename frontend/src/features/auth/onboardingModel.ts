import type { MessageKey } from '../../i18n';
import type { SetupField } from './sessionApi';

export type FormError =
  | { kind: 'setupCode' }
  | { kind: 'field'; field: SetupField }
  | { kind: 'empty'; field: 'setupCode' | SetupField }
  | { kind: 'setupDisabled' | 'rateLimited' | 'unavailable' };

export type FocusTarget = 'setupCode' | SetupField;

export const fieldIds: Record<FocusTarget, string> = {
  setupCode: 'setup-code',
  householdName: 'household-name',
  login: 'onboarding-login',
  password: 'onboarding-password',
  timezone: 'onboarding-timezone',
};

export function errorKey(error: FormError): MessageKey {
  switch (error.kind) {
    case 'setupCode': return 'onboarding.error.setupCode';
    case 'field': return `onboarding.error.${error.field}` as MessageKey;
    case 'empty':
      return error.field === 'setupCode'
        ? 'onboarding.error.setupCodeEmpty'
        : (`onboarding.error.${error.field}Empty` as MessageKey);
    case 'setupDisabled': return 'onboarding.error.setupDisabled';
    case 'rateLimited': return 'onboarding.error.rateLimited';
    case 'unavailable': return 'onboarding.error.unavailable';
  }
}

export function errorTarget(error: FormError): FocusTarget | null {
  if (error.kind === 'setupCode') return 'setupCode';
  if (error.kind === 'field' || error.kind === 'empty') return error.field;
  return null;
}

export function firstEmptyField(
  setupCode: string,
  householdName: string,
  login: string,
  password: string,
): FocusTarget | null {
  if (!setupCode.trim()) return 'setupCode';
  if (!householdName.trim()) return 'householdName';
  if (!login) return 'login';
  if (!password) return 'password';
  return null;
}

export function detectTimezone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
  } catch {
    return 'UTC';
  }
}

function supportedZones(): string[] {
  try {
    const intl = Intl as typeof Intl & { supportedValuesOf?: (key: string) => string[] };
    return intl.supportedValuesOf?.('timeZone') ?? [];
  } catch {
    return [];
  }
}

export function timezoneOptions(detected: string, supported = supportedZones()): string[] {
  return Array.from(new Set([...supported, 'UTC', detected])).sort((a, b) => a.localeCompare(b));
}
