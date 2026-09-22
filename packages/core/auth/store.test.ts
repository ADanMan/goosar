import { afterEach, describe, expect, it, vi } from 'vitest';
import type { ApiClient } from '../api/client';
import { ApiClient as RealApiClient } from '../api/client';
import type { StorageAdapter, User } from '../types';
import { createAuthStore, MFARequiredError } from './store';

afterEach(() => {
  vi.unstubAllGlobals();
});

const fakeUser: User = {
  id: 'u1',
  name: 'Alice',
  email: 'alice@example.com',
  avatar_url: null,
} as User;

function makeStorage(initial: Record<string, string> = {}): StorageAdapter & {
  snapshot: () => Record<string, string>;
} {
  const data = { ...initial };
  return {
    getItem: (k) => data[k] ?? null,
    setItem: (k, v) => {
      data[k] = v;
    },
    removeItem: (k) => {
      delete data[k];
    },
    snapshot: () => ({ ...data }),
  };
}

describe('authStore.verifyCode — token mode (#105)', () => {
  it('sets the token on the api client and persists it to storage before user is set', async () => {
    const storage = makeStorage();
    const api = {
      verifyCode: vi.fn().mockResolvedValue({ token: 'tok-123', user: fakeUser }),
      setToken: vi.fn(),
    } as unknown as ApiClient;
    const store = createAuthStore({ api, storage });

    expect(store.getState().user).toBeNull();

    const user = await store.getState().verifyCode('alice@example.com', '123456');

    expect(user).toEqual(fakeUser);
    expect(api.setToken).toHaveBeenCalledWith('tok-123');
    expect(storage.snapshot().goosar_token).toBe('tok-123');
    expect(store.getState().user).toEqual(fakeUser);
  });

  it('throws and does not persist a token or set user when the server returns an empty token', async () => {
    const storage = makeStorage();
    const api = {
      verifyCode: vi.fn().mockResolvedValue({ token: '', user: fakeUser }),
      setToken: vi.fn(),
    } as unknown as ApiClient;
    const store = createAuthStore({ api, storage });

    await expect(store.getState().verifyCode('alice@example.com', '123456')).rejects.toThrow();

    expect(api.setToken).not.toHaveBeenCalled();
    expect(storage.snapshot().goosar_token).toBeUndefined();
    expect(store.getState().user).toBeNull();
  });

  it('subsequent api.listWorkspaces() call carries the Authorization header after verifyCode resolves (end-to-end)', async () => {
    const fetchMock = vi.fn().mockImplementation((url: string) => {
      if ((url as string).endsWith('/auth/verify-code')) {
        return Promise.resolve(
          new Response(JSON.stringify({ token: 'tok-123', user: fakeUser }), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          }),
        );
      }
      return Promise.resolve(
        new Response(JSON.stringify([]), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      );
    });
    vi.stubGlobal('fetch', fetchMock);

    const api = new RealApiClient('https://api.example.test');
    const storage = makeStorage();
    const store = createAuthStore({ api, storage });

    await store.getState().verifyCode('alice@example.com', '123456');
    await api.listWorkspaces();

    const workspacesCall = fetchMock.mock.calls.find(([url]) =>
      (url as string).endsWith('/api/workspaces'),
    );
    expect(workspacesCall).toBeDefined();
    const headers = workspacesCall![1]!.headers as Record<string, string>;
    expect(headers['Authorization']).toBe('Bearer tok-123');
  });
});

describe('authStore.verifyCode — cookie mode', () => {
  it('does not call setToken or storage.setItem', async () => {
    const storage = makeStorage();
    const setItemSpy = vi.spyOn(storage, 'setItem');
    const api = {
      verifyCode: vi.fn().mockResolvedValue({ token: 'tok-123', user: fakeUser }),
      setToken: vi.fn(),
    } as unknown as ApiClient;
    const store = createAuthStore({ api, storage, cookieAuth: true });

    const user = await store.getState().verifyCode('alice@example.com', '123456');

    expect(user).toEqual(fakeUser);
    expect(api.setToken).not.toHaveBeenCalled();
    expect(setItemSpy).not.toHaveBeenCalled();
    expect(store.getState().user).toEqual(fakeUser);
  });

  it('does not throw when the token is empty (cookie backends legitimately omit it), and still sets the user', async () => {
    const storage = makeStorage();
    const setItemSpy = vi.spyOn(storage, 'setItem');
    const api = {
      verifyCode: vi.fn().mockResolvedValue({ token: '', user: fakeUser }),
      setToken: vi.fn(),
    } as unknown as ApiClient;
    const store = createAuthStore({ api, storage, cookieAuth: true });

    const user = await store.getState().verifyCode('alice@example.com', '123456');

    expect(user).toEqual(fakeUser);
    expect(store.getState().user).toEqual(fakeUser);
    expect(api.setToken).not.toHaveBeenCalled();
    expect(setItemSpy).not.toHaveBeenCalled();
  });

  it('does not throw when the token field is absent entirely, and still sets the user', async () => {
    const storage = makeStorage();
    const setItemSpy = vi.spyOn(storage, 'setItem');
    const api = {
      verifyCode: vi
        .fn()
        .mockResolvedValue({ user: fakeUser } as unknown as { token: string; user: User }),
      setToken: vi.fn(),
    } as unknown as ApiClient;
    const store = createAuthStore({ api, storage, cookieAuth: true });

    const user = await store.getState().verifyCode('alice@example.com', '123456');

    expect(user).toEqual(fakeUser);
    expect(store.getState().user).toEqual(fakeUser);
    expect(api.setToken).not.toHaveBeenCalled();
    expect(setItemSpy).not.toHaveBeenCalled();
  });
});

