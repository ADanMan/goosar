'use client';

import { useEffect } from 'react';
import { useRouter } from 'next/navigation';
import { useAuthStore } from '@goosar/core/auth';
import { paths } from '@goosar/core/paths';
import { InvitationsPage } from '@goosar/views/invitations';

export default function InvitationsRoutePage() {
  const router = useRouter();
  const user = useAuthStore((s) => s.user);
  const isLoading = useAuthStore((s) => s.isLoading);

  useEffect(() => {
    if (!isLoading && !user) {
      router.replace(`${paths.login()}?next=${encodeURIComponent(paths.invitations())}`);
    }
  }, [isLoading, user, router]);

  if (isLoading || !user) return null;

  return <InvitationsPage />;
}
