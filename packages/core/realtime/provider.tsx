'use client';

import {
  createContext,
  use,
  useEffect,
  useState,
  useCallback,
  useRef,
  useSyncExternalStore,
  type ReactNode,
} from 'react';
import { WSClient, type WSConnectionState } from '../api/ws-client';
import type { WSEventType, StorageAdapter } from '../types';
import type { ClientIdentity } from '../platform/types';
import type { StoreApi, UseBoundStore } from 'zustand';
import type { AuthState } from '../auth/store';
import { getCurrentSlug, subscribeToCurrentSlug } from '../platform/workspace-storage';
import { createLogger } from '../logger';
import { useRealtimeSync, type RealtimeSyncStores } from './use-realtime-sync';

type EventHandler = (payload: unknown, actorId?: string, actorType?: string) => void;

interface WSContextValue {
  subscribe: (event: WSEventType, handler: EventHandler) => () => void;
  onReconnect: (callback: () => void) => () => void;
  connectionState: WSConnectionState;
}

const WSContext = createContext<WSContextValue | null>(null);

export interface WSProviderProps {
  children: ReactNode;
  wsUrl: string;
  authStore: UseBoundStore<StoreApi<AuthState>>;
  storage: StorageAdapter;
  cookieAuth?: boolean;
  identity?: ClientIdentity;
  onToast?: (message: string, type?: 'info' | 'error') => void;
}

export function WSProvider({
  children,
  wsUrl,
  authStore,
  storage,
  cookieAuth,
  identity,
  onToast,
}: WSProviderProps) {
  const user = authStore((s) => s.user);
  const wsSlug = useSyncExternalStore(subscribeToCurrentSlug, getCurrentSlug, () => null);
  const [wsClient, setWsClient] = useState<WSClient | null>(null);
  const [connectionState, setConnectionState] = useState<WSConnectionState>('connecting');
  const unauthorizedRetriedRef = useRef(false);

  const identityPlatform = identity?.platform;
  const identityVersion = identity?.version;
  const identityOS = identity?.os;

  useEffect(() => {
    if (!user || !wsSlug) return;

    const token = cookieAuth ? null : storage.getItem('goosar_token');
    if (!cookieAuth && !token) return;

    const ws = new WSClient(wsUrl, {
      logger: createLogger('ws'),
      cookieAuth,
      identity:
        identityPlatform || identityVersion || identityOS
          ? {
              platform: identityPlatform,
              version: identityVersion,
              os: identityOS,
            }
          : undefined,
    });
    ws.setAuth(token, wsSlug);
    setWsClient(ws);
    setConnectionState(ws.getState());
    unauthorizedRetriedRef.current = false;
    ws.connect();

    return () => {
      ws.disconnect();
      setWsClient(null);
    };
  }, [user, wsSlug, wsUrl, storage, cookieAuth, identityPlatform, identityVersion, identityOS]);

  useEffect(() => {
    if (!wsClient) {
      setConnectionState('connecting');
      return;
    }
    setConnectionState(wsClient.getState());
    return wsClient.onStateChange(setConnectionState);
  }, [wsClient]);

  useEffect(() => {
    if (!wsClient || cookieAuth) return;
    return wsClient.onUnauthorized(() => {
      if (unauthorizedRetriedRef.current) return;
      unauthorizedRetriedRef.current = true;
      const freshToken = storage.getItem('goosar_token');
      if (freshToken && wsSlug) {
        wsClient.setAuth(freshToken, wsSlug);
        wsClient.connect();
      }
    });
  }, [wsClient, cookieAuth, storage, wsSlug]);

  const stores: RealtimeSyncStores = { authStore };

  useRealtimeSync(wsClient, stores, onToast);

  const subscribe = useCallback(
    (event: WSEventType, handler: EventHandler) => {
      if (!wsClient) return () => {};
      return wsClient.on(event, handler);
    },
    [wsClient],
  );

  const onReconnectCb = useCallback(
    (callback: () => void) => {
      if (!wsClient) return () => {};
      return wsClient.onReconnect(callback);
    },
    [wsClient],
  );

  return (
    <WSContext.Provider value={{ subscribe, onReconnect: onReconnectCb, connectionState }}>
      {children}
    </WSContext.Provider>
  );
}

export function useWS() {
  const ctx = use(WSContext);
  if (!ctx) throw new Error('useWS must be used within WSProvider');
  return ctx;
}

export function useWSConnectionState(): WSConnectionState {
  return use(WSContext)?.connectionState ?? 'connecting';
}
