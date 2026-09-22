'use client';

import { useEffect } from 'react';
import { useRouter } from 'next/navigation';
import { useQuery } from '@tanstack/react-query';
import { useAuthStore } from '@goosar/core/auth';
import { workspaceListOptions } from '@goosar/core/workspace';
import { resolvePostAuthDestination, useHasOnboarded } from '@goosar/core/paths';
import { isOfficialMarketingHost } from '@/lib/public-host';

export function RedirectIfAuthenticated() {
  const router = useRouter();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const hasOnboarded = useHasOnboarded();

  const { data: list = [], isFetched } = useQuery({
    ...workspaceListOptions(),
    enabled: !!user,
  });

  useEffect(() => {
    if (isLoading || !user || !isFetched) return;
    if (isOfficialMarketingHost(window.location.hostname)) return;
    router.replace(resolvePostAuthDestination(list, hasOnboarded));
  }, [isLoading, user, isFetched, list, hasOnboarded, router]);

  return null;
}
