'use client';

import { useQuery } from '@tanstack/react-query';
import { useAuthStore } from '../auth';
import type { MemberRole, MemberWithUser } from '../types';
import { memberListOptions } from '../workspace/queries';

export function useCurrentMember(wsId: string): {
  userId: string | null;
  role: MemberRole | null;
  member: MemberWithUser | null;
  isLoading: boolean;
} {
  const userId = useAuthStore((s) => s.user?.id ?? null);
  const { data: members, isLoading } = useQuery(memberListOptions(wsId));
  const member = members?.find((m) => m.user_id === userId) ?? null;
  return {
    userId,
    role: member?.role ?? null,
    member,
    isLoading,
  };
}
