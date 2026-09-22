package daemon

import (
	"strings"
	"testing"

	"github.com/adanman/goosar/server/pkg/agent"
)

func TestClassifyPoisonedOutput(t *testing.T) {
	cases := []struct {
		name       string
		output     string
		wantOK     bool
		wantReason string
	}{
		{
			name:       "iteration limit canonical",
			output:     "I reached the iteration limit and couldn't generate a summary.",
			wantOK:     true,
			wantReason: FailureReasonIterationLimit,
		},
		{
			name:       "iteration limit case insensitive",
			output:     "I REACHED THE ITERATION LIMIT and stopped",
			wantOK:     true,
			wantReason: FailureReasonIterationLimit,
		},
		{
			name:       "fallback meta message",
			output:     "Put your final update inside the content string. Keep it concise.",
			wantOK:     true,
			wantReason: FailureReasonAgentFallbackMsg,
		},
		{
			name:   "real conclusion is not poisoned",
			output: "Fixed the bug in auth.go and pushed PR #42.",
			wantOK: false,
		},
		{
			name:   "empty output",
			output: "",
			wantOK: false,
		},
		{
			name:   "mentions iteration but not the marker",
			output: "Each iteration of the loop processes one record.",
			wantOK: false,
		},
		{

			name: "long review quoting both markers is not poisoned",
			output: `Review for the rerun fix.

Detection markers under consideration:
- "I reached the iteration limit and couldn't generate a summary."
- "Put your final update inside the content string. Keep it concise."

The implementation looks correct: the daemon classifies these as
fallback output, persists a dedicated failure_reason, and the SQL
filter excludes them from the resume lookup. Resume-safe auto-retry
still keeps the resume contract, while poisoned sessions are filtered.
Approving with a follow-up note about the matcher being too permissive
on long outputs.`,
			wantOK: false,
		},
		{
			name:   "marker buried inside a long agent conclusion",
			output: strings.Repeat("All checks passed and the bug is fixed. ", 10) + "i reached the iteration limit while debugging earlier.",
			wantOK: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reason, ok := classifyPoisonedOutput(tc.output)
			if ok != tc.wantOK {
				t.Fatalf("classifyPoisonedOutput(%q) ok=%v, want %v", tc.output, ok, tc.wantOK)
			}
			if ok && reason != tc.wantReason {
				t.Fatalf("classifyPoisonedOutput(%q) reason=%q, want %q", tc.output, reason, tc.wantReason)
			}
		})
	}
}

