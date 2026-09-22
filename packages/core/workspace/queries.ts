import { queryOptions } from '@tanstack/react-query';
import { api } from '../api';
import type { Agent, Squad, Workspace } from '../types';

export const workspaceKeys = {
  all: (wsId: string) => ['workspaces', wsId] as const,
  list: () => ['workspaces', 'list'] as const,
  members: (wsId: string) => ['workspaces', wsId, 'members'] as const,
  invitations: (wsId: string) => ['workspaces', wsId, 'invitations'] as const,
  myInvitations: () => ['invitations', 'mine'] as const,
  agents: (wsId: string) => ['workspaces', wsId, 'agents'] as const,
  squads: (wsId: string) => ['workspaces', wsId, 'squads'] as const,
  squadMemberStatus: (wsId: string, squadId: string) =>
    ['workspaces', wsId, 'squads', squadId, 'members-status'] as const,
  skills: (wsId: string) => ['workspaces', wsId, 'skills'] as const,
  assigneeFrequency: (wsId: string) => ['workspaces', wsId, 'assignee-frequency'] as const,
  templates: () => ['workspace-templates'] as const,
  joinTargets: () => ['deployment', 'join-targets'] as const,
  capabilities: (wsId: string) => ['workspaces', wsId, 'capabilities'] as const,
};

export function workspaceListOptions() {
  return queryOptions({
    queryKey: workspaceKeys.list(),
    queryFn: () => api.listWorkspaces(),
  });
}

export function workspaceTemplateListOptions() {
  return queryOptions({
    queryKey: workspaceKeys.templates(),
    queryFn: () => api.listWorkspaceTemplates(),
  });
}

export function workspaceCapabilitiesOptions(wsId: string) {
  return queryOptions({
    queryKey: workspaceKeys.capabilities(wsId),
    queryFn: () => api.getWorkspaceCapabilities(wsId),
    enabled: wsId !== '',
    staleTime: 5 * 60 * 1000,
  });
}

export function workspaceBySlugOptions(slug: string) {
  return queryOptions({
    ...workspaceListOptions(),
    select: (list: Workspace[]) => list.find((w) => w.slug === slug) ?? null,
  });
}

export function memberListOptions(wsId: string) {
  return queryOptions({
    queryKey: workspaceKeys.members(wsId),
    queryFn: () => api.listMembers(wsId),
    enabled: wsId !== '',
  });
}

export function agentListOptions(wsId: string) {
  return queryOptions({
    queryKey: workspaceKeys.agents(wsId),
    queryFn: () => api.listAgents({ workspace_id: wsId, include_archived: true }),
  });
}

export function squadListOptions(wsId: string) {
  return queryOptions<Squad[]>({
    queryKey: workspaceKeys.squads(wsId),
    queryFn: () => api.listSquads(),
    enabled: !!wsId,
  });
}

export function squadMemberStatusOptions(wsId: string, squadId: string) {
  return queryOptions({
    queryKey: workspaceKeys.squadMemberStatus(wsId, squadId),
    queryFn: () => api.getSquadMemberStatus(squadId),
    enabled: !!wsId && !!squadId,
    staleTime: 30 * 1000,
    refetchOnWindowFocus: true,
  });
}

export function skillListOptions(wsId: string) {
  return queryOptions({
    queryKey: workspaceKeys.skills(wsId),
    queryFn: () => api.listSkills(),
  });
}

export function skillDetailOptions(wsId: string, skillId: string) {
  return queryOptions({
    queryKey: [...workspaceKeys.skills(wsId), skillId] as const,
    queryFn: () => api.getSkill(skillId),
    enabled: !!skillId,
  });
}

export function selectSkillAssignments(agents: Agent[] | undefined): Map<string, Agent[]> {
  const map = new Map<string, Agent[]>();
  if (!agents) return map;
  for (const a of agents) {
    if (a.archived_at) continue;
    for (const s of a.skills ?? []) {
      const existing = map.get(s.id);
      if (existing) existing.push(a);
      else map.set(s.id, [a]);
    }
  }
  return map;
}

export function invitationListOptions(wsId: string) {
  return queryOptions({
    queryKey: workspaceKeys.invitations(wsId),
    queryFn: () => api.listWorkspaceInvitations(wsId),
  });
}

export function myInvitationListOptions() {
  return queryOptions({
    queryKey: workspaceKeys.myInvitations(),
    queryFn: () => api.listMyInvitations(),
  });
}

export function assigneeFrequencyOptions(wsId: string) {
  return queryOptions({
    queryKey: workspaceKeys.assigneeFrequency(wsId),
    queryFn: () => api.getAssigneeFrequency(),
  });
}

export function joinTargetListOptions() {
  return queryOptions({
    queryKey: workspaceKeys.joinTargets(),
    queryFn: () => api.listJoinTargets(),
  });
}
