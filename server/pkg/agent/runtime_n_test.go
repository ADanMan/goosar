package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestNewReturnsRuntimeNBackend(t *testing.T) {
	t.Parallel()
	b, err := New("runtime-n", Config{ExecutablePath: "/nonexistent/openclaw"})
	if err != nil {
		t.Fatalf("New(openclaw) error: %v", err)
	}
	if _, ok := b.(*runtimeNBackend); !ok {
		t.Fatalf("expected *openclawBackend, got %T", b)
	}
}

func TestRuntimeNProcessOutputHappyPath(t *testing.T) {
	t.Parallel()

	b := &runtimeNBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	result := runtimeNResult{
		Payloads: []runtimeNPayload{{Text: "Hello from openclaw"}},
		Meta: runtimeNMeta{
			DurationMs: 1234,
			AgentMeta: map[string]any{
				"sessionId": "ses_abc",
				"usage": map[string]any{
					"input":      float64(100),
					"output":     float64(50),
					"cacheRead":  float64(10),
					"cacheWrite": float64(5),
				},
			},
		},
	}
	data, _ := json.Marshal(result)

	res := b.processOutput(strings.NewReader(string(data)), ch)

	if res.status != "completed" {
		t.Errorf("status: got %q, want %q", res.status, "completed")
	}
	if res.output != "Hello from openclaw" {
		t.Errorf("output: got %q, want %q", res.output, "Hello from openclaw")
	}
	if res.sessionID != "ses_abc" {
		t.Errorf("sessionID: got %q, want %q", res.sessionID, "ses_abc")
	}
	if res.usage.InputTokens != 100 {
		t.Errorf("input tokens: got %d, want 100", res.usage.InputTokens)
	}
	if res.usage.OutputTokens != 50 {
		t.Errorf("output tokens: got %d, want 50", res.usage.OutputTokens)
	}

	close(ch)
	var msgs []Message
	for m := range ch {
		msgs = append(msgs, m)
	}
	if len(msgs) != 1 || msgs[0].Type != MessageText {
		t.Errorf("expected 1 text message, got %d", len(msgs))
	}
	if msgs[0].Content != "Hello from openclaw" {
		t.Errorf("message content: got %q", msgs[0].Content)
	}
}

func TestRuntimeNProcessOutputMultiplePayloads(t *testing.T) {
	t.Parallel()

	b := &runtimeNBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	result := runtimeNResult{
		Payloads: []runtimeNPayload{
			{Text: "First"},
			{Text: "Second"},
		},
	}
	data, _ := json.Marshal(result)

	res := b.processOutput(strings.NewReader(string(data)), ch)

	if res.output != "FirstSecond" {
		t.Errorf("output: got %q, want %q", res.output, "FirstSecond")
	}

	close(ch)
	var msgs []Message
	for m := range ch {
		msgs = append(msgs, m)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 text messages, got %d", len(msgs))
	}
	if msgs[0].Content != "First" {
		t.Errorf("msg[0]: got %q, want %q", msgs[0].Content, "First")
	}
	if msgs[1].Content != "Second" {
		t.Errorf("msg[1]: got %q, want %q", msgs[1].Content, "Second")
	}
}

func TestRuntimeNProcessOutputEmptyPayloads(t *testing.T) {
	t.Parallel()

	b := &runtimeNBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	result := runtimeNResult{Payloads: []runtimeNPayload{}}
	data, _ := json.Marshal(result)

	res := b.processOutput(strings.NewReader(string(data)), ch)

	if res.status != "completed" {
		t.Errorf("status: got %q, want %q", res.status, "completed")
	}
	if res.output != "" {
		t.Errorf("output: got %q, want empty", res.output)
	}

	close(ch)
	var msgs []Message
	for m := range ch {
		msgs = append(msgs, m)
	}
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages, got %d", len(msgs))
	}
}

func TestRuntimeNProcessOutputWithLeadingLogLines(t *testing.T) {
	t.Parallel()

	b := &runtimeNBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	result := runtimeNResult{
		Payloads: []runtimeNPayload{{Text: "Done"}},
	}
	data, _ := json.Marshal(result)
	input := "some log line\nanother log\n" + string(data)

	res := b.processOutput(strings.NewReader(input), ch)

	if res.status != "completed" {
		t.Errorf("status: got %q, want %q", res.status, "completed")
	}
	if res.output != "Done" {
		t.Errorf("output: got %q, want %q", res.output, "Done")
	}

	close(ch)
}

func TestRuntimeNProcessOutputIgnoresTrailingLogLinesAfterJSON(t *testing.T) {
	t.Parallel()

	b := &runtimeNBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	result := runtimeNResult{
		Payloads: []runtimeNPayload{{Text: "Done"}},
	}
	data, _ := json.Marshal(result)
	input := string(data) + "\npost-result log line that should not block parsing"

	res := b.processOutput(strings.NewReader(input), ch)

	if res.status != "completed" {
		t.Errorf("status: got %q, want %q", res.status, "completed")
	}
	if res.output != "Done" {
		t.Errorf("output: got %q, want %q", res.output, "Done")
	}

	close(ch)
}