func TestClassifyPoisonedError(t *testing.T) {
	cases := []struct {
		name       string
		errMsg     string
		wantOK     bool
		wantReason string
	}{
		{

			name:       "claude could not process image",
			errMsg:     `API Error: 400 {"type":"error","error":{"type":"invalid_request_error","message":"Could not process image"},"request_id":"req_011CarVEtBLj95zD7i8xardY"}`,
			wantOK:     true,
			wantReason: FailureReasonAPIInvalidRequest,
		},
		{
			name:       "prompt too long is also poisoning",
			errMsg:     `API Error: 400 {"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long: 213000 tokens > 200000 maximum"}}`,
			wantOK:     true,
			wantReason: FailureReasonAPIInvalidRequest,
		},
		{
			name:       "case insensitive",
			errMsg:     `api error: 400 {"type":"INVALID_REQUEST_ERROR"}`,
			wantOK:     true,
			wantReason: FailureReasonAPIInvalidRequest,
		},
		{

			name:   "429 rate limit is transient",
			errMsg: `API Error: 429 {"type":"error","error":{"type":"rate_limit_error","message":"Number of request tokens has exceeded your per-minute rate limit"}}`,
			wantOK: false,
		},
		{
			name:   "5xx overloaded is transient",
			errMsg: `API Error: 529 {"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`,
			wantOK: false,
		},
		{

			name:   "401 auth error",
			errMsg: `API Error: 401 {"type":"error","error":{"type":"authentication_error","message":"invalid api key"}}`,
			wantOK: false,
		},
		{

			name:   "tool 400 without invalid_request_error",
			errMsg: `agent tool returned status 400: not found`,
			wantOK: false,
		},
		{
			name:   "empty error message",
			errMsg: "",
			wantOK: false,
		},
		{
			name:   "unrelated execution error",
			errMsg: "claude execution timeout after 10m",
			wantOK: false,
		},
		{

			name:       "kiro oversized history image",
			errMsg:     `kiro session/prompt failed: session/prompt: Internal error (code=-32603, data=Encountered an error in the response stream: messages.14.content.0.image.source.base64.data: At least one of the image dimensions exceed max allowed size: 8000 pixels)`,
			wantOK:     true,
			wantReason: FailureReasonAPIInvalidRequest,
		},
		{

			name:   "plain kiro internal error is not poisoning",
			errMsg: `kiro session/prompt failed: session/prompt: Internal error (code=-32603, data=Kiro failed to generate a response)`,
			wantOK: false,
		},
		{

			name:   "dimension phrase without image-content marker",
			errMsg: `some tool reported: image dimensions exceed max allowed size: 8000 pixels`,
			wantOK: false,
		},
		{

			name:       "hermes provider 400 on resumed prompt",
			errMsg:     `hermes session/prompt failed: session/prompt: Internal error (code=-32603, data=BadRequestError)`,
			wantOK:     true,
			wantReason: FailureReasonAPIInvalidRequest,
		},
		{

			name:   "hermes context window exceeded is not poisoning",
			errMsg: `hermes session/prompt failed: session/prompt: Internal error (code=-32603, data=ContextWindowExceededError)`,
			wantOK: false,
		},
		{

			name:   "hermes unknown exception class",
			errMsg: `hermes session/prompt failed: session/prompt: Internal error (code=-32603, data=TimeoutError)`,
			wantOK: false,
		},
		{

			name:   "hermes frame on session/new is out of scope",
			errMsg: `hermes session/new failed: session/new: Internal error (code=-32603, data=BadRequestError)`,
			wantOK: false,
		},
		{

			name:   "hermes llm_not_configured is environmental",
			errMsg: `hermes session/prompt failed: session/prompt: config.user.yaml llm.api_key is not set (code=-32603, data=llm_not_configured)`,
			wantOK: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reason, ok := classifyPoisonedError(tc.errMsg)
			if ok != tc.wantOK {
				t.Fatalf("classifyPoisonedError(%q) ok=%v, want %v", tc.errMsg, ok, tc.wantOK)
			}
			if ok && reason != tc.wantReason {
				t.Fatalf("classifyPoisonedError(%q) reason=%q, want %q", tc.errMsg, reason, tc.wantReason)
			}
		})
	}
}

func TestClassifyResumeUnsafeTimeout(t *testing.T) {
	cases := []struct {
		name       string
		provider   string
		errMsg     string
		wantOK     bool
		wantReason string
	}{
		{
			name:       "runtime-e semantic inactivity",
			provider:   "runtime-e",
			errMsg:     agent.RuntimeESemanticInactivityMarker + " after 10m0s without agent progress (last activity: tool-result:exec_command)",
			wantOK:     true,
			wantReason: FailureReasonCodexSemanticInactivity,
		},
		{
			name:       "runtime-e first turn no progress",
			provider:   "runtime-e",
			errMsg:     agent.RuntimeEFirstTurnNoProgressMarker + ` after 30s: received turn start but no item, turn/completed, or error event`,
			wantOK:     true,
			wantReason: FailureReasonCodexSemanticInactivity,
		},
		{
			name:     "runtime-e ordinary timeout remains resumable",
			provider: "runtime-e",
			errMsg:   "codex timed out after 30m0s",
			wantOK:   false,
		},
		{
			name:     "other provider same text is not classified",
			provider: "runtime-c",
			errMsg:   agent.RuntimeESemanticInactivityMarker + " after 10m0s without agent progress",
			wantOK:   false,
		},
		{
			name:     "empty error",
			provider: "runtime-e",
			errMsg:   "",
			wantOK:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reason, ok := classifyResumeUnsafeTimeout(tc.provider, tc.errMsg)
			if ok != tc.wantOK {
				t.Fatalf("classifyResumeUnsafeTimeout(%q, %q) ok=%v, want %v", tc.provider, tc.errMsg, ok, tc.wantOK)
			}
			if ok && reason != tc.wantReason {
				t.Fatalf("classifyResumeUnsafeTimeout(%q, %q) reason=%q, want %q", tc.provider, tc.errMsg, reason, tc.wantReason)
			}
		})
	}
}
