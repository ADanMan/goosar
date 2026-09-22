import { useEffect, useRef } from 'react';
import { bucketDiagnosticPath, setDiagnosticRoute } from '@goosar/core/diagnostics';
import { useAuthStore } from '@goosar/core/auth';
import { useActiveTabIdentity, useActiveTabUrl } from '@/stores/tab-store';
import { useWindowOverlayStore, type WindowOverlay } from '@/stores/window-overlay-store';
import type { RendererRouteContextInput } from '../../../shared/renderer-route-context';

export function DiagnosticRouteReporter() {
  const user = useAuthStore((s) => s.user);
  const overlay = useWindowOverlayStore((s) => s.overlay);
  const { slug: activeWorkspaceSlug } = useActiveTabIdentity();
  const activeTabUrl = useActiveTabUrl();

  const lastSentRef = useRef<string | null>(null);

  useEffect(() => {
    const surface = resolveSurface({
      user,
      overlay,
      activeWorkspaceSlug,
      activeTabUrl,
    });
    setDiagnosticRoute(surface.path);

    const context: RendererRouteContextInput = {
      surface: surface.kind,
      path: surface.path,
    };
    send(context, lastSentRef);
  }, [user, overlay, activeWorkspaceSlug, activeTabUrl]);

  return null;
}

function send(context: RendererRouteContextInput, lastSentRef: { current: string | null }) {
  const serialized = JSON.stringify(context);
  if (serialized === lastSentRef.current) return;
  lastSentRef.current = serialized;
  window.desktopAPI.setRendererRouteContext(context);
}

function resolveSurface({
  user,
  overlay,
  activeWorkspaceSlug,
  activeTabUrl,
}: {
  user: unknown;
  overlay: WindowOverlay | null;
  activeWorkspaceSlug: string | null;
  activeTabUrl: string | null;
}): { kind: RendererRouteContextInput['surface']; path: string } {
  if (!user) return { kind: 'login', path: '/login' };
  if (overlay) {
    return { kind: 'overlay', path: bucketDiagnosticPath(overlayPath(overlay)) };
  }
  if (activeWorkspaceSlug && activeTabUrl) {
    return { kind: 'tab', path: bucketDiagnosticPath(activeTabUrl) };
  }
  return { kind: 'tab', path: '/' };
}

function overlayPath(overlay: WindowOverlay): string {
  switch (overlay.type) {
    case 'new-workspace':
      return '/workspaces/new';
    case 'join-workspace':
      return '/workspaces/join';
    case 'onboarding':
      return '/onboarding';
    case 'invite':
      return `/invite/${overlay.invitationId}`;
    case 'invitations':
      return '/invitations';
  }
}
