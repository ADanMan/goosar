import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { CoreProvider } from '@goosar/core/platform';
import { pickLocale, type SupportedLocale } from '@goosar/core/i18n';
import { MFARequiredError, useAuthStore } from '@goosar/core/auth';
import { hasPendingOnboardingCompletion, useWelcomeStore } from '@goosar/core/onboarding';
import { workspaceKeys, workspaceListOptions } from '@goosar/core/workspace/queries';
import { api } from '@goosar/core/api';
import { useHasOnboarded } from '@goosar/core/paths';
import { setCurrentWorkspace, registerWorkspaceAccessRevokedHandler } from '@goosar/core/platform';
import { ThemeProvider } from '@goosar/ui/components/common/theme-provider';
import { GoosarIcon } from '@goosar/ui/components/common/goosar-icon';
import { Toaster } from '@goosar/ui/components/ui/sonner';
import { DesktopLoginPage } from './pages/login';
import { DesktopShell } from './components/desktop-layout';
import { UpdateNotification } from './components/update-notification';
import { IssueWindow } from './components/issue-window';
import { resolvePreWorkspaceGate } from './pre-workspace-gate';
import { useTabStore } from './stores/tab-store';
import { useWindowOverlayStore } from './stores/window-overlay-store';
import { useDaemonIPCBridge } from './platform/daemon-ipc-bridge';
import { createDesktopLocaleAdapter } from './platform/i18n-adapter';
import { captureEvent } from '@goosar/core/analytics';
import { RESOURCES } from '@goosar/views/locales';
import { DesktopClientUsageReporter } from './platform/client-usage-reporter';
import { DiagnosticRouteReporter } from './platform/diagnostic-route-reporter';
import { flushFreezeBreadcrumb } from './freeze-flush';
import { DiagnosticsControlReporter } from './platform/diagnostics-control-reporter';
import { UpdateControlReporter } from './platform/update-control-reporter';
import {
  setProvisioningWorkspace,
  notifyProvisioningAccessRevoked,
} from './platform/provisioning-bridge';

const HTML_LANG: Record<SupportedLocale, string> = {
  en: 'en',
  'zh-Hans': 'zh-CN',
  ko: 'ko-KR',
  ja: 'ja-JP',
  ru: 'ru-RU',
};

function useCmdWCloseTab() {
  useEffect(() => {
    return window.desktopAPI.onCloseActiveTab(() => {
      if (window.desktopAPI.windowContext?.kind === 'issue') {
        window.desktopAPI.closeWindow();
        return;
      }
      const store = useTabStore.getState();
      const { activeWorkspaceSlug, byWorkspace } = store;
      if (!activeWorkspaceSlug) {
        window.desktopAPI.closeWindow();
        return;
      }
      const group = byWorkspace[activeWorkspaceSlug];
      if (!group || group.tabs.length <= 1) {
        window.desktopAPI.closeWindow();
        return;
      }
      store.closeActiveTab();
    });
  }, []);
}

function IssueWindowContent() {
  const user = useAuthStore((state) => state.user);
  const isLoading = useAuthStore((state) => state.isLoading);
  const context = window.desktopAPI.windowContext ?? { kind: 'main' as const };

  if (context.kind !== 'issue') return null;
  if (isLoading) {
    return (
      <div className="flex h-screen items-center justify-center">
        <GoosarIcon className="size-6 animate-pulse" />
      </div>
    );
  }

  return user ? <IssueWindow context={context} /> : <DesktopLoginPage />;
}

function DesktopAuthSessionBridge() {
  const userId = useAuthStore((state) => state.user?.id ?? null);
  const isLoading = useAuthStore((state) => state.isLoading);

  useEffect(() => {
    if (isLoading) return;
    window.desktopAPI.reportAuthSession?.(userId);
  }, [isLoading, userId]);

  return null;
}

