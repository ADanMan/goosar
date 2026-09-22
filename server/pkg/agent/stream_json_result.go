package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"time"
)

const emptySuccessfulStreamResult = "The agent completed without a final response."

type streamTerminalState struct {
	lastAssistantText string
	finalResultText   string
	sawResult         bool
	resultIsError     bool
	scanErr           error
}

func streamFailureCause(provider string, timeout time.Duration, runErr, writeErr, exitErr error, sessionID string, state streamTerminalState) (failed bool, status, errMsg string) {
	switch {
	case errors.Is(runErr, context.DeadlineExceeded):
		return true, "timeout", fmt.Sprintf("%s timed out after %s", provider, timeout)
	case errors.Is(runErr, context.Canceled):
		return true, "aborted", "execution cancelled"
	case state.scanErr != nil:
		return true, "failed", fmt.Sprintf("%s stdout read error: %v", provider, state.scanErr)
	case writeErr != nil && sessionID == "":
		return true, "failed", fmt.Sprintf("write %s input: %v", provider, writeErr)
	case exitErr != nil:
		return true, "failed", fmt.Sprintf("%s exited with error: %v", provider, exitErr)
	case !state.sawResult:
		return true, "failed", provider + " stream ended without terminal result"
	default:
		return false, "", ""
	}
}

func finalizeStreamResult(
	provider string,
	timeout time.Duration,
	runErr error,
	writeErr error,
	exitErr error,
	sessionID string,
	state streamTerminalState,
	completionGuardError string,
) (status, output, errMsg string) {
	if state.resultIsError {
		errMsg = state.finalResultText
		if errMsg == "" {
			errMsg = provider + " returned an error result without details"
		}
		return "failed", "", errMsg
	}

	if failed, cause, detail := streamFailureCause(provider, timeout, runErr, writeErr, exitErr, sessionID, state); failed {
		return cause, "", detail
	}

	if completionGuardError != "" {
		return "failed", "", completionGuardError
	}

	switch {
	case state.finalResultText != "":
		return "completed", state.finalResultText, ""
	case state.lastAssistantText != "":
		return "completed", state.lastAssistantText, ""
	default:
		return "completed", emptySuccessfulStreamResult, ""
	}
}

type streamProtocolObservation struct {
	provider                   string
	cliVersion                 string
	model                      string
	exitCode                   int
	eventCount                 int
	invalidEventCount          int
	assistantEventCount        int
	toolUseCount               int
	sawResult                  bool
	resultIsError              bool
	resultBytes                int
	lastAssistantBytes         int
	scannerError               bool
	lastEventType              string
	anthropicBaseURLConfigured bool
}

func logStreamProtocolObservation(logger *slog.Logger, obs streamProtocolObservation) {
	logger.Info("agent stream protocol summary",
		"provider", obs.provider,
		"cli_version", obs.cliVersion,
		"model", obs.model,
		"exit_code", obs.exitCode,
		"event_count", obs.eventCount,
		"invalid_event_count", obs.invalidEventCount,
		"assistant_event_count", obs.assistantEventCount,
		"tool_use_count", obs.toolUseCount,
		"saw_result", obs.sawResult,
		"result_is_error", obs.resultIsError,
		"result_bytes", obs.resultBytes,
		"last_assistant_bytes", obs.lastAssistantBytes,
		"scanner_error", obs.scannerError,
		"last_event_type", obs.lastEventType,
		"anthropic_base_url_configured", obs.anthropicBaseURLConfigured,
	)
}

func streamProcessExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}
