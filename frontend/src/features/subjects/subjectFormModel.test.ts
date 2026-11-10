import { describe, expect, it } from 'vitest';
import {
  errorKey, errorTarget, hasEmptyName, submittedValues,
} from './subjectFormModel';
import type { ErrorTarget, FormError } from './subjectFormModel';

describe('subject form model', () => {
  const errorCases: Array<{ error: FormError; expected: ReturnType<typeof errorKey> }> = [
    { error: { kind: 'empty' }, expected: 'subjects.error.nameEmpty' },
    { error: { kind: 'failed' }, expected: 'subjects.error.save' },
    { error: { kind: 'locked' }, expected: 'subjects.error.memberPersonLocked' },
    { error: { kind: 'exists' }, expected: 'subjects.error.memberPersonExists' },
    { error: { kind: 'invalid', field: 'type', code: 'invalid_type' }, expected: 'subjects.error.type' },
    { error: { kind: 'invalid', field: 'name', code: 'invalid_characters' }, expected: 'subjects.error.nameCharacters' },
    { error: { kind: 'invalid', field: 'name', code: 'invalid_length' }, expected: 'subjects.error.nameLength' },
    { error: { kind: 'invalid', field: 'name', code: 'unknown' }, expected: 'subjects.error.nameInvalid' },
  ];
  const targetCases: Array<{ error: FormError; expected: ErrorTarget }> = [
    { error: { kind: 'empty' }, expected: 'name' },
    { error: { kind: 'failed' }, expected: null },
    { error: { kind: 'locked' }, expected: null },
    { error: { kind: 'exists' }, expected: null },
    { error: { kind: 'invalid', field: 'name', code: 'invalid_length' }, expected: 'name' },
    { error: { kind: 'invalid', field: 'type', code: 'invalid_type' }, expected: 'type' },
  ];

  it.each(errorCases)('maps $error to $expected', ({ error, expected }) => {
    expect(errorKey(error)).toBe(expected);
  });

  it.each(targetCases)('targets $error at $expected', ({ error, expected }) => {
    expect(errorTarget(error)).toBe(expected);
  });

  it('checks whitespace-only names while retaining the exact submitted name', () => {
    expect(hasEmptyName(' \t\n')).toBe(true);
    expect(hasEmptyName(' name ')).toBe(false);
    expect(submittedValues(' name ', 'person')).toEqual({ name: ' name ', type: 'person' });
  });

  it('submits only the name and type: account links move through the member-person transfer', () => {
    expect(submittedValues('Home', 'home')).toEqual({ name: 'Home', type: 'home' });
    expect(submittedValues('Anna', 'person')).toEqual({ name: 'Anna', type: 'person' });
  });

  it('maps a stray server link field to the generic name problem without a focus target', () => {
    const stray: FormError = { kind: 'invalid', field: 'linkedUserId', code: 'invalid_reference' };
    expect(errorKey(stray)).toBe('subjects.error.nameInvalid');
    expect(errorTarget(stray)).toBeNull();
  });
});
