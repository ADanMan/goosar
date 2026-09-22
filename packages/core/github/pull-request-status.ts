import type {
  GitHubPullRequestChecksConclusion,
  GitHubPullRequestChecksRollup,
  GitHubPullRequestMergeable,
  GitHubPullRequestMergeStateStatus,
} from '../types';

export type PullRequestChecksStatus =
  | { kind: 'failed'; failed: number; total: number; names: string[] }
  | { kind: 'pending'; passed: number; total: number; running: number }
  | { kind: 'passed'; total: number }
  | { kind: 'none' }
  | { kind: 'unavailable' };

export interface PullRequestChecksInput {
  snapshot_available?: boolean;
  checks_rollup?: GitHubPullRequestChecksRollup | null;
  checks_conclusion?: GitHubPullRequestChecksConclusion | null;
  checks_total?: number;
  checks_passed?: number;
  checks_failed?: number;
  checks_running?: number;
  checks_pending?: number;
  failed_check_names?: string[];
}

export function deriveChecksStatus(input: PullRequestChecksInput): PullRequestChecksStatus {
  if (input.snapshot_available === false) {
    return { kind: 'unavailable' };
  }
  const rollup = input.checks_rollup ?? null;
  const total = input.checks_total ?? 0;
  const passed = input.checks_passed ?? 0;
  const failed = input.checks_failed ?? 0;
  const running = input.checks_running ?? input.checks_pending ?? 0;
  const names = input.failed_check_names ?? [];

  if (rollup === 'failure' || rollup === 'error' || failed > 0) {
    return { kind: 'failed', failed, total, names };
  }
  if (rollup === 'pending' || rollup === 'expected') {
    return { kind: 'pending', passed, total, running };
  }
  if (rollup === 'success') {
    return { kind: 'passed', total };
  }
  if (input.checks_conclusion === 'failed') {
    return { kind: 'failed', failed, total, names };
  }
  if (input.checks_conclusion === 'pending') {
    return { kind: 'pending', passed, total, running };
  }
  if (input.checks_conclusion === 'passed') {
    return { kind: 'passed', total };
  }
  return input.snapshot_available === true ? { kind: 'none' } : { kind: 'unavailable' };
}

export type PullRequestMergeStatus =
  | { kind: 'conflicting' }
  | { kind: 'ready' }
  | { kind: 'blocked' }
  | { kind: 'behind' }
  | { kind: 'unstable' }
  | { kind: 'has_hooks' }
  | { kind: 'none' };

export interface PullRequestMergeInput {
  snapshot_available?: boolean;
  mergeable?: GitHubPullRequestMergeable | null;
  merge_state_status?: GitHubPullRequestMergeStateStatus | null;
}

export function deriveMergeStatus(input: PullRequestMergeInput): PullRequestMergeStatus {
  if (input.snapshot_available === false) return { kind: 'none' };
  const mergeable = input.mergeable ?? null;
  const mergeState = input.merge_state_status ?? null;

  if (mergeable === 'conflicting' || mergeState === 'dirty') return { kind: 'conflicting' };
  if (mergeState === 'clean') return { kind: 'ready' };
  if (mergeState === 'blocked') return { kind: 'blocked' };
  if (mergeState === 'behind') return { kind: 'behind' };
  if (mergeState === 'unstable') return { kind: 'unstable' };
  if (mergeState === 'has_hooks') return { kind: 'has_hooks' };
  return { kind: 'none' };
}

export interface PullRequestStatsInput {
  additions?: number;
  deletions?: number;
  changed_files?: number;
}

export function shouldShowPullRequestStats(input: PullRequestStatsInput): boolean {
  const a = input.additions ?? 0;
  const d = input.deletions ?? 0;
  const f = input.changed_files ?? 0;
  return a + d + f > 0;
}
