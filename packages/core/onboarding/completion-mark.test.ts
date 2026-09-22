import { beforeEach, describe, expect, it } from 'vitest';
import type { StorageAdapter } from '../types';
import {
  clearOnboardingCompletionMark,
  hasPendingOnboardingCompletion,
  markOnboardingCompletionPending,
} from './completion-mark';

function memStorage(): StorageAdapter {
  const m = new Map<string, string>();
  return {
    getItem: (k) => m.get(k) ?? null,
    setItem: (k, v) => {
      m.set(k, v);
    },
    removeItem: (k) => {
      m.delete(k);
    },
  };
}

describe('onboarding completion mark', () => {
  let storage: StorageAdapter;

  beforeEach(() => {
    storage = memStorage();
  });

  it('is absent until it is written', () => {
    expect(hasPendingOnboardingCompletion('u1', storage)).toBe(false);
    markOnboardingCompletionPending('u1', storage);
    expect(hasPendingOnboardingCompletion('u1', storage)).toBe(true);
  });

  it('is dropped by clear', () => {
    markOnboardingCompletionPending('u1', storage);
    clearOnboardingCompletionMark('u1', storage);
    expect(hasPendingOnboardingCompletion('u1', storage)).toBe(false);
  });

  it('does not leak between users on the same machine', () => {
    markOnboardingCompletionPending('u1', storage);
    expect(hasPendingOnboardingCompletion('u2', storage)).toBe(false);
    clearOnboardingCompletionMark('u2', storage);
    expect(hasPendingOnboardingCompletion('u1', storage)).toBe(true);
  });

  it('stores nothing but the bare fact under a user-scoped key', () => {
    markOnboardingCompletionPending('u1', storage);
    expect(storage.getItem('goosar_onboarding_completion_pending:u1')).toBe('1');
  });

  it('is a no-op without a user id', () => {
    expect(hasPendingOnboardingCompletion(null, storage)).toBe(false);
    markOnboardingCompletionPending(null, storage);
    markOnboardingCompletionPending(undefined, storage);
    clearOnboardingCompletionMark(null, storage);
    expect(hasPendingOnboardingCompletion(null, storage)).toBe(false);
  });
});

describe('onboarding completion mark — storage that refuses', () => {
  function hostileStorage(): StorageAdapter {
    return {
      getItem: () => {
        throw new DOMException('blocked', 'SecurityError');
      },
      setItem: () => {
        throw new DOMException('quota', 'QuotaExceededError');
      },
      removeItem: () => {
        throw new DOMException('blocked', 'SecurityError');
      },
    };
  }

  it('never lets a refusing storage escape into the caller', () => {
    const hostile = hostileStorage();
    expect(() => markOnboardingCompletionPending('u1', hostile)).not.toThrow();
    expect(() => clearOnboardingCompletionMark('u1', hostile)).not.toThrow();
    expect(() => hasPendingOnboardingCompletion('u1', hostile)).not.toThrow();
  });

  it("answers 'nothing pending' when it cannot read", () => {
    expect(hasPendingOnboardingCompletion('u1', hostileStorage())).toBe(false);
  });
});