function AppContent() {
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const qc = useQueryClient();

  useEffect(() => {
    registerWorkspaceAccessRevokedHandler(notifyProvisioningAccessRevoked);
    return () => registerWorkspaceAccessRevokedHandler(null);
  }, []);

  const [bootstrapping, setBootstrapping] = useState(false);

  const runtimeConfig = window.desktopAPI.runtimeConfig.ok
    ? window.desktopAPI.runtimeConfig.config
    : null;

  useEffect(() => {
    if (!runtimeConfig) return;
    window.daemonAPI.setTargetApiUrl(runtimeConfig.apiUrl);
  }, [runtimeConfig]);

  useEffect(() => {
    return window.desktopAPI.onInviteOpen((invitationId) => {
      useWindowOverlayStore.getState().open({ type: 'invite', invitationId });
    });
  }, []);

  useEffect(() => {
    const bootstrapLogin = async (login: () => Promise<unknown>) => {
      if (useAuthStore.getState().user) return;
      setBootstrapping(true);
      try {
        await login();
        const wsList = await api.listWorkspaces();
        qc.setQueryData(workspaceKeys.list(), wsList);
      } catch (err) {
        if (err instanceof MFARequiredError && err.mfaToken) {
          useAuthStore.getState().setPendingMfaToken(err.mfaToken);
        }
        // Otherwise: token invalid, or the emailed link was already used or
        // expired — user stays on the login page and falls back to the code.
      } finally {
        setBootstrapping(false);
      }
    };

    const unsubToken = window.desktopAPI.onAuthToken((token) =>
      bootstrapLogin(() => useAuthStore.getState().loginWithToken(token)),
    );
    const unsubLink = window.desktopAPI.onAuthLinkToken((linkToken) =>
      bootstrapLogin(() => useAuthStore.getState().loginWithLinkToken(linkToken)),
    );
    return () => {
      unsubToken();
      unsubLink();
    };
  }, [qc]);

  useEffect(() => {
    if (!user) return;
    const token = localStorage.getItem('goosar_token');
    if (!token) return;
    const userId = user.id;
    (async () => {
      try {
        await window.daemonAPI.syncToken(token, userId);
        await window.daemonAPI.autoStart();
      } catch (err) {
        console.error('Failed to sync daemon on login', err);
      }
    })();
  }, [user]);

  const { data: workspaces = [], isFetched: workspaceListFetched } = useQuery({
    ...workspaceListOptions(),
    enabled: !!user,
  });
  const wsCount = workspaces.length;
  const hasOnboarded = useHasOnboarded();

  const activeWorkspaceSlug = useTabStore((s) => s.activeWorkspaceSlug);
  const activeWsId = activeWorkspaceSlug
    ? workspaces.find((w) => w.slug === activeWorkspaceSlug)?.id
    : undefined;
  useDaemonIPCBridge(activeWsId);

  useEffect(() => {
    if (!user || !workspaceListFetched) return undefined;
    const { overlay, open } = useWindowOverlayStore.getState();
    if (overlay) return undefined;
    const gate = resolvePreWorkspaceGate({
      hasOnboarded,
      workspaceCount: wsCount,
      hasPendingCompletion: hasPendingOnboardingCompletion(user.id),
    });
    if (gate === 'dashboard') return undefined;
    if (gate === 'new-workspace') {
      open({ type: 'new-workspace' });
      return undefined;
    }
    setCurrentWorkspace(null, null);
    setProvisioningWorkspace(null);
    if (gate === 'onboarding') {
      open({ type: 'onboarding' });
      return undefined;
    }
    let cancelled = false;
    void api
      .listMyInvitations()
      .then((invites) => {
        if (cancelled) return;
        const { overlay: latestOverlay, open: latestOpen } = useWindowOverlayStore.getState();
        if (latestOverlay) return;
        if (invites.length > 0) {
          qc.setQueryData(workspaceKeys.myInvitations(), invites);
          latestOpen({ type: 'invitations' });
        } else {
          latestOpen({ type: 'onboarding' });
        }
      })
      .catch(() => {
        if (cancelled) return;
        const { overlay: latestOverlay, open: latestOpen } = useWindowOverlayStore.getState();
        if (latestOverlay) return;
        latestOpen({ type: 'onboarding' });
      });
    return () => {
      cancelled = true;
    };
  }, [user, workspaceListFetched, wsCount, workspaces, hasOnboarded, qc]);

  useLayoutEffect(() => {
    if (!workspaceListFetched) return;
    const validSlugs = new Set(workspaces.map((w) => w.slug));
    useTabStore.getState().validateWorkspaceSlugs(validSlugs);
    const { activeWorkspaceSlug, switchWorkspace } = useTabStore.getState();
    if (!activeWorkspaceSlug && workspaces.length > 0) {
      switchWorkspace(workspaces[0].slug);
    }
  }, [workspaces, workspaceListFetched]);

  const sessionStartedEmptyRef = useRef<boolean | null>(null);
  useEffect(() => {
    if (!user) {
      sessionStartedEmptyRef.current = null;
      return;
    }
    if (!workspaceListFetched) return;
    if (sessionStartedEmptyRef.current === null) {
      sessionStartedEmptyRef.current = wsCount === 0;
      return;
    }
    if (sessionStartedEmptyRef.current && wsCount >= 1) {
      void window.daemonAPI.restart();
      sessionStartedEmptyRef.current = false;
    }
  }, [user, workspaceListFetched, wsCount]);

  if (isLoading || bootstrapping) {
    return (
      <div className="flex h-screen items-center justify-center">
        <GoosarIcon className="size-6 animate-pulse" />
      </div>
    );
  }

  return user ? <DesktopShell /> : <DesktopLoginPage />;
}

