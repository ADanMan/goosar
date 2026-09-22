import { useEffect, useMemo } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { bucketDiagnosticPath, setDiagnosticRoute } from '@goosar/core/diagnostics';
import { NavigationProvider, type NavigationAdapter } from '@goosar/views/navigation';
import { parseIssueWindowPath } from '../../../shared/issue-window';

function useContentLinkHandler(
  navigate: ReturnType<typeof useNavigate>,
  runtimeConfig: typeof window.desktopAPI.runtimeConfig,
) {
  useEffect(() => {
    const handler = (e: Event) => {
      const path = (e as CustomEvent<{ path?: string }>).detail?.path;
      if (!path) return;
      const issuePath = parseIssueWindowPath(path);
      if (issuePath) {
        void navigate(issuePath.path);
        return;
      }
      if (!runtimeConfig.ok) return;
      void window.desktopAPI.openExternal(`${runtimeConfig.config.appUrl}${path}`);
    };
    window.addEventListener('goosar:navigate', handler);
    return () => window.removeEventListener('goosar:navigate', handler);
  }, [navigate, runtimeConfig]);
}

export function IssueWindowNavigationProvider({ children }: { children: React.ReactNode }) {
  const location = useLocation();
  const navigate = useNavigate();
  const runtimeConfig = window.desktopAPI.runtimeConfig;
  const currentPath = `${location.pathname}${location.search}${location.hash}`;

  useEffect(() => {
    const bucketed = bucketDiagnosticPath(currentPath);
    setDiagnosticRoute(bucketed);
    window.desktopAPI.setRendererRouteContext({
      surface: 'tab',
      path: bucketed,
    });
  }, [currentPath]);

  useContentLinkHandler(navigate, runtimeConfig);

  const adapter = useMemo<NavigationAdapter>(() => {
    const navigateToIssue = (path: string, replace = false) => {
      const issuePath = parseIssueWindowPath(path);
      if (!issuePath) return;
      void navigate(issuePath.path, { replace });
    };

    return {
      push: (path) => navigateToIssue(path),
      replace: (path) => navigateToIssue(path, true),
      back: () => void navigate(-1),
      pathname: location.pathname,
      searchParams: new URLSearchParams(location.search),
      openInNewTab: (path, title) => {
        void window.desktopAPI.openIssueWindow({
          path,
          title: title ?? 'Issue',
        });
      },
      getShareableUrl: (path) =>
        runtimeConfig.ok ? `${runtimeConfig.config.appUrl}${path}` : path,
    };
  }, [location.pathname, location.search, navigate, runtimeConfig]);

  return <NavigationProvider value={adapter}>{children}</NavigationProvider>;
}