describe('authStore.loginWithLinkToken', () => {
  it('token mode: exchanges the link token, persists the session token, sets user', async () => {
    const storage = makeStorage();
    const api = {
      verifyLink: vi.fn().mockResolvedValue({ token: 'tok-link', user: fakeUser }),
      setToken: vi.fn(),
    } as unknown as ApiClient;
    const store = createAuthStore({ api, storage });

    const user = await store.getState().loginWithLinkToken('link-token-abc');

    expect(api.verifyLink).toHaveBeenCalledWith('link-token-abc');
    expect(user).toEqual(fakeUser);
    expect(api.setToken).toHaveBeenCalledWith('tok-link');
    expect(storage.snapshot().goosar_token).toBe('tok-link');
    expect(store.getState().user).toEqual(fakeUser);
  });

  it('throws MFARequiredError with the ticket when a second factor is owed', async () => {
    const storage = makeStorage();
    const api = {
      verifyLink: vi
        .fn()
        .mockResolvedValue({ token: '', mfa_required: true, mfa_token: 'ticket-1' }),
      setToken: vi.fn(),
    } as unknown as ApiClient;
    const store = createAuthStore({ api, storage });

    await expect(store.getState().loginWithLinkToken('link-token-abc')).rejects.toBeInstanceOf(
      MFARequiredError,
    );
    await expect(store.getState().loginWithLinkToken('link-token-abc')).rejects.toMatchObject({
      mfaToken: 'ticket-1',
    });
    expect(api.setToken).not.toHaveBeenCalled();
    expect(storage.snapshot().goosar_token).toBeUndefined();
    expect(store.getState().user).toBeNull();
  });

  it('cookie mode: does not touch token storage, still sets user', async () => {
    const storage = makeStorage();
    const setItemSpy = vi.spyOn(storage, 'setItem');
    const api = {
      verifyLink: vi.fn().mockResolvedValue({ token: '', user: fakeUser }),
      setToken: vi.fn(),
    } as unknown as ApiClient;
    const store = createAuthStore({ api, storage, cookieAuth: true });

    const user = await store.getState().loginWithLinkToken('link-token-abc');

    expect(user).toEqual(fakeUser);
    expect(api.setToken).not.toHaveBeenCalled();
    expect(setItemSpy).not.toHaveBeenCalled();
    expect(store.getState().user).toEqual(fakeUser);
  });

  it('throws on the empty-fallback response (malformed server body) without setting user', async () => {
    const storage = makeStorage();
    const api = {
      verifyLink: vi.fn().mockResolvedValue({ token: '', user: { ...fakeUser, id: '' } }),
      setToken: vi.fn(),
    } as unknown as ApiClient;
    const store = createAuthStore({ api, storage, cookieAuth: true });

    await expect(store.getState().loginWithLinkToken('link-token-abc')).rejects.toThrow();

    expect(store.getState().user).toBeNull();
  });

  it('token mode: throws when the server returns no token and persists nothing', async () => {
    const storage = makeStorage();
    const api = {
      verifyLink: vi.fn().mockResolvedValue({ token: '', user: fakeUser }),
      setToken: vi.fn(),
    } as unknown as ApiClient;
    const store = createAuthStore({ api, storage });

    await expect(store.getState().loginWithLinkToken('link-token-abc')).rejects.toThrow();

    expect(api.setToken).not.toHaveBeenCalled();
    expect(storage.snapshot().goosar_token).toBeUndefined();
    expect(store.getState().user).toBeNull();
  });

  it('propagates an ApiError from a spent or expired link without setting user', async () => {
    const storage = makeStorage();
    const api = {
      verifyLink: vi.fn().mockRejectedValue(new Error('invalid or expired link')),
      setToken: vi.fn(),
    } as unknown as ApiClient;
    const store = createAuthStore({ api, storage });

    await expect(store.getState().loginWithLinkToken('spent')).rejects.toThrow(
      'invalid or expired link',
    );
    expect(store.getState().user).toBeNull();
  });
});
