import { describe, it, expect } from 'vitest';
import { memberLabel } from './member-label';

describe('memberLabel', () => {
  it('uses the display name when there is one', () => {
    expect(memberLabel({ name: 'Alice', email: 'a@goosar.test' })).toBe('Alice');
  });

  it('falls back to the email when the name is empty', () => {
    expect(memberLabel({ name: '', email: 'a@goosar.test' })).toBe('a@goosar.test');
  });

  it('trims BOTH sides, so whitespace never becomes an invisible target', () => {
    expect(memberLabel({ name: '  Alice  ', email: 'a@goosar.test' })).toBe('Alice');
    expect(memberLabel({ name: '   ', email: '  a@goosar.test  ' })).toBe('a@goosar.test');
    expect(memberLabel({ name: '   ', email: '   ' })).toBe('');
  });

  it('returns an empty label when nothing identifies the member', () => {
    expect(memberLabel({ name: '', email: '' })).toBe('');
  });
});
