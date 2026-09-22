'use client';

import type { Agent, Skill } from '../types';
import { useCurrentMember } from './use-current-member';
import { canAssignAgentToIssue, canDeleteSkill, canEditAgent, canEditSkill } from './rules';
import { deny, type Decision } from './types';

const PENDING: Decision = deny('unknown', '');

export function useAgentPermissions(
  agent: Agent | null,
  wsId: string,
): {
  canEdit: Decision;
  canAssign: Decision;
  isLoading: boolean;
} {
  const { userId, role, isLoading } = useCurrentMember(wsId);
  const ctx = { userId, role };
  if (agent === null) {
    return { canEdit: PENDING, canAssign: PENDING, isLoading };
  }
  return {
    canEdit: canEditAgent(agent, ctx),
    canAssign: canAssignAgentToIssue(agent, ctx),
    isLoading,
  };
}

export function useSkillPermissions(
  skill: Skill | null,
  wsId: string,
): {
  canEdit: Decision;
  canDelete: Decision;
} {
  const { userId, role } = useCurrentMember(wsId);
  const ctx = { userId, role };
  if (skill === null) {
    return { canEdit: PENDING, canDelete: PENDING };
  }
  return {
    canEdit: canEditSkill(skill, ctx),
    canDelete: canDeleteSkill(skill, ctx),
  };
}
