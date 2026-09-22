/**
 * @vitest-environment jsdom
 */
import { afterEach, describe, expect, it, vi } from 'vitest';
import { render, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { setApiInstance } from '../api';
import { ApiError, type ApiClient } from '../api/client';
import { createAuthStore, registerAuthStore } from '../auth';
import type { StorageAdapter, User } from '../types';
import { AuthInitializer } from './auth-initializer';

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

function makeApi(opts: {
  getMe: () => Promise<User>;
  listWorkspaces: () => Promise<unknown[]>;
}): ApiClient & {
  setToken: ReturnType<typeof vi.fn>;
  getMe: ReturnType<typeof vi.fn>;
} {
  return {
    getConfig: vi.fn().mockResolvedValue({}),
    getMe: vi.fn(opts.getMe),
    listWorkspaces: vi.fn(opts.listWorkspaces),
    setToken: vi.fn(),
  } as unknown as ApiClient & {
    setToken: ReturnType<typeof vi.fn>;
    getMe: ReturnType<typeof vi.fn>;
  };
}

function renderInitializer(props: {
  api: ApiClient;
  storage: StorageAdapter;
  cookieAuth?: boolean;
  onLogin?: () => void;
  onLogout?: () => void;
}) {
  setApiInstance(props.api);
  const store = createAuthStore({
    api: props.api,
    storage: props.storage,
    cookieAuth: props.cookieAuth,
  });
  registerAuthStore(store);
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });

  function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  }

  render(
    <Wrapper>
      <AuthInitializer
        onLogin={props.onLogin}
        onLogout={props.onLogout}
        storage={props.storage}
        cookieAuth={props.cookieAuth}
      >
        <div>ready</div>
      </AuthInitializer>
    </Wrapper>,
  );

  return { store };
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe('AuthInitializer — token mode', () => {
  it('keeps the token and does not log out on a non-401 error (network Error)', async () => {
    const storage = makeStorage({ goosar_token: 'tok-123' });
    const api = makeApi({
      getMe: () => Promise.reject(new TypeError('fetch failed')),
      listWorkspaces: () => Promise.reject(new TypeError('fetch failed')),
    });
    const onLogout = vi.fn();

    renderInitializer({ api, storage, onLogout });

    await waitFor(() => expect(api.getMe).toHaveBeenCalled());
    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(api.setToken).not.toHaveBeenCalledWith(null);
    expect(storage.snapshot().goosar_token).toBe('tok-123');
    expect(onLogout).not.toHaveBeenCalled();
  });

  it('keeps the token and does not log out on a non-401 ApiError (503)', async () => {
    const storage = makeStorage({ goosar_token: 'tok-123' });
    const api = makeApi({
      getMe: () => Promise.reject(new ApiError('server error', 503, 'Service Unavailable')),
      listWorkspaces: () =>
        Promise.reject(new ApiError('server error', 503, 'Service Unavailable')),
    });
    const onLogout = vi.fn();

    renderInitializer({ api, storage, onLogout });

    await waitFor(() => expect(api.getMe).toHaveBeenCalled());
    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(api.setToken).not.toHaveBeenCalledWith(null);
    expect(storage.snapshot().goosar_token).toBe('tok-123');
    expect(onLogout).not.toHaveBeenCalled();
  });

  it('clears the token and logs out on a genuine 401 ApiError', async () => {
    const storage = makeStorage({ goosar_token: 'tok-123' });
    const api = makeApi({
      getMe: () => Promise.reject(new ApiError('unauthorized', 401, 'Unauthorized')),
      listWorkspaces: () => Promise.resolve([]),
    });
    const onLogout = vi.fn();

    renderInitializer({ api, storage, onLogout });

    await waitFor(() => expect(onLogout).toHaveBeenCalledTimes(1));
    expect(api.setToken).toHaveBeenCalledWith(null);
    expect(storage.snapshot().goosar_token).toBeUndefined();
  });
});

describe('AuthInitializer — cookie mode', () => {
  it('does not log out on a non-401 error (network Error)', async () => {
    const storage = makeStorage();
    const api = makeApi({
      getMe: () => Promise.reject(new TypeError('fetch failed')),
      listWorkspaces: () => Promise.reject(new TypeError('fetch failed')),
    });
    const onLogout = vi.fn();

    renderInitializer({ api, storage, cookieAuth: true, onLogout });

    await waitFor(() => expect(api.getMe).toHaveBeenCalled());
    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(onLogout).not.toHaveBeenCalled();
  });

  it('logs out on a genuine 401 ApiError', async () => {
    const storage = makeStorage();
    const api = makeApi({
      getMe: () => Promise.reject(new ApiError('unauthorized', 401, 'Unauthorized')),
      listWorkspaces: () => Promise.resolve([]),
    });
    const onLogout = vi.fn();

    renderInitializer({ api, storage, cookieAuth: true, onLogout });

    await waitFor(() => expect(onLogout).toHaveBeenCalledTimes(1));
  });

  it('populates the user on success', async () => {
    const storage = makeStorage();
    const api = makeApi({
      getMe: () => Promise.resolve(fakeUser),
      listWorkspaces: () => Promise.resolve([]),
    });
    const onLogin = vi.fn();

    const { store } = renderInitializer({ api, storage, cookieAuth: true, onLogin });

    await waitFor(() => expect(store.getState().user).toEqual(fakeUser));
    expect(onLogin).toHaveBeenCalledTimes(1);
  });
});
