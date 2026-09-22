import { describe, it, expect } from 'vitest';
import enSettings from '@goosar/views/locales/en/settings.json';
import ruSettings from '@goosar/views/locales/ru/settings.json';
import { KINIT_FAILURE_REASONS, kinitFailureMessage } from './kinit-copy';

function fakeT(bundle: unknown) {
  return ((selector: (b: never) => string) => {
    const raw = selector(bundle as never);
    if (typeof raw !== 'string') throw new Error('missing key');
    return raw;
  }) as never;
}

const en = fakeT(enSettings);
const ru = fakeT(ruSettings);

describe('kinit failure copy', () => {
  it('has a Russian sentence for every reason the main process can emit', () => {
    for (const reason of KINIT_FAILURE_REASONS) {
      const text = kinitFailureMessage(ru, reason, 'English fallback.');
      expect(/[а-яА-Я]/.test(text), reason).toBe(true);
      expect(text, reason).not.toBe('English fallback.');
    }
  });

  it('falls back to the main process message for a reason it does not know', () => {
    expect(kinitFailureMessage(ru, 'quantum_realm', 'Ticket refused.')).toBe('Ticket refused.');
  });

  it('falls back to the generic sentence when there is no message either', () => {
    const text = kinitFailureMessage(ru, 'quantum_realm', '');
    expect(/[а-яА-Я]/.test(text)).toBe(true);
  });

  it('keeps the English wording usable in the English bundle', () => {
    expect(kinitFailureMessage(en, 'wrong_password', 'x')).toMatch(/password/i);
  });
});
