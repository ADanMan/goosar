'use client';

import { useEffect, useMemo } from 'react';
import { ApiClient } from '../api/client';
import { installFreezeWatchdog } from '../diagnostics/freeze-watchdog';
import { setApiInstance, setSchemaLogger } from '../api';
import { createAuthStore, registerAuthStore } from '../auth';
import { createChatStore, registerChatStore } from '../chat';
import { I18nProvider, LocaleAdapterProvider, UserLocaleSync } from '../i18n/react';
import { WSProvider } from '../realtime';
import { QueryProvider } from '../provider';
import { createLogger } from '../logger';
import { defaultStorage } from './storage';
import { AuthInitializer } from './auth-initializer';
import type { CoreProviderProps, ClientIdentity } from './types';
import type { StorageAdapter } from '../types/storage';
import { ClientUsageReporter } from '../client-usage';
import { configureShortcutPlatform, configureShortcutRuntime } from '../shortcuts/platform';

let initialized = false;
let authStore: ReturnType<typeof createAuthStore>;
let chatStore: ReturnType<typeof createChatStore>;
function initCore(
  apiBaseUrl: string,
  storage: StorageAdapter,
  onLogin?: () => void,
  onLogout?: () => void,
  cookieAuth?: boolean,
  identity?: ClientIdentity,
) {
  if (initialized) return;

  configureShortcutPlatform(
    identity?.os === 'macos' ||
      identity?.os === 'windows' ||
      identity?.os === 'linux' ||
      identity?.os === 'unknown'
      ? identity.os
      : null,
  );
  configureShortcutRuntime(identity?.platform === 'desktop' ? 'desktop' : null);

  const api = new ApiClient(apiBaseUrl, {
    logger: createLogger('api'),
    onUnauthorized: () => {
      storage.removeItem('goosar_token');
    },
    identity,
  });
  setApiInstance(api);
  setSchemaLogger(createLogger('api-schema'));

  if (!cookieAuth) {
    const token = storage.getItem('goosar_token');
    if (token) api.setToken(token);
  }

  authStore = createAuthStore({ api, storage, onLogin, onLogout, cookieAuth });
  registerAuthStore(authStore);

  chatStore = createChatStore({ storage });
  registerChatStore(chatStore);

  initialized = true;
}

export function CoreProvider({
  children,
  apiBaseUrl = '',
  wsUrl = 'ws://localhost:8080/ws',
  storage = defaultStorage,
  cookieAuth,
  onLogin,
  onLogout,
  identity,
  locale,
  resources,
  localeAdapter,
}: CoreProviderProps) {
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useMemo(() => initCore(apiBaseUrl, storage, onLogin, onLogout, cookieAuth, identity), []);

  useEffect(() => {
    installFreezeWatchdog();
  }, []);

  const tree = (
    <QueryProvider>
      <AuthInitializer
        onLogin={onLogin}
        onLogout={onLogout}
        storage={storage}
        cookieAuth={cookieAuth}
        identity={identity}
      >
        {/* Desktop's reporter owns both activity and runtime state so it must
            be the only writer for that installation. */}
        {identity?.platform !== 'desktop' && (
          <ClientUsageReporter storage={storage} identity={identity} />
        )}
        <WSProvider
          wsUrl={wsUrl}
          authStore={authStore}
          storage={storage}
          cookieAuth={cookieAuth}
          identity={identity}
        >
          {children}
        </WSProvider>
      </AuthInitializer>
    </QueryProvider>
  );

  const withAdapter = localeAdapter ? (
    <LocaleAdapterProvider adapter={localeAdapter}>
      <UserLocaleSync />
      {tree}
    </LocaleAdapterProvider>
  ) : (
    tree
  );

  return (
    <I18nProvider locale={locale} resources={resources}>
      {withAdapter}
    </I18nProvider>
  );
}
