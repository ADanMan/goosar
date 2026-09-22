// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { ApiClient } from '../api/client';
import { setApiInstance } from '../api';
import { createAuthStore, registerAuthStore, useAuthStore } from '../auth';
import type { StorageAdapter, User } from '../types';
import { completeOnboarding, retryOnboardingCompletionDelivery } from './store';

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

function makeUser(onboardedAt: string | null): User {
  return {
    id: 'u1',
    email: 'user@goosar.ru',
    name: 'User',
    onboarded_at: onboardedAt,
    onboarding_questionnaire: {},
  } as unknown as User;
}

const NOT_ONBOARDED = makeUser(null);
const ONBOARDED = makeUser('2026-09-01T00:00:00Z');

const apiMocks = {
  getMe: vi.fn(),
  markOnboardingComplete: vi.fn(),
};

function markPresent(): boolean {
  return window.localStorage.getItem('goosar_onboarding_completion_pending:u1') === '1';
}

beforeEach(() => {
  vi.clearAllMocks();
  window.localStorage.clear();
  const fakeApi = apiMocks as unknown as ApiClient;
  setApiInstance(fakeApi);
  const store = createAuthStore({ api: fakeApi, storage: memStorage() });
  registerAuthStore(store);
  store.setState({ user: NOT_ONBOARDED, isLoading: false });
});

describe('completeOnboarding', () => {
  it('marks the completion pending BEFORE the request leaves', async () => {
    let markedWhenCalled = false;
    apiMocks.markOnboardingComplete.mockImplementation(() => {
      markedWhenCalled = markPresent();
      return Promise.reject(new Error('ERR_CONNECTION_TIMED_OUT'));
    });

    await expect(completeOnboarding('full', 'ws_1')).rejects.toThrow();

    expect(markedWhenCalled).toBe(true);
    expect(markPresent()).toBe(true);
  });

  it('keeps the mark when the POST fails', async () => {
    apiMocks.markOnboardingComplete.mockRejectedValue(new Error('boom'));

    await expect(completeOnboarding('full', 'ws_1')).rejects.toThrow('boom');

    expect(markPresent()).toBe(true);
    expect(apiMocks.getMe).not.toHaveBeenCalled();
  });

  it('keeps the mark when the POST lands but the refresh does not', async () => {
    apiMocks.markOnboardingComplete.mockResolvedValue(ONBOARDED);
    apiMocks.getMe.mockRejectedValue(new Error('offline'));

    await expect(completeOnboarding('full', 'ws_1')).rejects.toThrow('offline');

    expect(markPresent()).toBe(true);
  });

  it('drops the mark only once a refreshed user carries onboarded_at', async () => {
    apiMocks.markOnboardingComplete.mockResolvedValue(ONBOARDED);
    apiMocks.getMe.mockResolvedValue(ONBOARDED);

    await completeOnboarding('full', 'ws_1');

    expect(markPresent()).toBe(false);
  });

  it('keeps the mark when the refreshed user still has no onboarded_at', async () => {
    apiMocks.markOnboardingComplete.mockResolvedValue(NOT_ONBOARDED);
    apiMocks.getMe.mockResolvedValue(NOT_ONBOARDED);

    await completeOnboarding('full', 'ws_1');

    expect(markPresent()).toBe(true);
  });

  it('leaves no mark when an already-onboarded user replays and the POST fails', async () => {
    useAuthStore.getState().setUser(ONBOARDED);
    apiMocks.markOnboardingComplete.mockRejectedValue(new Error('ERR_CONNECTION_TIMED_OUT'));

    await expect(completeOnboarding('full', 'ws_1')).rejects.toThrow();

    expect(markPresent()).toBe(false);
  });

  it('still marks a first pass — the replay exemption is not a blanket one', async () => {
    apiMocks.markOnboardingComplete.mockRejectedValue(new Error('boom'));

    await expect(completeOnboarding('full', 'ws_1')).rejects.toThrow('boom');

    expect(markPresent()).toBe(true);
  });

  it('still sends the completion when this browser refuses to store', async () => {
    const denied = new DOMException('blocked', 'SecurityError');
    const refusing = {
      getItem: () => {
        throw denied;
      },
      setItem: () => {
        throw denied;
      },
      removeItem: () => {
        throw denied;
      },
    };
    const real = Object.getOwnPropertyDescriptor(window, 'localStorage')!;
    Object.defineProperty(window, 'localStorage', {
      configurable: true,
      value: refusing,
    });
    try {
      apiMocks.markOnboardingComplete.mockResolvedValue(ONBOARDED);
      apiMocks.getMe.mockResolvedValue(ONBOARDED);

      await expect(completeOnboarding('full', 'ws_1')).resolves.toBeUndefined();

      expect(apiMocks.markOnboardingComplete).toHaveBeenCalledTimes(1);
    } finally {
      Object.defineProperty(window, 'localStorage', real);
    }
  });
});