func TestRuntimeNProcessOutputNoJSON(t *testing.T) {
	t.Parallel()

	b := &runtimeNBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	res := b.processOutput(strings.NewReader("not json at all"), ch)

	if res.status != "completed" {
		t.Errorf("status: got %q, want %q", res.status, "completed")
	}
	if res.output != "not json at all" {
		t.Errorf("output: got %q", res.output)
	}

	close(ch)
}

func TestRuntimeNProcessOutputEmptyInput(t *testing.T) {
	t.Parallel()

	b := &runtimeNBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	res := b.processOutput(strings.NewReader(""), ch)

	if res.status != "failed" {
		t.Errorf("status: got %q, want %q", res.status, "failed")
	}
	if res.errMsg != "openclaw returned no parseable output" {
		t.Errorf("errMsg: got %q", res.errMsg)
	}

	close(ch)
}

func TestRuntimeNProcessOutputReadError(t *testing.T) {
	t.Parallel()

	b := &runtimeNBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	res := b.processOutput(&ioErrReader{data: ""}, ch)

	if res.status != "failed" {
		t.Errorf("status: got %q, want %q", res.status, "failed")
	}
	if !strings.Contains(res.errMsg, "read stdout") {
		t.Errorf("errMsg: got %q, want it to contain 'read stdout'", res.errMsg)
	}

	close(ch)
}

func TestRuntimeNProcessOutputWithBracesInLogLines(t *testing.T) {
	t.Parallel()

	b := &runtimeNBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	result := runtimeNResult{
		Payloads: []runtimeNPayload{{Text: "Final answer"}},
		Meta:     runtimeNMeta{DurationMs: 500},
	}
	data, _ := json.Marshal(result)

	input := `[tools] exec failed: complex interpreter invocation detected. raw_params={"command":"echo hello"}` + "\n" + string(data)

	res := b.processOutput(strings.NewReader(input), ch)

	if res.status != "completed" {
		t.Errorf("status: got %q, want %q", res.status, "completed")
	}
	if res.output != "Final answer" {
		t.Errorf("output: got %q, want %q", res.output, "Final answer")
	}

	close(ch)
}

func TestRuntimeNResultBlobWithLeadingPrefixRejected(t *testing.T) {
	t.Parallel()

	b := &runtimeNBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	result := runtimeNResult{
		Payloads: []runtimeNPayload{{Text: "Should not match"}},
		Meta:     runtimeNMeta{DurationMs: 500},
	}
	data, _ := json.Marshal(result)
	input := "some prefix " + string(data)

	res := b.processOutput(strings.NewReader(input), ch)

	if res.status != "completed" {
		t.Errorf("status: got %q, want %q", res.status, "completed")
	}
	if res.output != input {
		t.Errorf("output: got %q, want raw input back", res.output)
	}

	close(ch)
}

