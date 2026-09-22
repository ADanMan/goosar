// Человекочитаемые подписи для причины падения задачи на бэкенде, показываются в UI.
const REASON_LABEL: Record<string, string> = {
  queued_expired: 'Expired in queue',
  runtime_offline: 'Daemon offline',
  runtime_reconnect_timeout: 'Daemon did not reconnect in time',
  runtime_recovery: 'Daemon restarted',
  timeout: 'Task timed out',
  iteration_limit: 'Hit the iteration limit',
  agent_blocked: 'Waiting on human input',
  api_invalid_request: 'Rejected by the model API',
  skill_bundle_unavailable: "Couldn't download the agent's skills",

  'agent_error.provider_auth_or_access': 'Provider auth failed',
  'agent_error.provider_quota_limit': 'Provider quota exhausted',
  'agent_error.provider_capacity_or_rate_limit': 'Rate limited by provider',
  'agent_error.provider_server_error': 'Provider server error',
  'agent_error.provider_network': 'Network error reaching provider',

  'agent_error.process_failure': 'Agent process crashed',
  'agent_error.empty_or_unparseable_output': 'Agent returned no usable output',
  'agent_error.agent_timeout': 'Agent timed out',
  'agent_error.context_overflow': 'Context window exceeded',
  'agent_error.missing_config': 'Missing API key or configuration',
  'agent_error.model_not_found_or_unavailable': 'Model unavailable',
  'agent_error.runtime_version_unsupported': 'Runner CLI version unsupported',
  'agent_error.runtime_missing_executable': 'Runner CLI not installed',
  'agent_error.unknown': 'Agent execution error',

  agent_error: 'Agent execution error',
  codex_semantic_inactivity: 'Codex semantic inactivity timeout',
  manual: 'Cancelled by user',
};

export function failureReasonLabel(reason: string | null | undefined): string | null {
  if (!reason) return null;
  return REASON_LABEL[reason] ?? reason;
}
