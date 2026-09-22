'use client';

import { Suspense, useEffect, useRef } from 'react';
import { useRouter, useSearchParams } from 'next/navigation';
import { useQuery } from '@tanstack/react-query';
import { useAuthStore } from '@goosar/core/auth';
import {
  ONBOARDING_REPLAY_PARAM,
  ONBOARDING_REPLAY_VALUE,
  paths,
  resolvePostAuthDestination,
  useHasOnboarded,
} from '@goosar/core/paths';
import { workspaceListOptions } from '@goosar/core/workspace/queries';
import { CliInstallInstructions, OnboardingFlow } from '@goosar/views/onboarding';

function OnboardingPageContent() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const isReplay = searchParams.get(ONBOARDING_REPLAY_PARAM) === ONBOARDING_REPLAY_VALUE;
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const hasOnboarded = useHasOnboarded();
  const { data: workspaces = [], isFetched: workspacesFetched } = useQuery({
    ...workspaceListOptions(),
    enabled: !!user,
  });
  const completingRef = useRef(false);
  const admittedRef = useRef(false);

  useEffect(() => {
    if (isLoading || !user) {
      if (!isLoading && !user) router.replace(paths.login());
      return;
    }
    if (completingRef.current || admittedRef.current) return;
    if (!hasOnboarded || isReplay) {
      admittedRef.current = true;
      return;
    }
    if (!workspacesFetched) return;
    router.replace(resolvePostAuthDestination(workspaces, hasOnboarded));
  }, [isLoading, user, hasOnboarded, isReplay, workspacesFetched, workspaces, router]);

  if (isLoading || !user) return null;
  if (hasOnboarded && !isReplay && !admittedRef.current) return null;

  return (
    <div className="h-full overflow-y-auto bg-background">
      <OnboardingFlow
        onComplete={(ws, issueId) => {
          completingRef.current = true;
          if (ws && issueId) {
            router.push(paths.workspace(ws.slug).issueDetail(issueId));
          } else if (ws) {
            router.push(paths.workspace(ws.slug).capabilities());
          } else {
            router.push(paths.root());
          }
        }}
        runtimeInstructions={<CliInstallInstructions />}
      />
    </div>
  );
}

export default function OnboardingPage() {
  return (
    <Suspense fallback={null}>
      <OnboardingPageContent />
    </Suspense>
  );
}