func TestRuntimeNStreamingTextEvents(t *testing.T) {
	t.Parallel()

	b := &runtimeNBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := []string{
		`{"type":"text","text":"Hello "}`,
		`{"type":"text","text":"world"}`,
	}
	input := strings.Join(lines, "\n")

	res := b.processOutput(strings.NewReader(input), ch)

	if res.status != "completed" {
		t.Errorf("status: got %q, want %q", res.status, "completed")
	}
	if res.output != "Hello world" {
		t.Errorf("output: got %q, want %q", res.output, "Hello world")
	}

	close(ch)
	var msgs []Message
	for m := range ch {
		msgs = append(msgs, m)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Type != MessageText || msgs[0].Content != "Hello " {
		t.Errorf("msg[0]: type=%s content=%q", msgs[0].Type, msgs[0].Content)
	}
	if msgs[1].Type != MessageText || msgs[1].Content != "world" {
		t.Errorf("msg[1]: type=%s content=%q", msgs[1].Type, msgs[1].Content)
	}
}

func TestRuntimeNStreamingToolUseEvents(t *testing.T) {
	t.Parallel()

	b := &runtimeNBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := []string{
		`{"type":"tool_use","tool":"bash","callId":"call_1","input":{"command":"ls -la"}}`,
		`{"type":"tool_result","tool":"bash","callId":"call_1","text":"total 42\ndrwxr-xr-x"}`,
		`{"type":"text","text":"Listed files."}`,
	}
	input := strings.Join(lines, "\n")

	res := b.processOutput(strings.NewReader(input), ch)

	if res.status != "completed" {
		t.Errorf("status: got %q, want %q", res.status, "completed")
	}

	close(ch)
	var msgs []Message
	for m := range ch {
		msgs = append(msgs, m)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}

	if msgs[0].Type != MessageToolUse {
		t.Errorf("msg[0] type: got %s, want tool-use", msgs[0].Type)
	}
	if msgs[0].Tool != "bash" {
		t.Errorf("msg[0] tool: got %q, want %q", msgs[0].Tool, "bash")
	}
	if msgs[0].CallID != "call_1" {
		t.Errorf("msg[0] callID: got %q, want %q", msgs[0].CallID, "call_1")
	}
	if msgs[0].Input["command"] != "ls -la" {
		t.Errorf("msg[0] input: got %v", msgs[0].Input)
	}

	if msgs[1].Type != MessageToolResult {
		t.Errorf("msg[1] type: got %s, want tool-result", msgs[1].Type)
	}
	if msgs[1].CallID != "call_1" {
		t.Errorf("msg[1] callID: got %q", msgs[1].CallID)
	}
	if msgs[1].Output != "total 42\ndrwxr-xr-x" {
		t.Errorf("msg[1] output: got %q", msgs[1].Output)
	}

	if msgs[2].Type != MessageText || msgs[2].Content != "Listed files." {
		t.Errorf("msg[2]: type=%s content=%q", msgs[2].Type, msgs[2].Content)
	}
}

func TestRuntimeNStreamingErrorEvent(t *testing.T) {
	t.Parallel()

	b := &runtimeNBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := []string{
		`{"type":"text","text":"Starting..."}`,
		`{"type":"error","text":"model not found: gpt-99"}`,
	}
	input := strings.Join(lines, "\n")

	res := b.processOutput(strings.NewReader(input), ch)

	if res.status != "failed" {
		t.Errorf("status: got %q, want %q", res.status, "failed")
	}
	if res.errMsg != "model not found: gpt-99" {
		t.Errorf("errMsg: got %q", res.errMsg)
	}

	close(ch)
	var msgs []Message
	for m := range ch {
		msgs = append(msgs, m)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[1].Type != MessageError {
		t.Errorf("msg[1] type: got %s, want error", msgs[1].Type)
	}
}

func TestRuntimeNStreamingStepFinishUsage(t *testing.T) {
	t.Parallel()

	b := &runtimeNBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := []string{
		`{"type":"step_start"}`,
		`{"type":"text","text":"Done"}`,
		`{"type":"step_finish","usage":{"input":200,"output":100,"cacheRead":50,"cacheWrite":25}}`,
	}
	input := strings.Join(lines, "\n")

	res := b.processOutput(strings.NewReader(input), ch)

	if res.usage.InputTokens != 200 {
		t.Errorf("input tokens: got %d, want 200", res.usage.InputTokens)
	}
	if res.usage.OutputTokens != 100 {
		t.Errorf("output tokens: got %d, want 100", res.usage.OutputTokens)
	}
	if res.usage.CacheReadTokens != 50 {
		t.Errorf("cache read: got %d, want 50", res.usage.CacheReadTokens)
	}
	if res.usage.CacheWriteTokens != 25 {
		t.Errorf("cache write: got %d, want 25", res.usage.CacheWriteTokens)
	}

	close(ch)
}

func TestRuntimeNStreamingSessionID(t *testing.T) {
	t.Parallel()

	b := &runtimeNBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := []string{
		`{"type":"text","text":"Hi","sessionId":"ses_stream_123"}`,
	}
	input := strings.Join(lines, "\n")

	res := b.processOutput(strings.NewReader(input), ch)

	if res.sessionID != "ses_stream_123" {
		t.Errorf("sessionID: got %q, want %q", res.sessionID, "ses_stream_123")
	}

	close(ch)
}

func TestRuntimeNStreamingMixedWithLogLines(t *testing.T) {
	t.Parallel()

	b := &runtimeNBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := []string{
		"[info] initializing agent...",
		`{"type":"text","text":"Hello"}`,
		"[debug] tool exec completed",
		`{"type":"text","text":" world"}`,
	}
	input := strings.Join(lines, "\n")

	res := b.processOutput(strings.NewReader(input), ch)

	if res.status != "completed" {
		t.Errorf("status: got %q, want %q", res.status, "completed")
	}
	if res.output != "Hello world" {
		t.Errorf("output: got %q, want %q", res.output, "Hello world")
	}

	close(ch)
	var msgs []Message
	for m := range ch {
		msgs = append(msgs, m)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 text messages, got %d", len(msgs))
	}
}

func TestRuntimeNLifecycleErrorPhase(t *testing.T) {
	t.Parallel()

	b := &runtimeNBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := []string{
		`{"type":"text","text":"Working..."}`,
		`{"type":"lifecycle","phase":"error","text":"agent crashed unexpectedly"}`,
	}
	input := strings.Join(lines, "\n")

	res := b.processOutput(strings.NewReader(input), ch)

	if res.status != "failed" {
		t.Errorf("status: got %q, want %q", res.status, "failed")
	}
	if res.errMsg != "agent crashed unexpectedly" {
		t.Errorf("errMsg: got %q", res.errMsg)
	}

	close(ch)
	var msgs []Message
	for m := range ch {
		msgs = append(msgs, m)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[1].Type != MessageError {
		t.Errorf("msg[1] type: got %s, want error", msgs[1].Type)
	}
}

func TestRuntimeNLifecycleFailedPhase(t *testing.T) {
	t.Parallel()

	b := &runtimeNBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := []string{
		`{"type":"lifecycle","phase":"failed","message":"timeout exceeded"}`,
	}
	input := strings.Join(lines, "\n")

	res := b.processOutput(strings.NewReader(input), ch)

	if res.status != "failed" {
		t.Errorf("status: got %q, want %q", res.status, "failed")
	}
	if res.errMsg != "timeout exceeded" {
		t.Errorf("errMsg: got %q, want %q", res.errMsg, "timeout exceeded")
	}

	close(ch)
}

func TestRuntimeNLifecycleCancelledPhase(t *testing.T) {
	t.Parallel()

	b := &runtimeNBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := []string{
		`{"type":"lifecycle","phase":"cancelled"}`,
	}
	input := strings.Join(lines, "\n")

	res := b.processOutput(strings.NewReader(input), ch)

	if res.status != "failed" {
		t.Errorf("status: got %q, want %q", res.status, "failed")
	}

	if res.errMsg != "unknown openclaw error" {
		t.Errorf("errMsg: got %q", res.errMsg)
	}

	close(ch)
}

func TestRuntimeNLifecycleRunningPhaseIgnored(t *testing.T) {
	t.Parallel()

	b := &runtimeNBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := []string{
		`{"type":"lifecycle","phase":"running"}`,
		`{"type":"text","text":"Hello"}`,
	}
	input := strings.Join(lines, "\n")

	res := b.processOutput(strings.NewReader(input), ch)

	if res.status != "completed" {
		t.Errorf("status: got %q, want %q", res.status, "completed")
	}

	close(ch)
}

func TestRuntimeNStructuredErrorObject(t *testing.T) {
	t.Parallel()

	b := &runtimeNBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := []string{
		`{"type":"error","error":{"name":"ModelNotFoundError","data":{"message":"model gpt-99 not available"}}}`,
	}
	input := strings.Join(lines, "\n")

	res := b.processOutput(strings.NewReader(input), ch)

	if res.status != "failed" {
		t.Errorf("status: got %q, want %q", res.status, "failed")
	}
	if res.errMsg != "model gpt-99 not available" {
		t.Errorf("errMsg: got %q, want %q", res.errMsg, "model gpt-99 not available")
	}

	close(ch)
}

func TestRuntimeNStructuredErrorNameOnly(t *testing.T) {
	t.Parallel()

	b := &runtimeNBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := []string{
		`{"type":"error","error":{"name":"AuthenticationError"}}`,
	}
	input := strings.Join(lines, "\n")

	res := b.processOutput(strings.NewReader(input), ch)

	if res.errMsg != "AuthenticationError" {
		t.Errorf("errMsg: got %q, want %q", res.errMsg, "AuthenticationError")
	}

	close(ch)
}

func TestRuntimeNStructuredErrorMessageField(t *testing.T) {
	t.Parallel()

	b := &runtimeNBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := []string{
		`{"type":"error","error":{"message":"rate limit exceeded"}}`,
	}
	input := strings.Join(lines, "\n")

	res := b.processOutput(strings.NewReader(input), ch)

	if res.errMsg != "rate limit exceeded" {
		t.Errorf("errMsg: got %q, want %q", res.errMsg, "rate limit exceeded")
	}

	close(ch)
}

func TestRuntimeNUsageAlternativeFieldNames(t *testing.T) {
	t.Parallel()

	data := map[string]any{
		"inputTokens":       float64(500),
		"outputTokens":      float64(200),
		"cachedInputTokens": float64(100),
	}
	usage := parseRuntimeNUsage(data)

	if usage.InputTokens != 500 {
		t.Errorf("InputTokens: got %d, want 500", usage.InputTokens)
	}
	if usage.OutputTokens != 200 {
		t.Errorf("OutputTokens: got %d, want 200", usage.OutputTokens)
	}
	if usage.CacheReadTokens != 100 {
		t.Errorf("CacheReadTokens: got %d, want 100", usage.CacheReadTokens)
	}
}

func TestRuntimeNUsageSnakeCaseFieldNames(t *testing.T) {
	t.Parallel()

	data := map[string]any{
		"input_tokens":                float64(300),
		"output_tokens":               float64(150),
		"cache_read_input_tokens":     float64(80),
		"cache_creation_input_tokens": float64(40),
	}
	usage := parseRuntimeNUsage(data)

	if usage.InputTokens != 300 {
		t.Errorf("InputTokens: got %d, want 300", usage.InputTokens)
	}
	if usage.OutputTokens != 150 {
		t.Errorf("OutputTokens: got %d, want 150", usage.OutputTokens)
	}
	if usage.CacheReadTokens != 80 {
		t.Errorf("CacheReadTokens: got %d, want 80", usage.CacheReadTokens)
	}
	if usage.CacheWriteTokens != 40 {
		t.Errorf("CacheWriteTokens: got %d, want 40", usage.CacheWriteTokens)
	}
}

func TestRuntimeNUsageOriginalFieldNames(t *testing.T) {
	t.Parallel()

	data := map[string]any{
		"input":      float64(100),
		"output":     float64(50),
		"cacheRead":  float64(10),
		"cacheWrite": float64(5),
	}
	usage := parseRuntimeNUsage(data)

	if usage.InputTokens != 100 {
		t.Errorf("InputTokens: got %d, want 100", usage.InputTokens)
	}
	if usage.OutputTokens != 50 {
		t.Errorf("OutputTokens: got %d, want 50", usage.OutputTokens)
	}
	if usage.CacheReadTokens != 10 {
		t.Errorf("CacheReadTokens: got %d, want 10", usage.CacheReadTokens)
	}
	if usage.CacheWriteTokens != 5 {
		t.Errorf("CacheWriteTokens: got %d, want 5", usage.CacheWriteTokens)
	}
}

func TestRuntimeNUsageAccumulationAcrossSteps(t *testing.T) {
	t.Parallel()

	b := &runtimeNBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	lines := []string{
		`{"type":"step_finish","usage":{"inputTokens":100,"outputTokens":50}}`,
		`{"type":"step_finish","usage":{"inputTokens":200,"outputTokens":80,"cachedInputTokens":60}}`,
	}
	input := strings.Join(lines, "\n")

	res := b.processOutput(strings.NewReader(input), ch)

	if res.usage.InputTokens != 300 {
		t.Errorf("InputTokens: got %d, want 300", res.usage.InputTokens)
	}
	if res.usage.OutputTokens != 130 {
		t.Errorf("OutputTokens: got %d, want 130", res.usage.OutputTokens)
	}
	if res.usage.CacheReadTokens != 60 {
		t.Errorf("CacheReadTokens: got %d, want 60", res.usage.CacheReadTokens)
	}

	close(ch)
}

func TestRuntimeNUsageFinalResultAlternativeFields(t *testing.T) {
	t.Parallel()

	b := &runtimeNBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	result := runtimeNResult{
		Payloads: []runtimeNPayload{{Text: "Done"}},
		Meta: runtimeNMeta{
			DurationMs: 1000,
			AgentMeta: map[string]any{
				"usage": map[string]any{
					"inputTokens":       float64(400),
					"outputTokens":      float64(180),
					"cachedInputTokens": float64(90),
				},
			},
		},
	}
	data, _ := json.Marshal(result)

	res := b.processOutput(strings.NewReader(string(data)), ch)

	if res.usage.InputTokens != 400 {
		t.Errorf("InputTokens: got %d, want 400", res.usage.InputTokens)
	}
	if res.usage.OutputTokens != 180 {
		t.Errorf("OutputTokens: got %d, want 180", res.usage.OutputTokens)
	}
	if res.usage.CacheReadTokens != 90 {
		t.Errorf("CacheReadTokens: got %d, want 90", res.usage.CacheReadTokens)
	}

	close(ch)
}

func TestRuntimeNProcessOutputMultilineJSON(t *testing.T) {
	t.Parallel()

	b := &runtimeNBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	result := runtimeNResult{
		Payloads: []runtimeNPayload{{Text: "Pretty printed response"}},
		Meta: runtimeNMeta{
			DurationMs: 4764,
			AgentMeta: map[string]any{
				"sessionId": "test-session",
				"usage": map[string]any{
					"input":  float64(100),
					"output": float64(34),
				},
			},
		},
	}

	data, _ := json.MarshalIndent(result, "", "  ")

	res := b.processOutput(strings.NewReader(string(data)), ch)

	if res.status != "completed" {
		t.Errorf("status: got %q, want %q", res.status, "completed")
	}
	if res.output != "Pretty printed response" {
		t.Errorf("output: got %q, want %q", res.output, "Pretty printed response")
	}
	if res.sessionID != "test-session" {
		t.Errorf("sessionID: got %q, want %q", res.sessionID, "test-session")
	}

	close(ch)
	var msgs []Message
	for m := range ch {
		msgs = append(msgs, m)
	}
	if len(msgs) != 1 || msgs[0].Content != "Pretty printed response" {
		t.Errorf("expected 1 text message with content, got %d msgs", len(msgs))
	}
}

func TestRuntimeNProcessOutputMultilineJSONWithLeadingLogs(t *testing.T) {
	t.Parallel()

	b := &runtimeNBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	result := runtimeNResult{
		Payloads: []runtimeNPayload{{Text: "Answer after logs"}},
		Meta:     runtimeNMeta{DurationMs: 100},
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	input := "some startup log\nanother log line\n" + string(data)

	res := b.processOutput(strings.NewReader(input), ch)

	if res.status != "completed" {
		t.Errorf("status: got %q, want %q", res.status, "completed")
	}
	if res.output != "Answer after logs" {
		t.Errorf("output: got %q, want %q", res.output, "Answer after logs")
	}

	close(ch)
}

func TestRuntimeNInt64Float(t *testing.T) {
	t.Parallel()
	data := map[string]any{"count": float64(42)}
	if got := runtimeNInt64(data, "count"); got != 42 {
		t.Errorf("got %d, want 42", got)
	}
}

func TestRuntimeNInt64Missing(t *testing.T) {
	t.Parallel()
	data := map[string]any{}
	if got := runtimeNInt64(data, "count"); got != 0 {
		t.Errorf("got %d, want 0", got)
	}
}

func TestRuntimeNInt64Nil(t *testing.T) {
	t.Parallel()
	data := map[string]any{"count": "not a number"}
	if got := runtimeNInt64(data, "count"); got != 0 {
		t.Errorf("got %d, want 0", got)
	}
}

func indexOf(args []string, s string) int {
	for i, a := range args {
		if a == s {
			return i
		}
	}
	return -1
}

func TestBuildRuntimeNArgsMinimal(t *testing.T) {
	t.Parallel()

	args := buildRuntimeNArgs("do work", "ses-1", ExecOptions{}, slog.Default())
	expected := []string{"agent", "--local", "--json", "--session-id", "ses-1", "--message", "do work"}

	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(args), args)
	}
	for i, want := range expected {
		if args[i] != want {
			t.Errorf("args[%d] = %q, want %q", i, args[i], want)
		}
	}
}

func TestBuildRuntimeNArgsMapsModelToAgent(t *testing.T) {
	t.Parallel()

	args := buildRuntimeNArgs("task", "ses-2", ExecOptions{
		Model:        "deepseek-v4-agent",
		SystemPrompt: "You are a helpful agent.",
	}, slog.Default())

	if idx := indexOf(args, "--model"); idx != -1 {
		t.Fatalf("unexpected --model flag at %d: %v", idx, args)
	}
	if idx := indexOf(args, "--system-prompt"); idx != -1 {
		t.Fatalf("unexpected --system-prompt flag at %d: %v", idx, args)
	}

	agentIdx := indexOf(args, "--agent")
	if agentIdx == -1 || agentIdx+1 >= len(args) {
		t.Fatalf("expected --agent <value> in args: %v", args)
	}
	if got := args[agentIdx+1]; got != "deepseek-v4-agent" {
		t.Errorf("--agent value = %q, want %q", got, "deepseek-v4-agent")
	}
}

func TestBuildRuntimeNArgsCustomAgentWinsOverModel(t *testing.T) {
	t.Parallel()

	args := buildRuntimeNArgs("task", "ses-2b", ExecOptions{
		Model:      "from-dropdown",
		CustomArgs: []string{"--agent", "from-custom-args"},
	}, slog.Default())

	count := 0
	for _, a := range args {
		if a == "--agent" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one --agent flag, got %d: %v", count, args)
	}
	agentIdx := indexOf(args, "--agent")
	if args[agentIdx+1] != "from-custom-args" {
		t.Errorf("custom --agent should win, got %q", args[agentIdx+1])
	}
}

func TestBuildRuntimeNArgsPrependsSystemPromptToMessage(t *testing.T) {
	t.Parallel()

	args := buildRuntimeNArgs("do the thing", "ses-3", ExecOptions{
		SystemPrompt: "You are a read-only agent.",
	}, slog.Default())

	msgIdx := indexOf(args, "--message")
	if msgIdx == -1 || msgIdx+1 >= len(args) {
		t.Fatalf("expected --message <value> in args: %v", args)
	}
	got := args[msgIdx+1]
	want := "You are a read-only agent.\n\ndo the thing"
	if got != want {
		t.Errorf("--message payload mismatch:\n got:  %q\n want: %q", got, want)
	}
}

func TestBuildRuntimeNArgsEmptySystemPromptLeavesMessageUnchanged(t *testing.T) {
	t.Parallel()

	args := buildRuntimeNArgs("just do it", "ses-4", ExecOptions{}, slog.Default())

	msgIdx := indexOf(args, "--message")
	if msgIdx == -1 || msgIdx+1 >= len(args) {
		t.Fatalf("expected --message <value> in args: %v", args)
	}
	if got := args[msgIdx+1]; got != "just do it" {
		t.Errorf("--message payload: got %q, want %q", got, "just do it")
	}
}

func TestBuildRuntimeNArgsTimeout(t *testing.T) {
	t.Parallel()

	args := buildRuntimeNArgs("task", "ses-5", ExecOptions{
		Timeout: 90 * time.Second,
	}, slog.Default())

	idx := indexOf(args, "--timeout")
	if idx == -1 || idx+1 >= len(args) {
		t.Fatalf("expected --timeout <value> in args: %v", args)
	}
	if got := args[idx+1]; got != "90" {
		t.Errorf("--timeout value: got %q, want %q", got, "90")
	}
}

func TestBuildRuntimeNArgsFiltersBlockedCustomArgs(t *testing.T) {
	t.Parallel()

	args := buildRuntimeNArgs("task", "ses-6", ExecOptions{
		CustomArgs: []string{
			"--agent", "research-bot",
			"--model", "gpt-4o",
			"--system-prompt", "You are helpful",
			"--session-id", "hijacked",
			"--message", "hijacked",
		},
	}, slog.Default())

	if idx := indexOf(args, "--model"); idx != -1 {
		t.Errorf("--model should be filtered from custom_args: %v", args)
	}
	if idx := indexOf(args, "--system-prompt"); idx != -1 {
		t.Errorf("--system-prompt should be filtered from custom_args: %v", args)
	}

	if idx := indexOf(args, "--agent"); idx == -1 || idx+1 >= len(args) || args[idx+1] != "research-bot" {
		t.Errorf("expected --agent research-bot to survive filtering: %v", args)
	}

	if count := countOccurrences(args, "--session-id"); count != 1 {
		t.Errorf("expected 1 --session-id (daemon-managed), got %d: %v", count, args)
	}
	if count := countOccurrences(args, "--message"); count != 1 {
		t.Errorf("expected 1 --message (daemon-managed), got %d: %v", count, args)
	}
}

func TestBuildRuntimeNArgsLocalModeIsDefault(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"", "local"} {
		args := buildRuntimeNArgs("do work", "ses-local", ExecOptions{
			RuntimeNMode: mode,
		}, slog.Default())
		if idx := indexOf(args, "--local"); idx == -1 {
			t.Errorf("mode=%q: expected --local in args, got %v", mode, args)
		}
	}
}

func TestBuildRuntimeNArgsGatewayModeDropsLocal(t *testing.T) {
	t.Parallel()

	args := buildRuntimeNArgs("do work", "ses-gw", ExecOptions{
		RuntimeNMode: "gateway",
	}, slog.Default())

	if idx := indexOf(args, "--local"); idx != -1 {
		t.Errorf("gateway mode must not append --local, got %v", args)
	}

	for _, want := range []string{"agent", "--json", "--session-id", "--message"} {
		if idx := indexOf(args, want); idx == -1 {
			t.Errorf("gateway mode dropped daemon-managed flag %q: %v", want, args)
		}
	}
}

func TestBuildRuntimeNArgsGatewayModeStillFiltersLocalFromCustomArgs(t *testing.T) {
	t.Parallel()

	args := buildRuntimeNArgs("do work", "ses-mix", ExecOptions{
		RuntimeNMode: "gateway",
		CustomArgs:   []string{"--local"},
	}, slog.Default())

	if idx := indexOf(args, "--local"); idx != -1 {
		t.Errorf("gateway mode must filter custom_args --local, got %v", args)
	}
}

func TestRuntimeNProcessOutputExtractsModelFromAgentMeta(t *testing.T) {
	t.Parallel()

	b := &runtimeNBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	result := runtimeNResult{
		Payloads: []runtimeNPayload{{Text: "ok"}},
		Meta: runtimeNMeta{
			DurationMs: 9501,
			AgentMeta: map[string]any{
				"sessionId": "goosar-1776752018613706000",
				"provider":  "deepseek",
				"model":     "deepseek-chat",
				"usage": map[string]any{
					"input":      float64(414),
					"output":     float64(163),
					"cacheRead":  float64(33280),
					"cacheWrite": float64(0),
				},
			},
		},
	}
	data, _ := json.Marshal(result)

	res := b.processOutput(strings.NewReader(string(data)), ch)

	if res.model != "deepseek-chat" {
		t.Errorf("model: got %q, want %q", res.model, "deepseek-chat")
	}
	if res.sessionID != "goosar-1776752018613706000" {
		t.Errorf("sessionID: got %q", res.sessionID)
	}
	if res.usage.InputTokens != 414 {
		t.Errorf("input tokens: got %d, want 414", res.usage.InputTokens)
	}
}

func TestRuntimeNProcessOutputModelEmptyWhenAgentMetaOmitsIt(t *testing.T) {
	t.Parallel()

	b := &runtimeNBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	result := runtimeNResult{
		Payloads: []runtimeNPayload{{Text: "ok"}},
		Meta: runtimeNMeta{
			AgentMeta: map[string]any{
				"sessionId": "ses_xyz",
				"usage": map[string]any{
					"input":  float64(10),
					"output": float64(5),
				},
			},
		},
	}
	data, _ := json.Marshal(result)

	res := b.processOutput(strings.NewReader(string(data)), ch)

	if res.model != "" {
		t.Errorf("model: got %q, want empty", res.model)
	}
}

func TestRuntimeNProcessOutputWholeBufferPrettyJSON(t *testing.T) {
	t.Parallel()

	b := &runtimeNBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	input := `{
  "payloads": [
    {
      "text": "Pretty printed answer line 1.\n"
    },
    {
      "text": "Pretty printed answer line 2."
    }
  ],
  "meta": {
    "durationMs": 9501,
    "agentMeta": {
      "sessionId": "ses_pretty_printed",
      "provider": "openrouter",
      "model": "anthropic/claude-opus-4.7",
      "usage": {
        "input": 414,
        "output": 163,
        "cacheRead": 33280,
        "cacheWrite": 0
      }
    }
  }
}
`

	res := b.processOutput(strings.NewReader(input), ch)

	if res.status != "completed" {
		t.Errorf("status: got %q, want %q", res.status, "completed")
	}
	if res.errMsg != "" {
		t.Errorf("errMsg: got %q, want empty", res.errMsg)
	}
	wantOutput := "Pretty printed answer line 1.\nPretty printed answer line 2."
	if res.output != wantOutput {
		t.Errorf("output: got %q, want %q", res.output, wantOutput)
	}
	if res.sessionID != "ses_pretty_printed" {
		t.Errorf("sessionID: got %q, want %q", res.sessionID, "ses_pretty_printed")
	}
	if res.model != "anthropic/claude-opus-4.7" {
		t.Errorf("model: got %q, want %q", res.model, "anthropic/claude-opus-4.7")
	}
	if res.usage.InputTokens != 414 {
		t.Errorf("input tokens: got %d, want 414", res.usage.InputTokens)
	}
	if res.usage.OutputTokens != 163 {
		t.Errorf("output tokens: got %d, want 163", res.usage.OutputTokens)
	}
	if res.usage.CacheReadTokens != 33280 {
		t.Errorf("cache read: got %d, want 33280", res.usage.CacheReadTokens)
	}

	close(ch)
	var msgs []Message
	for m := range ch {
		msgs = append(msgs, m)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 text messages, got %d", len(msgs))
	}
}

func TestRuntimeNProcessOutputDeeplyIndentedFixture(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("testdata/runtime-n-2026.5.5-stdout.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	if !strings.Contains(string(data), "\n  ") {
		t.Fatalf("fixture is not pretty-printed; this test must run against multi-line JSON")
	}

	result, ok := parseWholeBufferRuntimeNResult(data)
	if !ok {
		t.Fatalf("parseWholeBufferOpenclawResult failed; the whole-buffer fast path is broken")
	}
	if result.Payloads == nil {
		t.Errorf("expected payloads to populate from whole-buffer parse")
	}
	if result.Meta.DurationMs == 0 {
		t.Errorf("expected meta.durationMs to populate from whole-buffer parse")
	}
}

func TestRuntimeNProcessOutputEmptyBufferCanonicalError(t *testing.T) {
	t.Parallel()

	b := &runtimeNBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	res := b.processOutput(strings.NewReader(""), ch)

	if res.status != "failed" {
		t.Errorf("status: got %q, want %q", res.status, "failed")
	}
	if res.errMsg != "openclaw returned no parseable output" {
		t.Errorf("errMsg: got %q, want canonical empty-buffer message", res.errMsg)
	}

	close(ch)
}

func countOccurrences(args []string, s string) int {
	n := 0
	for _, a := range args {
		if a == s {
			n++
		}
	}
	return n
}

func TestRuntimeNProcessOutputStdoutFixture(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("testdata/runtime-n-2026.5.5-stdout.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if len(data) < 1000 {
		t.Fatalf("fixture too small (%d bytes); did the file get truncated?", len(data))
	}

	b := &runtimeNBackend{cfg: Config{Logger: slog.Default()}}
	ch := make(chan Message, 256)

	res := b.processOutput(strings.NewReader(string(data)), ch)

	if res.status != "completed" {
		t.Errorf("status: got %q, want %q", res.status, "completed")
	}
	if res.errMsg != "" {
		t.Errorf("errMsg: got %q, want empty", res.errMsg)
	}
	if res.output != "hi" {
		t.Errorf("output: got %q, want %q", res.output, "hi")
	}
	if res.sessionID == "" {
		t.Errorf("sessionID: got empty, want non-empty")
	}
	if res.model != "anthropic/claude-opus-4.7" {
		t.Errorf("model: got %q, want %q", res.model, "anthropic/claude-opus-4.7")
	}
	if res.usage.InputTokens != 34620 {
		t.Errorf("usage.InputTokens: got %d, want %d", res.usage.InputTokens, 34620)
	}
	if res.usage.OutputTokens != 6 {
		t.Errorf("usage.OutputTokens: got %d, want %d", res.usage.OutputTokens, 6)
	}
	if res.usage.CacheWriteTokens != 46482 {
		t.Errorf("usage.CacheWriteTokens: got %d, want %d", res.usage.CacheWriteTokens, 46482)
	}

	close(ch)

	var gotText bool
	for сообщение := range ch {
		if сообщение.Type == MessageText && strings.Contains(сообщение.Content, "hi") {
			gotText = true
		}
	}
	if !gotText {
		t.Errorf("expected a MessageText event containing %q", "hi")
	}
}

func TestParseRuntimeNVersion(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{"bare", "2026.5.5", "2026.5.5", true},
		{"with prefix", "openclaw 2026.5.5", "2026.5.5", true},
		{"with v prefix", "openclaw v2026.5.5", "2026.5.5", true},
		{"with commit suffix", "openclaw 2026.5.5 c37871e", "2026.5.5", true},
		{"trailing newline", "openclaw 2026.5.5\n", "2026.5.5", true},
		{"two segments rejected", "openclaw 2026.5", "", false},
		{"no version at all", "openclaw build info", "", false},
		{"empty", "", "", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := parseRuntimeNVersion(c.in)
			if ok != c.ok {
				t.Fatalf("ok = %v, want %v (input=%q)", ok, c.ok, c.in)
			}
			if got != c.want {
				t.Errorf("got %q, want %q (input=%q)", got, c.want, c.in)
			}
		})
	}
}

