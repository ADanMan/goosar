'use client';

import { useQuery } from '@tanstack/react-query';
import type { MemberRole, SkillSummary } from '@goosar/core/types';
import { useAuthStore } from '@goosar/core/auth';
import { memberListOptions } from '@goosar/core/workspace/queries';

export function useCanEditSkill(skill: SkillSummary | null | undefined, wsId: string): boolean {
  const userId = useAuthStore((s) => s.user?.id ?? null);
  const { data: members = [] } = useQuery(memberListOptions(wsId));

  if (!skill) return false;
  const myRole = members.find((m) => m.user_id === userId)?.role ?? null;
  return canEditSkill(skill, { userId, role: myRole });
}

export function canEditSkill(
  skill: SkillSummary,
  opts: { userId: string | null; role: MemberRole | null },
): boolean {
  if (opts.role === 'admin' || opts.role === 'owner') return true;
  return skill.created_by === opts.userId;
}
