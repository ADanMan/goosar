import { api } from '@goosar/core/api';
import { useAuthStore } from '@goosar/core/auth';
import { configStore, isPerimeterDeliveryProfile } from '@goosar/core/config';
import type { Agent } from '@goosar/core/types';
import { buildHelperMcpConfig } from '../onboarding/presets';

export const HELPER_SYSTEM_KEY = 'goosar_helper';

export const HELPER_AGENT_NAME = 'Goosar Helper';

export const HELPER_AVATAR_URL =
  "data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 1024 1024'%3E%3Cdefs%3E%3ClinearGradient id='t' x1='0' y1='0' x2='0' y2='1'%3E%3Cstop offset='0%25' stop-color='%2323242C'/%3E%3Cstop offset='100%25' stop-color='%2313141A'/%3E%3C/linearGradient%3E%3C/defs%3E%3Crect width='1024' height='1024' rx='224' fill='url(%23t)'/%3E%3Ccircle cx='512' cy='512' r='300' fill='%23FFFFFF'/%3E%3Ccircle cx='512' cy='512' r='174' fill='%23F0BE32'/%3E%3C/svg%3E";

export function isWorkspaceHelper(agent: Agent): boolean {
  if (agent.archived_at) return false;
  const systemKey = agent.system_key ?? '';
  if (systemKey !== '') return systemKey === HELPER_SYSTEM_KEY;
  return agent.name === HELPER_AGENT_NAME && agent.visibility === 'workspace';
}

function isOwnWorkspaceHelper(agent: Agent, userId: string | null): boolean {
  if (!isWorkspaceHelper(agent)) return false;
  const ownerId = agent.owner_id ?? null;
  if (ownerId === null) return true;
  return userId !== null && ownerId === userId;
}

async function findWorkspaceHelper(workspaceId: string): Promise<Agent | null> {
  try {
    const agents = await api.listAgents({ workspace_id: workspaceId });
    const userId = useAuthStore.getState().user?.id ?? null;
    return pickOwnWorkspaceHelper(agents ?? [], userId);
  } catch {
    return null;
  }
}

export function pickOwnWorkspaceHelper(
  agents: readonly Agent[],
  userId: string | null,
): Agent | null {
  const candidates = agents.filter((agent) => isOwnWorkspaceHelper(agent, userId));
  return candidates.find((agent) => (agent.owner_id ?? null) === userId) ?? candidates[0] ?? null;
}

export function findOwnWorkspaceHelper(workspaceId: string): Promise<Agent | null> {
  return findWorkspaceHelper(workspaceId);
}

async function shouldSeedCorporatePresets(workspaceId: string): Promise<boolean> {
  if (!isPerimeterDeliveryProfile(configStore.getState())) return true;
  const userId = useAuthStore.getState().user?.id ?? null;
  if (userId === null) return false;
  try {
    const members = await api.listMembers(workspaceId);
    return members.some((m) => m.user_id === userId && m.perimeter_access === true);
  } catch {
    return false;
  }
}

async function corporatePresetPatch(
  workspaceId: string,
  helper: Agent,
): Promise<{ mcp_config?: unknown }> {
  if (helper.mcp_config !== null && helper.mcp_config !== undefined) return {};
  if (helper.mcp_config_redacted === true) return {};
  if (!(await shouldSeedCorporatePresets(workspaceId))) return {};
  return { mcp_config: buildHelperMcpConfig() };
}

const pendingHelperPrep = new Map<string, Promise<Agent | null>>();

export function prepareWorkspaceHelper(
  workspaceId: string,
  runtimeId: string | null,
): Promise<Agent | null> {
  const key = `${workspaceId}:${runtimeId ?? ''}`;
  const existing = pendingHelperPrep.get(key);
  if (existing) return existing;

  const promise = (async (): Promise<Agent | null> => {
    const helper = await findWorkspaceHelper(workspaceId);
    if (!helper) return null;

    const rebind =
      runtimeId !== null && helper.runtime_id !== runtimeId ? { runtime_id: runtimeId } : {};
    const preset = await corporatePresetPatch(workspaceId, helper);

    let current = helper;
    const applied: Record<string, unknown> = {};
    for (const patch of [rebind, preset]) {
      if (Object.keys(patch).length === 0) continue;
      try {
        const updated = await api.updateAgent(current.id, patch);
        Object.assign(applied, patch);
        if (updated?.id) current = updated;
      } catch {
        // The Helper exists and is usable; only this refinement failed. The
        // welcome flow must not turn into an error screen over it, and the
        // next refinement still gets its chance.
      }
    }
    return Object.keys(applied).length > 0 ? { ...current, ...applied } : current;
  })();

  pendingHelperPrep.set(key, promise);
  promise
    .finally(() => {
      if (pendingHelperPrep.get(key) === promise) {
        pendingHelperPrep.delete(key);
      }
    })
    .catch(() => {});
  return promise;
}
