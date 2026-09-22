'use client';

import { useRouter } from 'next/navigation';
import { useEffect } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useAuthStore } from '@goosar/core/auth';
import { paths } from '@goosar/core/paths';
import { workspaceListOptions } from '@goosar/core/workspace/queries';
import { NewWorkspacePage } from '@goosar/views/workspace/new-workspace-page';

export default function Page() {
  const router = useRouter();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const { data: wsList = [] } = useQuery({
    ...workspaceListOptions(),
    enabled: !!user,
  });

  useEffect(() => {
    if (!isLoading && !user) router.replace(paths.login());
  }, [isLoading, user, router]);

  if (isLoading || !user) return null;

  const onBack = wsList.length > 0 ? () => router.push(paths.root()) : undefined;

  return (
    <NewWorkspacePage
      onSuccess={(ws) => router.push(paths.workspace(ws.slug).issues())}
      onBack={onBack}
      onJoinInstead={() => router.push(paths.joinWorkspace())}
    />
  );
}
