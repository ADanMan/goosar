import { describe, expect, it } from 'vitest';
import { updaterHardDisabled } from './update-gate';

describe('updaterHardDisabled', () => {
  it('is off by default (cloud builds unchanged)', () => {
    expect(updaterHardDisabled({})).toBe(false);
  });

  it('turns on for the documented spellings', () => {
    expect(updaterHardDisabled({ GOOSAR_DESKTOP_NO_UPDATER: '1' })).toBe(true);
    expect(updaterHardDisabled({ GOOSAR_DESKTOP_NO_UPDATER: 'true' })).toBe(true);
  });

  it('stays off for other values', () => {
    expect(updaterHardDisabled({ GOOSAR_DESKTOP_NO_UPDATER: '0' })).toBe(false);
    expect(updaterHardDisabled({ GOOSAR_DESKTOP_NO_UPDATER: '' })).toBe(false);
    expect(updaterHardDisabled({ GOOSAR_DESKTOP_NO_UPDATER: 'yes' })).toBe(false);
  });
});
