import type { Workspace } from '../types';

export interface GitHubSettings {
  enabled: boolean;
  prSidebar: boolean;
  coAuthor: boolean;
  autoLinkPRs: boolean;
}

export function deriveGitHubSettings(
  workspace: Pick<Workspace, 'settings'> | null | undefined,
): GitHubSettings {
  const s = (workspace?.settings ?? {}) as Record<string, unknown>;
  const enabled = s.github_enabled !== false;
  return {
    enabled,
    prSidebar: enabled && s.github_pr_sidebar_enabled !== false,
    coAuthor: enabled && s.co_authored_by_enabled !== false,
    autoLinkPRs: enabled && s.github_auto_link_prs_enabled !== false,
  };
}