describe('retryOnboardingCompletionDelivery', () => {
  it('asks the server first and sends nothing when it already onboarded us', async () => {
    window.localStorage.setItem('goosar_onboarding_completion_pending:u1', '1');
    apiMocks.getMe.mockResolvedValue(ONBOARDED);

    await expect(retryOnboardingCompletionDelivery()).resolves.toBe('confirmed');

    expect(apiMocks.markOnboardingComplete).not.toHaveBeenCalled();
    expect(markPresent()).toBe(false);
  });

  it('sends the completion anyway when the probing read fails', async () => {
    window.localStorage.setItem('goosar_onboarding_completion_pending:u1', '1');
    apiMocks.getMe
      .mockRejectedValueOnce(new TypeError('Failed to fetch'))
      .mockResolvedValue(ONBOARDED);

    await expect(retryOnboardingCompletionDelivery()).resolves.toBe('confirmed');

    expect(apiMocks.markOnboardingComplete).toHaveBeenCalledTimes(1);
    expect(markPresent()).toBe(false);
  });

  it('re-sends the completion when the server still has no flag', async () => {
    window.localStorage.setItem('goosar_onboarding_completion_pending:u1', '1');
    apiMocks.getMe.mockResolvedValueOnce(NOT_ONBOARDED).mockResolvedValueOnce(ONBOARDED);
    apiMocks.markOnboardingComplete.mockResolvedValue(ONBOARDED);

    await expect(retryOnboardingCompletionDelivery()).resolves.toBe('confirmed');

    expect(apiMocks.markOnboardingComplete).toHaveBeenCalledWith();
    expect(markPresent()).toBe(false);
  });

  it('keeps the mark and rethrows when re-delivery fails', async () => {
    window.localStorage.setItem('goosar_onboarding_completion_pending:u1', '1');
    apiMocks.getMe.mockResolvedValue(NOT_ONBOARDED);
    apiMocks.markOnboardingComplete.mockRejectedValue(new Error('still down'));

    await expect(retryOnboardingCompletionDelivery()).rejects.toThrow('still down');

    expect(markPresent()).toBe(true);
  });

  it('does not report a failed read-back as a failed delivery', async () => {
    window.localStorage.setItem('goosar_onboarding_completion_pending:u1', '1');
    apiMocks.getMe
      .mockResolvedValueOnce(NOT_ONBOARDED)
      .mockRejectedValueOnce(new Error('Failed to fetch'));
    apiMocks.markOnboardingComplete.mockResolvedValue(ONBOARDED);

    await expect(retryOnboardingCompletionDelivery()).resolves.toBe('delivered_unverified');

    expect(markPresent()).toBe(true);
  });

  it("separates 'delivered, still not marked' from 'delivered, unread'", async () => {
    window.localStorage.setItem('goosar_onboarding_completion_pending:u1', '1');
    apiMocks.getMe.mockResolvedValue(NOT_ONBOARDED);
    apiMocks.markOnboardingComplete.mockResolvedValue(NOT_ONBOARDED);

    await expect(retryOnboardingCompletionDelivery()).resolves.toBe('not_confirmed');
    expect(markPresent()).toBe(true);
  });
});
