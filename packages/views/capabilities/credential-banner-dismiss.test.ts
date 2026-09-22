import { describe, expect, it } from 'vitest';
import {
  MAX_CREDENTIAL_BANNER_APPEARANCES,
  nextAppearanceState,
  nextDismissState,
  shouldShowCredentialBanner,
} from './credential-banner-dismiss';

describe('shouldShowCredentialBanner', () => {
  it('says nothing when nothing is missing, whatever the stored count', () => {
    expect(shouldShowCredentialBanner({ count: 0, signature: '' }, '')).toBe(false);
    expect(shouldShowCredentialBanner({ count: 0, signature: 'outlook' }, '')).toBe(false);
  });

  it('runs out on APPEARANCES, so a reader who never presses a button still outlasts it', () => {
    let state = { count: 0, signature: '' };
    const signature = 'outlook';
    for (let i = 0; i < MAX_CREDENTIAL_BANNER_APPEARANCES; i++) {
      expect(shouldShowCredentialBanner(state, signature)).toBe(true);
      state = nextAppearanceState(state, signature);
    }
    expect(shouldShowCredentialBanner(state, signature)).toBe(false);
  });

  it('goes quiet about a list the moment it is dismissed once', () => {
    const state = nextDismissState({ count: 0, signature: '' }, 'outlook');
    expect(shouldShowCredentialBanner(state, 'outlook')).toBe(false);
  });

  it('keeps a dismissed list dismissed even if it is somehow shown again', () => {
    let state = nextDismissState({ count: 0, signature: '' }, 'outlook');
    state = nextAppearanceState(state, 'outlook');
    expect(shouldShowCredentialBanner(state, 'outlook')).toBe(false);
  });

  it('comes back when a NEW service starts needing a key', () => {
    const state = nextDismissState({ count: 0, signature: '' }, 'outlook');
    expect(shouldShowCredentialBanner(state, 'outlook')).toBe(false);
    expect(shouldShowCredentialBanner(state, 'atlassian,outlook')).toBe(true);
  });

  it('restarts the budget rather than inheriting it when the list changes', () => {
    const state = nextDismissState({ count: 0, signature: '' }, 'outlook');
    const afterNewList = nextAppearanceState(state, 'atlassian,outlook');
    expect(afterNewList).toEqual({ count: 1, signature: 'atlassian,outlook' });
    expect(shouldShowCredentialBanner(afterNewList, 'atlassian,outlook')).toBe(true);
  });

  it('counts repeated appearances of the same list', () => {
    const once = nextAppearanceState({ count: 0, signature: '' }, 'outlook');
    expect(once).toEqual({ count: 1, signature: 'outlook' });
    expect(nextAppearanceState(once, 'outlook')).toEqual({
      count: 2,
      signature: 'outlook',
    });
  });

  it('treats a shrinking list as a new list, so finishing one key is not silence about the rest', () => {
    const state = nextDismissState({ count: 0, signature: '' }, 'atlassian,outlook');
    expect(shouldShowCredentialBanner(state, 'atlassian,outlook')).toBe(false);
    expect(shouldShowCredentialBanner(state, 'outlook')).toBe(true);
  });
});
