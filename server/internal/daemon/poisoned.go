package daemon

import (
	"strings"

	"github.com/adanman/goosar/server/pkg/agent"
	"github.com/adanman/goosar/server/pkg/taskfailure"
)

const (
	FailureReasonIterationLimit          = string(taskfailure.ReasonIterationLimit)
	FailureReasonAgentFallbackMsg        = "agent_fallback_message"
	FailureReasonAPIInvalidRequest       = string(taskfailure.ReasonAPIInvalidRequest)
	FailureReasonCodexSemanticInactivity = "codex_semantic_inactivity"
)

const poisonedOutputCharLimit = 320

type fallbackMarker struct {
	needle string
	reason string
}

var knownFallbackMarkers = []fallbackMarker{
	{needle: "i reached the iteration limit", reason: FailureReasonIterationLimit},
	{needle: "put your final update inside the content string", reason: FailureReasonAgentFallbackMsg},
}

func classifyPoisonedOutput(output string) (string, bool) {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" || len(trimmed) > poisonedOutputCharLimit {
		return "", false
	}
	lowered := strings.ToLower(trimmed)
	for _, marker := range knownFallbackMarkers {
		if strings.Contains(lowered, marker.needle) {
			return marker.reason, true
		}
	}
	return "", false
}

func classifyPoisonedError(errMsg string) (string, bool) {
	if errMsg == "" {
		return "", false
	}
	lowered := strings.ToLower(errMsg)

	if isKiroOversizedHistoryImage(lowered) {
		return FailureReasonAPIInvalidRequest, true
	}
	if isAnthropicInvalidRequestBody(lowered) {
		return FailureReasonAPIInvalidRequest, true
	}
	if isDanglingToolCallBadRequest(lowered) {
		return FailureReasonAPIInvalidRequest, true
	}
	return "", false
}

func isAnthropicInvalidRequestBody(lowered string) bool {
	return strings.Contains(lowered, "invalid_request_error") && strings.Contains(lowered, "400")
}

func isKiroOversizedHistoryImage(lowered string) bool {
	return strings.Contains(lowered, "image dimensions exceed max allowed size") &&
		strings.Contains(lowered, "image.source.base64.data")
}

func isDanglingToolCallBadRequest(lowered string) bool {
	return strings.Contains(lowered, "session/prompt") &&
		strings.Contains(lowered, "(code=-32603, data=badrequesterror)")
}

func classifyResumeUnsafeTimeout(provider, errMsg string) (string, bool) {
	if errMsg == "" || !strings.EqualFold(strings.TrimSpace(provider), runtimeCodeE) {
		return "", false
	}
	lowered := strings.ToLower(errMsg)
	markers := []string{
		strings.ToLower(agent.RuntimeESemanticInactivityMarker),
		strings.ToLower(agent.RuntimeEFirstTurnNoProgressMarker),
	}
	for _, marker := range markers {
		if strings.Contains(lowered, marker) {
			return FailureReasonCodexSemanticInactivity, true
		}
	}
	return "", false
}