function BlockingRuntimeConfigError({
  message,
  locale,
}: {
  message: string;
  locale: SupportedLocale;
}) {
  const strings = (
    RESOURCES[locale].layout as { config_error: { title: string; description: string } }
  ).config_error;
  return (
    <div className="flex h-screen items-center justify-center bg-background p-8 text-foreground">
      <div className="max-w-xl rounded-lg border bg-card p-6 shadow-sm">
        <h1 className="text-lg font-semibold">{strings.title}</h1>
        <p className="mt-3 text-sm text-muted-foreground">{strings.description}</p>
        <pre className="mt-4 whitespace-pre-wrap rounded-md bg-muted p-3 text-xs text-muted-foreground">
          {message}
        </pre>
      </div>
    </div>
  );
}

async function handleDaemonLogout() {
  window.desktopAPI.reportAuthSession?.(null);
  useTabStore.getState().reset();
  useWindowOverlayStore.getState().close();
  useWelcomeStore.getState().reset();
  setProvisioningWorkspace(null);
  try {
    await window.daemonAPI.clearToken();
  } catch {
    // Best-effort — clearing is followed by stop which also hardens state.
  }
  try {
    await window.daemonAPI.stop();
  } catch {
    // Daemon may already be stopped.
  }
}

export default function App() {
  const { version, os } = window.desktopAPI.appInfo;
  const runtimeConfigResult = window.desktopAPI.runtimeConfig;
  const windowContext = window.desktopAPI.windowContext ?? { kind: 'main' as const };
  useCmdWCloseTab();

  useEffect(
    () =>
      flushFreezeBreadcrumb({
        getLastFreeze: () => window.desktopAPI.getLastFreeze(),
        ackFreeze: (ts) => window.desktopAPI.ackFreeze(ts),
        capture: captureEvent,
      }),
    [],
  );

  const identity = useMemo(() => ({ platform: 'desktop', version, os }), [version, os]);
  const localeAdapter = useMemo(() => createDesktopLocaleAdapter(), []);
  const locale = useMemo(() => pickLocale(localeAdapter), [localeAdapter]);
  const resources = useMemo(() => ({ [locale]: RESOURCES[locale] }), [locale]);

  useLayoutEffect(() => {
    document.documentElement.lang = HTML_LANG[locale];
  }, [locale]);

  useEffect(() => {
    window.desktopAPI.setUiLocale?.(locale);
  }, [locale]);

  return (
    <ThemeProvider>
      {runtimeConfigResult.ok ? (
        <CoreProvider
          apiBaseUrl={runtimeConfigResult.config.apiUrl}
          wsUrl={runtimeConfigResult.config.wsUrl}
          onLogout={windowContext.kind === 'main' ? handleDaemonLogout : undefined}
          identity={identity}
          locale={locale}
          resources={resources}
          localeAdapter={localeAdapter}
        >
          <DesktopAuthSessionBridge />
          {windowContext.kind === 'main' && <DiagnosticRouteReporter />}
          <DiagnosticsControlReporter />
          <UpdateControlReporter />
          {windowContext.kind === 'main' && (
            <DesktopClientUsageReporter apiUrl={runtimeConfigResult.config.apiUrl} />
          )}
          {windowContext.kind === 'issue' ? <IssueWindowContent /> : <AppContent />}
        </CoreProvider>
      ) : (
        <BlockingRuntimeConfigError message={runtimeConfigResult.error.message} locale={locale} />
      )}
      <Toaster />
      {windowContext.kind === 'main' && <UpdateNotification />}
    </ThemeProvider>
  );
}
