'use client';

import { useEffect } from 'react';
import { useRouter, useParams } from 'next/navigation';
import { useQuery } from '@tanstack/react-query';
import { useAuthStore } from '@goosar/core/auth';
import { paths } from '@goosar/core/paths';
import { workspaceListOptions } from '@goosar/core/workspace/queries';
import { InvitePage } from '@goosar/views/invite';

export default function InviteAcceptPage() {
  const router = useRouter();
  const params = useParams<{ id: string }>();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);
  const { data: wsList = [] } = useQuery({
    ...workspaceListOptions(),
    enabled: !!user,
  });

  useEffect(() => {
    if (!isLoading && !user) {
      router.replace(`${paths.login()}?next=${encodeURIComponent(paths.invite(params.id))}`);
    }
  }, [isLoading, user, router, params.id]);

  if (isLoading || !user) return null;

  const onBack = wsList.length > 0 ? () => router.push(paths.root()) : undefined;

  return <InvitePage invitationId={params.id} onBack={onBack} />;
}
