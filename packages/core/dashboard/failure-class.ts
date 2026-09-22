// Группировка отображения для `agent_task_queue.failure_reason`.
export const FAILURE_CLASSES = [
  'auth',
  'rate_limit',
  'timeout',
  'provider',
  'runtime',
  'agent',
  'other',
] as const;

export type FailureClass = (typeof FAILURE_CLASSES)[number];

const REASON_CLASS: Record<string, FailureClass> = {
  'agent_error.provider_auth_or_access': 'auth',
  'agent_error.missing_config': 'auth',

  'agent_error.provider_capacity_or_rate_limit': 'rate_limit',
  'agent_error.provider_quota_limit': 'rate_limit',

  timeout: 'timeout',
  'agent_error.agent_timeout': 'timeout',
  codex_semantic_inactivity: 'timeout',

  'agent_error.provider_server_error': 'provider',
  'agent_error.provider_network': 'provider',
  'agent_error.model_not_found_or_unavailable': 'provider',
  api_invalid_request: 'provider',

  runtime_offline: 'runtime',
  runtime_recovery: 'runtime',
  queued_expired: 'runtime',
  'agent_error.runtime_missing_executable': 'runtime',
  'agent_error.runtime_version_unsupported': 'runtime',
  skill_bundle_unavailable: 'runtime',

  'agent_error.process_failure': 'agent',
  'agent_error.empty_or_unparseable_output': 'agent',
  'agent_error.context_overflow': 'agent',
  iteration_limit: 'agent',
  agent_blocked: 'agent',

  'agent_error.unknown': 'other',
  agent_error: 'other',
  manual: 'other',
  unclassified: 'other',
};

export function failureClassOf(reason: string): FailureClass {
  return REASON_CLASS[reason] ?? 'other';
}
