'use client';

import { useRouter } from 'next/navigation';
import { useEffect } from 'react';
import { useAuthStore } from '@goosar/core/auth';
import { paths } from '@goosar/core/paths';
import { JoinWorkspacePage } from '@goosar/views/workspace/join-workspace-page';

export default function Page() {
  const router = useRouter();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);

  useEffect(() => {
    if (!isLoading && !user) {
      router.replace(`${paths.login()}?next=${encodeURIComponent(paths.joinWorkspace())}`);
    }
  }, [isLoading, user, router]);

  if (isLoading || !user) return null;

  return <JoinWorkspacePage onCreateInstead={() => router.push(paths.newWorkspace())} />;
}
