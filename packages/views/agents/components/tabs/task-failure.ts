import type { useT } from '../../../i18n';

type AgentsT = ReturnType<typeof useT<'agents'>>['t'];

// Сопоставление причины падения задачи, как её присылает бэкенд, с
// переведённой подписью из локали agents.json → task_failure.*.
function reasonLabelFromKey(reason: string, t: AgentsT): string | null {
  switch (reason) {
    case 'queued_expired':
      return t(($) => $.task_failure.queued_expired);
    case 'runtime_offline':
      return t(($) => $.task_failure.runtime_offline);
    case 'runtime_reconnect_timeout':
      return t(($) => $.task_failure.runtime_reconnect_timeout);
    case 'runtime_recovery':
      return t(($) => $.task_failure.runtime_recovery);
    case 'timeout':
      return t(($) => $.task_failure.timeout);
    case 'iteration_limit':
      return t(($) => $.task_failure.iteration_limit);
    case 'agent_blocked':
      return t(($) => $.task_failure.agent_blocked);
    case 'api_invalid_request':
      return t(($) => $.task_failure.api_invalid_request);
    case 'skill_bundle_unavailable':
      return t(($) => $.task_failure.skill_bundle_unavailable);

    case 'agent_error.provider_auth_or_access':
      return t(($) => $.task_failure.provider_auth_or_access);
    case 'agent_error.provider_quota_limit':
      return t(($) => $.task_failure.provider_quota_limit);
    case 'agent_error.provider_capacity_or_rate_limit':
      return t(($) => $.task_failure.provider_capacity_or_rate_limit);
    case 'agent_error.provider_server_error':
      return t(($) => $.task_failure.provider_server_error);
    case 'agent_error.provider_network':
      return t(($) => $.task_failure.provider_network);

    case 'agent_error.process_failure':
      return t(($) => $.task_failure.process_failure);
    case 'agent_error.empty_or_unparseable_output':
      return t(($) => $.task_failure.empty_or_unparseable_output);
    case 'agent_error.agent_timeout':
      return t(($) => $.task_failure.agent_timeout);
    case 'agent_error.context_overflow':
      return t(($) => $.task_failure.context_overflow);
    case 'agent_error.missing_config':
      return t(($) => $.task_failure.missing_config);
    case 'agent_error.model_not_found_or_unavailable':
      return t(($) => $.task_failure.model_not_found_or_unavailable);
    case 'agent_error.runtime_version_unsupported':
      return t(($) => $.task_failure.runtime_version_unsupported);
    case 'agent_error.runtime_missing_executable':
      return t(($) => $.task_failure.runtime_missing_executable);
    case 'agent_error.unknown':
      return t(($) => $.task_failure.agent_error_unknown);

    case 'agent_error':
      return t(($) => $.task_failure.agent_error);
    case 'codex_semantic_inactivity':
      return t(($) => $.task_failure.codex_semantic_inactivity);
    case 'manual':
      return t(($) => $.task_failure.manual);

    default:
      return null;
  }
}

export function failureReasonLabel(reason: string | null | undefined, t: AgentsT): string | null {
  if (!reason) return null;
  return reasonLabelFromKey(reason, t) ?? reason;
}