func TestCompareRuntimeNVersion(t *testing.T) {
	t.Parallel()

	cases := []struct {
		a, b string
		want int
	}{
		{"2026.5.5", "2026.5.5", 0},
		{"2026.5.4", "2026.5.5", -1},
		{"2026.5.6", "2026.5.5", 1},
		{"2026.4.99", "2026.5.0", -1},
		{"2027.0.0", "2026.99.99", 1},
		{"0.0.0", "2026.5.5", -1},
	}

	for _, c := range cases {
		got := compareRuntimeNVersion(c.a, c.b)
		if got != c.want {
			t.Errorf("compareOpenclawVersion(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestRuntimeNExecuteRejectsOldVersion(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakePath := filepath.Join(t.TempDir(), "openclaw")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--version\" ]; then\n" +
		"  echo 'openclaw 2026.4.9 abc123'\n" +
		"  exit 0\n" +
		"fi\n" +
		"echo 'fake openclaw should not have been invoked' >&2\n" +
		"exit 99\n"
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("runtime-n", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new openclaw backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = backend.Execute(ctx, "prompt-ignored", ExecOptions{Timeout: 5 * time.Second})
	if err == nil {
		t.Fatal("expected Execute to return a version error, got nil")
	}
	сообщение := err.Error()
	for _, want := range []string{"2026.4.9", "2026.5.5", "openclaw update"} {
		if !strings.Contains(сообщение, want) {
			t.Errorf("error message missing %q: %s", want, сообщение)
		}
	}
}

func TestRuntimeNExecuteAllowsCurrentVersion(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	fakePath := filepath.Join(t.TempDir(), "openclaw")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--version\" ]; then\n" +
		"  echo 'openclaw 2026.5.5 c37871e'\n" +
		"  exit 0\n" +
		"fi\n" +
		"exit 0\n"
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("runtime-n", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new openclaw backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Execute returned synchronous error past the version gate: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	select {
	case result := <-session.Result:
		if strings.Contains(result.Error, "openclaw update") {
			t.Errorf("version gate fired for a current version: %q", result.Error)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}
