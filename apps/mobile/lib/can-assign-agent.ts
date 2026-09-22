/**
 * Мобильная копия правила, определяющего, можно ли назначить агента на
 * issue. Логика продублирована (а не импортирована из core), поэтому при
 * изменении правила на бэкенде/вебе её нужно синхронизировать вручную.
 *
 * Правило: агенты с видимостью workspace — доступны всем участникам;
 * приватные агенты — только владельцу и админам/владельцам workspace.
 */
import type { Agent } from '@goosar/core/types';

type MemberRoleLike = 'owner' | 'admin' | 'member' | null | undefined;

export function canAssignAgent(
  agent: Agent,
  userId: string | undefined | null,
  memberRole: MemberRoleLike,
): boolean {
  if (!userId) return false;

  const role: MemberRoleLike =
    memberRole === 'owner' || memberRole === 'admin' || memberRole === 'member' ? memberRole : null;

  if (agent.visibility === 'workspace') {
    return role !== null;
  }
  if (role === 'owner' || role === 'admin') return true;
  return agent.owner_id !== null && agent.owner_id === userId;
}
