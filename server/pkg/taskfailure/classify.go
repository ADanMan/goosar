package taskfailure

import (
	"regexp"
	"strings"
)

var providerHTTP5xxRe = regexp.MustCompile(`(^|[^0-9])5[0-9][0-9]([^0-9]|$)`)

var (
	httpAuthCodeRe     = regexp.MustCompile(`(^|[^0-9])(401|403)([^0-9]|$)`)
	httpQuotaCodeRe    = regexp.MustCompile(`(^|[^0-9])402([^0-9]|$)`)
	httpCapacityCodeRe = regexp.MustCompile(`(^|[^0-9])(429|529)([^0-9]|$)`)
)

func Classify(rawError string) Reason {
	trimmed := strings.TrimSpace(rawError)
	if trimmed == "" {

		return ReasonAgentUnknown
	}
	lower := strings.ToLower(trimmed)

	switch {

	case containsAny(lower,
		"context length",
		"context_length_exceeded",
		"maximum context",
		"prompt is too long",
		"context size has been exceeded",
	),
		containsAny(lower, contextWindowExceededWitnesses...),

		strings.Contains(lower, "token") && strings.Contains(lower, "limit"):
		return ReasonAgentContextOverflow

	case strings.Contains(lower, "missing environment variable"),
		strings.Contains(lower, "missing") && strings.Contains(lower, "api_key"),
		strings.Contains(lower, "api key") && strings.Contains(lower, "required"),
		strings.Contains(lower, "no llm provider configured"),
		strings.Contains(lower, "no provider configured"):
		return ReasonAgentMissingConfig

	case httpAuthCodeRe.MatchString(lower),
		containsAny(lower,
			"unauthorized",
			"login required",
			"not logged in",
			"please login again",
			"refresh token",
			"invalid api key",
			"access token",
			"subscription access",
			"does not have access",
			"you may not have access",
		):
		return ReasonAgentProviderAuthOrAccess

	case httpQuotaCodeRe.MatchString(lower),
		containsAny(lower,
			"insufficient_balance",
			"balance is too low",
			"monthly usage limit",
			"usage limit",
			"you've hit your limit",

			"you\u2019ve hit your limit",
			"credits",
			"quota",
		):
		return ReasonAgentProviderQuotaLimit

	case httpCapacityCodeRe.MatchString(lower),
		containsAny(lower,
			"rate limit",
			"overloaded",
			"no capacity available",
		):
		return ReasonAgentProviderCapacityOrRateLimit

	case containsAny(lower,
		"server had an error",
		"provider returned error",
		"internal error",
		"service unavailable",
		"bad gateway",
	),
		providerHTTP5xxRe.MatchString(lower):
		return ReasonAgentProviderServerError

	case containsAny(lower,
		"stream disconnected",
		"connection closed",
		"mid-response",
		"error sending request",
		"unable to connect",
		"dial tcp",
		"connection refused",
		"connectionrefused",
		"dns",
		"i/o timeout",
		"deadline exceeded",
		"timeout exceeded while awaiting",
	):
		return ReasonAgentProviderNetwork

	case strings.Contains(lower, "model") && strings.Contains(lower, "not found"),
		containsAny(lower,
			"unknown model",
			"selected model",
			"http 404",
			"404 page not found",
		):
		return ReasonAgentModelNotFoundOrUnavailable

	case containsAny(lower,
		"returned empty output",
		"returned no parseable output",
	):
		return ReasonAgentEmptyOrUnparseableOutput

	case strings.Contains(lower, "timed out after"):
		return ReasonAgentTimeout

	case strings.Contains(lower, "executable not found"):
		return ReasonAgentRuntimeMissingExecutable

	case containsAny(lower,
		"below the minimum supported version",
		"requires a newer version",
	):
		return ReasonAgentRuntimeVersionUnsupported

	case containsAny(lower,
		"exit status",
		"signal",
		"panic",
		"sigsegv",
		"process exited",
		"pipe has been ended",
		"file already closed",
		"initialize failed",
	):
		return ReasonAgentProcessFailure
	}

	return ReasonAgentUnknown
}

var contextWindowExceededWitnesses = []string{
	"context window limit",
	"model_context_window_exceeded",
}

const legacySkillBundlePrefix = "resolve skill bundles:"

var legacySkillBundleReasons = map[string]bool{
	string(ReasonAgentUnknown):         true,
	string(ReasonAgentProviderNetwork): true,
	"agent_error":                      true,
}

var legacyContextOverflowReasons = map[string]bool{
	string(ReasonAgentUnknown): true,
	"agent_error":              true,
}

func NormalizeDaemonReason(reason, rawError string) Reason {
	if legacySkillBundleReasons[reason] &&
		strings.HasPrefix(strings.TrimSpace(rawError), legacySkillBundlePrefix) {
		return ReasonSkillBundleUnavailable
	}

	if legacyContextOverflowReasons[reason] &&
		containsAny(strings.ToLower(rawError), contextWindowExceededWitnesses...) {
		return ReasonAgentContextOverflow
	}
	return Reason(reason)
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
