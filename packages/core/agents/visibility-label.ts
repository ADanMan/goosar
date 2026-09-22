import type { AgentVisibility } from '../types';

export const VISIBILITY_LABEL: Record<AgentVisibility, string> = {
  workspace: 'Workspace',
  private: 'Personal',
};

export const VISIBILITY_DESCRIPTION: Record<AgentVisibility, string> = {
  workspace: 'All members can assign',
  private: 'Only you and workspace admins can assign',
};

export const VISIBILITY_TOOLTIP: Record<AgentVisibility, string> = {
  workspace: 'Workspace — all members can assign',
  private: 'Personal — only you and workspace admins can assign',
};

export function visibilityLabel(v: AgentVisibility): string {
  return VISIBILITY_LABEL[v];
}
