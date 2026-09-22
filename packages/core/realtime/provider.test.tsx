/**
 * @vitest-environment jsdom
 */
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen, waitFor } from '@testing-library/react';
import type { ReactNode } from 'react';
import type { StoreApi, UseBoundStore } from 'zustand';
import { describe, expect, it, vi, beforeEach } from 'vitest';
import type { AuthState } from '../auth/store';
import { WSProvider, useWS, useWSConnectionState } from './provider';

const { instances } = vi.hoisted(() => ({ instances: [] as any[] }));

vi.mock('../api/ws-client', () => {
  class FakeWSClient {
    connect = vi.fn();
    disconnect = vi.fn();
    setAuth = vi.fn();
    on = vi.fn(() => () => {});
    onAny = vi.fn(() => () => {});
    onReconnect = vi.fn(() => () => {});
    stateChangeCb: ((s: string) => void) | null = null;
    unauthorizedCb: ((reason?: string) => void) | null = null;
    state = 'connecting';
    getState = vi.fn(() => this.state);
    onStateChange = vi.fn((cb: (s: string) => void) => {
      this.stateChangeCb = cb;
      return () => {
        this.stateChangeCb = null;
      };
    });
    onUnauthorized = vi.fn((cb: (reason?: string) => void) => {
      this.unauthorizedCb = cb;
      return () => {
        this.unauthorizedCb = null;
      };
    });
    emitState(next: string) {
      this.state = next;
      this.stateChangeCb?.(next);
    }
    emitUnauthorized(reason?: string) {
      this.unauthorizedCb?.(reason);
    }
    constructor() {
      instances.push(this);
    }
  }
  return { WSClient: FakeWSClient };
});

vi.mock('../platform/workspace-storage', () => ({
  getCurrentSlug: () => 'acme',
  getCurrentWsId: () => 'ws-1',
  subscribeToCurrentSlug: () => () => {},
  createWorkspaceAwareStorage: (adapter: unknown) => adapter,
  registerForWorkspaceRehydration: () => {},
}));

vi.mock('../paths', () => ({
  useHasOnboarded: () => true,
  resolvePostAuthDestination: () => '/',
}));

function createAuthStore(): UseBoundStore<StoreApi<AuthState>> {
  const state = { user: { id: 'u1' } } as unknown as AuthState;
  const store = ((selector: (s: AuthState) => unknown) =>
    selector(state)) as unknown as UseBoundStore<StoreApi<AuthState>>;
  Object.assign(store, {
    getState: () => state,
    subscribe: () => () => {},
    setState: () => {},
    destroy: () => {},
  });
  return store;
}

function createStorage(initialToken: string | null) {
  let token = initialToken;
  return {
    getItem: vi.fn((key: string) => (key === 'goosar_token' ? token : null)),
    setItem: vi.fn((key: string, value: string) => {
      if (key === 'goosar_token') token = value;
    }),
    removeItem: vi.fn(),
    setToken: (t: string | null) => {
      token = t;
    },
  };
}

function ConnectionStateProbe() {
  const { connectionState } = useWS();
  return <div data-testid="state">{connectionState}</div>;
}

function renderProvider(storage: ReturnType<typeof createStorage>) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={qc}>
        <WSProvider
          wsUrl="ws://example.test/ws"
          authStore={createAuthStore()}
          storage={storage as any}
        >
          {children}
        </WSProvider>
      </QueryClientProvider>
    );
  }
  return render(<ConnectionStateProbe />, { wrapper: Wrapper });
}

describe('WSProvider — connectionState (#114)', () => {
  beforeEach(() => {
    instances.length = 0;
  });

  it("exposes the WSClient's connection state reactively", async () => {
    const storage = createStorage('tok');
    renderProvider(storage);

    await waitFor(() => expect(instances.length).toBe(1));
    expect(screen.getByTestId('state').textContent).toBe('connecting');

    instances[0].emitState('degraded');
    await waitFor(() => expect(screen.getByTestId('state').textContent).toBe('degraded'));

    instances[0].emitState('connected');
    await waitFor(() => expect(screen.getByTestId('state').textContent).toBe('connected'));
  });
});

describe('useWSConnectionState outside a WSProvider (#257)', () => {
  function StateProbe() {
    return <span data-testid="state">{useWSConnectionState()}</span>;
  }

  it('reports "connecting" instead of throwing', () => {
    render(<StateProbe />);
    expect(screen.getByTestId('state').textContent).toBe('connecting');
  });

  it('still throws from useWS, which needs a real client', () => {
    function WSProbe() {
      useWS();
      return null;
    }
    expect(() => render(<WSProbe />)).toThrow('useWS must be used within WSProvider');
  });
});

describe('WSProvider — auth rejection retry-once (#107)', () => {
  beforeEach(() => {
    instances.length = 0;
  });

  it('retries once with a refreshed token from storage on auth rejection', async () => {
    const storage = createStorage('stale-token');
    renderProvider(storage);
    await waitFor(() => expect(instances.length).toBe(1));
    const ws = instances[0];
    expect(ws.connect).toHaveBeenCalledTimes(1);

    storage.setToken('fresh-token');
    ws.emitUnauthorized('invalid token');

    await waitFor(() => expect(ws.setAuth).toHaveBeenCalledWith('fresh-token', 'acme'));
    expect(ws.connect).toHaveBeenCalledTimes(2);

    ws.emitUnauthorized('invalid token');
    await Promise.resolve();
    expect(ws.connect).toHaveBeenCalledTimes(2);
  });

  it('does not retry when storage has no token to refresh to', async () => {
    const storage = createStorage('stale-token');
    renderProvider(storage);
    await waitFor(() => expect(instances.length).toBe(1));
    const ws = instances[0];

    storage.setToken(null);
    ws.emitUnauthorized('invalid token');
    await Promise.resolve();

    expect(ws.setAuth).toHaveBeenCalledTimes(1);
    expect(ws.connect).toHaveBeenCalledTimes(1);
  });
});
