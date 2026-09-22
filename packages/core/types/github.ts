export type GitHubPullRequestState = 'open' | 'closed' | 'merged' | 'draft';

export type GitHubPullRequestChecksConclusion = 'passed' | 'failed' | 'pending';

export type GitHubMergeableState = string;

export type GitHubPullRequestMergeable = 'mergeable' | 'conflicting' | 'unknown';

export type GitHubPullRequestMergeStateStatus =
  'clean' | 'dirty' | 'blocked' | 'behind' | 'unstable' | 'draft' | 'has_hooks' | 'unknown';

export type GitHubPullRequestChecksRollup =
  'success' | 'failure' | 'pending' | 'error' | 'expected';

export interface GitHubInstallation {
  id: string;
  workspace_id: string;
  installation_id?: number;
  account_login: string;
  account_type: 'User' | 'Organization';
  account_avatar_url: string | null;
  created_at: string;
  connected_by?: string;
}

export interface GitHubPullRequest {
  id: string;
  provider?: 'github' | 'forgejo' | 'gitea' | 'gitlab';
  workspace_id: string;
  repo_owner: string;
  repo_name: string;
  number: number;
  title: string;
  state: GitHubPullRequestState;
  html_url: string;
  branch: string | null;
  author_login: string | null;
  author_avatar_url: string | null;
  merged_at: string | null;
  closed_at: string | null;
  pr_created_at: string;
  pr_updated_at: string;
  mergeable?: GitHubPullRequestMergeable | null;
  merge_state_status?: GitHubPullRequestMergeStateStatus | null;
  checks_rollup?: GitHubPullRequestChecksRollup | null;
  snapshot_available?: boolean;
  checks_total?: number;
  checks_passed?: number;
  checks_failed?: number;
  checks_running?: number;
  failed_check_names?: string[];
  snapshot_stale?: boolean;
  snapshot_fetched_at?: string | null;
  mergeable_state?: GitHubMergeableState | null;
  checks_conclusion?: GitHubPullRequestChecksConclusion | null;
  checks_pending?: number;
  additions?: number;
  deletions?: number;
  changed_files?: number;
}

export interface ListGitHubInstallationsResponse {
  installations: GitHubInstallation[];
  configured: boolean;
  repository_browse_configured?: boolean;
  can_manage?: boolean;
}

export interface GitHubConnectResponse {
  url?: string;
  configured: boolean;
}

export interface GitHubRepository {
  id: number;
  full_name: string;
  html_url: string;
  clone_url: string;
  description: string | null;
  private: boolean;
  archived: boolean;
  default_branch: string;
}

export interface ListGitHubRepositoriesResponse {
  repositories: GitHubRepository[];
  total_count: number;
  next_page: number | null;
}
