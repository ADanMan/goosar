'use client';

import { use, useEffect } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useRouter } from 'next/navigation';
import { WorkspaceSlugProvider, paths } from '@goosar/core/paths';
import { workspaceBySlugOptions } from '@goosar/core/workspace';
import { setCurrentWorkspace } from '@goosar/core/platform';
import { useAuthStore } from '@goosar/core/auth';
import { NoAccessPage } from '@goosar/views/workspace/no-access-page';
import { WelcomeAfterOnboarding } from '@goosar/views/workspace/welcome-after-onboarding';
import { GoosarIcon } from '@goosar/ui/components/common/goosar-icon';
import { useWorkspaceSeen } from '@goosar/views/workspace/use-workspace-seen';

export default function WorkspaceLayout({
  children,
  params,
}: {
  children: React.ReactNode;
  params: Promise<{ workspaceSlug: string }>;
}) {
  const { workspaceSlug } = use(params);
  const user = useAuthStore((s) => s.user);
  const isAuthLoading = useAuthStore((s) => s.isLoading);
  const router = useRouter();

  useEffect(() => {
    if (!isAuthLoading && !user) router.replace(paths.login());
  }, [isAuthLoading, user, router]);

  useEffect(() => {
    if (user && user.onboarded_at == null) {
      router.replace(paths.onboarding());
    }
  }, [user, router]);

  const { data: workspace, isFetched: listFetched } = useQuery({
    ...workspaceBySlugOptions(workspaceSlug),
    enabled: !!user,
  });

  if (workspace) {
    setCurrentWorkspace(workspaceSlug, workspace.id);
  }

  useEffect(() => {
    if (!workspace || typeof document === 'undefined') return;
    const oneYear = 60 * 60 * 24 * 365;
    const secure = location.protocol === 'https:' ? '; Secure' : '';
    document.cookie = `last_workspace_slug=${encodeURIComponent(workspaceSlug)}; path=/; max-age=${oneYear}; SameSite=Lax${secure}`;
  }, [workspace, workspaceSlug]);

  const hasBeenSeen = useWorkspaceSeen(workspaceSlug, !!workspace);

  const loadingIndicator = (
    <div className="flex h-svh items-center justify-center">
      <GoosarIcon className="size-10 animate-pulse" />
    </div>
  );

  if (isAuthLoading) return loadingIndicator;
  if (!listFetched) return loadingIndicator;
  if (!workspace) {
    if (hasBeenSeen) return null;
    return <NoAccessPage />;
  }

  return (
    <WorkspaceSlugProvider slug={workspaceSlug}>
      {children}
      {/* Reads the welcome-store transient signal parked by
       *  OnboardingFlow.handleRuntimeNext. Runtime path → loading veil →
       *  blocking Modal with Helper + starter cards. Skip path → Modal
       *  with two seeded issues. No signal → null. */}
      <WelcomeAfterOnboarding />
    </WorkspaceSlugProvider>
  );
}
