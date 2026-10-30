import { describe, expect, it } from 'vitest';
import {
  errorKey, errorTarget, hasEmptyName, hasUnknownMember, shouldShowClearedLinkNotice,
  shouldShowLinkedAccount, submittedValues,
} from './subjectFormModel';
import type { ErrorTarget, FormError } from './subjectFormModel';

describe('subject form model', () => {
  const errorCases: Array<{ error: FormError; expected: ReturnType<typeof errorKey> }> = [
    { error: { kind: 'empty' }, expected: 'subjects.error.nameEmpty' },
    { error: { kind: 'failed' }, expected: 'subjects.error.save' },
    { error: { kind: 'invalid', field: 'type', code: 'invalid_type' }, expected: 'subjects.error.type' },
    { error: { kind: 'invalid', field: 'linkedUserId', code: 'requires_person' }, expected: 'subjects.error.linkRequiresPerson' },
    { error: { kind: 'invalid', field: 'linkedUserId', code: 'invalid_reference' }, expected: 'subjects.error.linkInvalid' },
    { error: { kind: 'invalid', field: 'name', code: 'invalid_characters' }, expected: 'subjects.error.nameCharacters' },
    { error: { kind: 'invalid', field: 'name', code: 'invalid_length' }, expected: 'subjects.error.nameLength' },
    { error: { kind: 'invalid', field: 'name', code: 'unknown' }, expected: 'subjects.error.nameInvalid' },
  ];
  const targetCases: Array<{ error: FormError; expected: ErrorTarget }> = [
    { error: { kind: 'empty' }, expected: 'name' },
    { error: { kind: 'failed' }, expected: null },
    { error: { kind: 'invalid', field: 'name', code: 'invalid_length' }, expected: 'name' },
    { error: { kind: 'invalid', field: 'type', code: 'invalid_type' }, expected: 'type' },
    { error: { kind: 'invalid', field: 'linkedUserId', code: 'invalid_reference' }, expected: 'linkedUserId' },
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
    expect(submittedValues(' name ', 'person', 'u1')).toEqual({ name: ' name ', type: 'person', linkedUserId: 'u1' });
  });

  it('only submits links for people and only shows the linked selector when eligible', () => {
    expect(submittedValues('Home', 'home', 'u1').linkedUserId).toBeNull();
    expect(shouldShowLinkedAccount('person', false, 2, null)).toBe(false);
    expect(shouldShowLinkedAccount('home', true, 2, 'u1')).toBe(false);
    expect(shouldShowLinkedAccount('person', true, 0, null)).toBe(false);
    expect(shouldShowLinkedAccount('person', true, 1, null)).toBe(true);
    expect(shouldShowLinkedAccount('person', true, 0, 'u1')).toBe(true);
  });

  it('preserves unknown member options and shows clearing notice only for an initial existing link', () => {
    expect(hasUnknownMember('unknown', [{ userId: 'known' }])).toBe(true);
    expect(hasUnknownMember('known', [{ userId: 'known' }])).toBe(false);
    expect(hasUnknownMember(null, [])).toBe(false);
    expect(hasUnknownMember('', [])).toBe(false);
    expect(shouldShowClearedLinkNotice('vehicle', 'u1', 'u1')).toBe(true);
    expect(shouldShowClearedLinkNotice('vehicle', '', '')).toBe(false);
    expect(shouldShowClearedLinkNotice('vehicle', 'u1', null)).toBe(false);
    expect(shouldShowClearedLinkNotice('person', 'u1', 'u1')).toBe(false);
  });
});
