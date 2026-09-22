// Пакет dispatch хранит единый словарь исходов допуска задач к выполнению.
// Это листовой пакет без внутренних зависимостей, чтобы сервисный и handler-слой
// использовали один и тот же enum.
package dispatch

type ReasonCode string

const (
	ReasonQueued    ReasonCode = "queued"
	ReasonCoalesced ReasonCode = "coalesced"
	ReasonDeferred  ReasonCode = "deferred"

	ReasonInvocationNotAllowed ReasonCode = "invocation_not_allowed"

	ReasonTargetUnavailable ReasonCode = "target_unavailable"

	ReasonRuntimeOffline ReasonCode = "runtime_offline"

	ReasonAttributionBlocked ReasonCode = "attribution_blocked"

	ReasonAlreadyActive ReasonCode = "already_active"

	ReasonSelfTriggerSuppressed ReasonCode = "self_trigger_suppressed"

	ReasonInternalError ReasonCode = "internal_error"
)
