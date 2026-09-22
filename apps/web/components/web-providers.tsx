'use client';

import { useMemo } from 'react';
import { CoreProvider } from '@goosar/core/platform';
import { createBrowserCookieLocaleAdapter } from '@goosar/core/i18n/browser';
import type { LocaleResources, SupportedLocale } from '@goosar/core/i18n';
import { useWelcomeStore } from '@goosar/core/onboarding';
import packageJson from '../package.json';
import { WebNavigationProvider } from '@/platform/navigation';
import { setLoggedInCookie, clearLoggedInCookie } from '@/features/auth/auth-cookie';
import { detectWebOS } from '@/platform/client-os';

function hasLegacyToken(): boolean {
  if (typeof window === 'undefined') return false;
  try {
    return Boolean(window.localStorage.getItem('goosar_token'));
  } catch {
    return false;
  }
}

function deriveWsUrl(): string | undefined {
  if (typeof window === 'undefined') return undefined;
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  return `${proto}//${window.location.host}/ws`;
}

const WEB_VERSION = process.env.NEXT_PUBLIC_APP_VERSION || packageJson.version || 'dev';

export function WebProviders({
  children,
  locale,
  resources,
  apiBaseUrl,
  wsUrl,
}: {
  children: React.ReactNode;
  locale: SupportedLocale;
  resources: Record<string, LocaleResources>;
  apiBaseUrl?: string;
  wsUrl?: string;
}) {
  const cookieAuth = !hasLegacyToken();
  const identity = useMemo(
    () => ({ platform: 'web', version: WEB_VERSION, os: detectWebOS() }),
    [],
  );
  const localeAdapter = useMemo(() => createBrowserCookieLocaleAdapter(), []);
  return (
    <CoreProvider
      apiBaseUrl={apiBaseUrl}
      wsUrl={wsUrl || deriveWsUrl()}
      cookieAuth={cookieAuth}
      onLogin={setLoggedInCookie}
      onLogout={() => {
        useWelcomeStore.getState().reset();
        clearLoggedInCookie();
      }}
      identity={identity}
      locale={locale}
      resources={resources}
      localeAdapter={localeAdapter}
    >
      <WebNavigationProvider>{children}</WebNavigationProvider>
    </CoreProvider>
  );
}
