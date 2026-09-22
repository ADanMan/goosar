// Пакет taskfailure — каноническая таксономия значений failure_reason
// в agent_task_queue и chat_message: вместо грубого «agent_error» — конкретные
// причины (401 провайдера, исчерпание квоты и т. д.).
package taskfailure

import "strings"

type Reason string

const agentErrorPrefix = "agent_error."

const (
	ReasonQueuedExpired Reason = "queued_expired"

	ReasonRuntimeOffline Reason = "runtime_offline"

	ReasonRuntimeReconnectTimeout Reason = "runtime_reconnect_timeout"

	ReasonRuntimeRecovery Reason = "runtime_recovery"

	ReasonTimeout Reason = "timeout"

	ReasonIterationLimit Reason = "iteration_limit"

	ReasonAgentBlocked Reason = "agent_blocked"

	ReasonAPIInvalidRequest Reason = "api_invalid_request"

	ReasonSkillBundleUnavailable Reason = "skill_bundle_unavailable"

	ReasonEnvRootBusy Reason = "env_root_busy"

	ReasonAgentProviderAuthOrAccess Reason = "agent_error.provider_auth_or_access"

	ReasonAgentProviderQuotaLimit Reason = "agent_error.provider_quota_limit"

	ReasonAgentProviderCapacityOrRateLimit Reason = "agent_error.provider_capacity_or_rate_limit"

	ReasonAgentProviderServerError Reason = "agent_error.provider_server_error"

	ReasonAgentProviderNetwork Reason = "agent_error.provider_network"

	ReasonAgentProcessFailure Reason = "agent_error.process_failure"

	ReasonAgentEmptyOrUnparseableOutput Reason = "agent_error.empty_or_unparseable_output"

	ReasonAgentTimeout Reason = "agent_error.agent_timeout"

	ReasonAgentContextOverflow Reason = "agent_error.context_overflow"

	ReasonAgentMissingConfig Reason = "agent_error.missing_config"

	ReasonAgentModelNotFoundOrUnavailable Reason = "agent_error.model_not_found_or_unavailable"

	ReasonAgentRuntimeVersionUnsupported Reason = "agent_error.runtime_version_unsupported"

	ReasonAgentRuntimeMissingExecutable Reason = "agent_error.runtime_missing_executable"

	ReasonAgentUnknown Reason = "agent_error.unknown"
)

var allReasons = []Reason{

	ReasonQueuedExpired,
	ReasonRuntimeOffline,
	ReasonRuntimeReconnectTimeout,
	ReasonRuntimeRecovery,
	ReasonTimeout,
	ReasonIterationLimit,
	ReasonAgentBlocked,
	ReasonAPIInvalidRequest,
	ReasonSkillBundleUnavailable,
	ReasonEnvRootBusy,

	ReasonAgentProviderAuthOrAccess,
	ReasonAgentProviderQuotaLimit,
	ReasonAgentProviderCapacityOrRateLimit,
	ReasonAgentProviderServerError,
	ReasonAgentProviderNetwork,

	ReasonAgentProcessFailure,
	ReasonAgentEmptyOrUnparseableOutput,
	ReasonAgentTimeout,
	ReasonAgentContextOverflow,
	ReasonAgentMissingConfig,
	ReasonAgentModelNotFoundOrUnavailable,
	ReasonAgentRuntimeVersionUnsupported,
	ReasonAgentRuntimeMissingExecutable,

	ReasonAgentUnknown,
}

func (r Reason) String() string { return string(r) }

func (r Reason) IsAgentError() bool {
	return strings.HasPrefix(string(r), agentErrorPrefix)
}

func AllReasons() []Reason {
	out := make([]Reason, len(allReasons))
	copy(out, allReasons)
	return out
}
