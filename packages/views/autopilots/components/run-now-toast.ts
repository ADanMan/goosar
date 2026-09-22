// Классифицирует результат ручного запуска автопилота ("Run now") в тип тоста.

export type RunNowToastKind = 'success' | 'warning' | 'error';

export function runNowToastKind(status: string | undefined): RunNowToastKind {
  switch (status) {
    case 'issue_created':
    case 'running':
      return 'success';
    case 'skipped':
      return 'warning';
    case 'failed':
      return 'error';
    default:
      return 'error';
  }
}

export type RunNowBlockedKey =
  | 'run_blocked_invocation_not_allowed'
  | 'run_blocked_runtime_offline'
  | 'run_blocked_target_unavailable'
  | 'run_blocked_attribution'
  | 'run_blocked_already_active'
  | 'run_blocked_generic';

export function runNowBlockedKey(reasonCode: string | undefined): RunNowBlockedKey {
  switch (reasonCode) {
    case 'invocation_not_allowed':
      return 'run_blocked_invocation_not_allowed';
    case 'runtime_offline':
      return 'run_blocked_runtime_offline';
    case 'target_unavailable':
      return 'run_blocked_target_unavailable';
    case 'attribution_blocked':
      return 'run_blocked_attribution';
    case 'already_active':
      return 'run_blocked_already_active';
    default:
      return 'run_blocked_generic';
  }
}
