package agent

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func quietRuntimeALogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestBuildRuntimeAArgsBasic(t *testing.T) {
	t.Parallel()

	args := buildRuntimeAArgs(
		"hello",
		"/tmp/agy.log",
		20*time.Minute,
		ExecOptions{Cwd: "/work"},
		quietRuntimeALogger(),
	)

	want := []string{
		"-p", "hello",
		"--dangerously-skip-permissions",
		"--print-timeout", "20m0s",
		"--log-file", "/tmp/agy.log",
		"--add-dir", "/work",
	}
	if !slices.Equal(args, want) {
		t.Fatalf("buildAntigravityArgs basic mismatch\n got: %v\nwant: %v", args, want)
	}
}

func TestBuildRuntimeAArgsModel(t *testing.T) {
	t.Parallel()

	args := buildRuntimeAArgs(
		"hello",
		"/tmp/agy.log",
		20*time.Minute,
		ExecOptions{Cwd: "/work", Model: "Claude Opus 4.6 (Thinking)"},
		quietRuntimeALogger(),
	)

	want := []string{
		"-p", "hello",
		"--dangerously-skip-permissions",
		"--model", "Claude Opus 4.6 (Thinking)",
		"--print-timeout", "20m0s",
		"--log-file", "/tmp/agy.log",
		"--add-dir", "/work",
	}
	if !slices.Equal(args, want) {
		t.Fatalf("buildAntigravityArgs with model mismatch\n got: %v\nwant: %v", args, want)
	}

	bare := buildRuntimeAArgs("hi", "/tmp/agy.log", 0, ExecOptions{}, quietRuntimeALogger())
	if slices.Contains(bare, "--model") {
		t.Fatalf("--model must be omitted when opts.Model is empty; got %v", bare)
	}
}

func TestBuildRuntimeAArgsNoCapUsesLargePrintTimeout(t *testing.T) {
	t.Parallel()

	args := buildRuntimeAArgs(
		"hello",
		"/tmp/agy.log",
		0,
		ExecOptions{Cwd: "/work"},
		quietRuntimeALogger(),
	)

	want := []string{
		"-p", "hello",
		"--dangerously-skip-permissions",
		"--print-timeout", runtimeAFormatTimeout(runtimeANoCapPrintTimeout),
		"--log-file", "/tmp/agy.log",
		"--add-dir", "/work",
	}
	if !slices.Equal(args, want) {
		t.Fatalf("buildAntigravityArgs(timeout=0) mismatch\n got: %v\nwant: %v", args, want)
	}

	if runtimeANoCapPrintTimeout <= 5*time.Minute {
		t.Fatalf("antigravityNoCapPrintTimeout %s must be far larger than agy's 5m default", runtimeANoCapPrintTimeout)
	}
}

func TestRuntimeAPrintTimeoutResolvesBudget(t *testing.T) {
	t.Parallel()

	if got := runtimeAPrintTimeout(20 * time.Minute); got != 20*time.Minute {
		t.Errorf("positive cap should pass through: got %s, want 20m", got)
	}
	if got := runtimeAPrintTimeout(0); got != runtimeANoCapPrintTimeout {
		t.Errorf("zero cap should resolve to no-cap sentinel: got %s, want %s", got, runtimeANoCapPrintTimeout)
	}
	if got := runtimeAPrintTimeout(-1); got != runtimeANoCapPrintTimeout {
		t.Errorf("negative cap should resolve to no-cap sentinel: got %s, want %s", got, runtimeANoCapPrintTimeout)
	}
}

func TestRuntimeAPrintTimedOut(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	timedOut := filepath.Join(dir, "timeout.log")
	if err := os.WriteFile(timedOut, []byte(strings.Join([]string{
		`I0623 17:17:38.930400 65926 printmode.go:156] Print mode: conversation=ea49cf41-4156-425a-a2f7-4238335d4c8b, sending message`,
		`E0623 17:17:59.017212 65926 printmode.go:289] Print mode: timed out after 100 polls (printed=3)`,
	}, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	if !runtimeAPrintTimedOut(timedOut) {
		t.Error("expected the print-mode timeout marker to be detected")
	}

	clean := filepath.Join(dir, "clean.log")
	if err := os.WriteFile(clean, []byte(
		`I0623 17:17:38.930400 65926 printmode.go:156] Print mode: conversation=abc, sending message`,
	), 0o644); err != nil {
		t.Fatal(err)
	}
	if runtimeAPrintTimedOut(clean) {
		t.Error("a clean log must not be flagged as timed out")
	}

	if runtimeAPrintTimedOut("/nonexistent/path") {
		t.Error("missing log file must be treated as not-timed-out")
	}
	if runtimeAPrintTimedOut("") {
		t.Error("empty log path must be treated as not-timed-out")
	}
}

func TestRuntimeAProviderError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	providerErr := filepath.Join(dir, "provider-error.log")
	if err := os.WriteFile(providerErr, []byte(strings.Join([]string{
		`I0624 12:34:24.652899 94820 printmode.go:156] Print mode: conversation=44a57718-801c-41e7-9691-3225be2b1cb8, sending message`,
		`E0624 12:34:25.944050 94820 log.go:398] agent executor error: FAILED_PRECONDITION (code 400): User location is not supported for the API use.: FAILED_PRECONDITION (code 400): User location is not supported for the API use.`,
	}, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	got := runtimeAProviderError(providerErr)
	if !strings.Contains(got, "FAILED_PRECONDITION") || !strings.Contains(got, "User location is not supported") {
		t.Fatalf("expected provider error to be extracted, got %q", got)
	}

	authNoise := filepath.Join(dir, "auth-noise.log")
	if err := os.WriteFile(authNoise, []byte(strings.Join([]string{
		`W0624 12:34:21.518895 94820 log_context.go:117] Cache(loadCodeAssistResponse): Singleflight refresh failed: error getting token source: You are not logged into Antigravity.`,
		`I0624 12:34:24.084675 94820 printmode.go:192] Print mode: silent auth succeeded`,
	}, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := runtimeAProviderError(authNoise); got != "" {
		t.Fatalf("auth retry noise must not be treated as terminal provider error, got %q", got)
	}

	if got := runtimeAProviderError("/nonexistent/path"); got != "" {
		t.Fatalf("missing log should yield empty provider error, got %q", got)
	}
	if got := runtimeAProviderError(""); got != "" {
		t.Fatalf("empty log path should yield empty provider error, got %q", got)
	}
}

func TestBuildRuntimeAArgsResume(t *testing.T) {
	t.Parallel()

	args := buildRuntimeAArgs(
		"continue",
		"/tmp/agy.log",
		20*time.Minute,
		ExecOptions{ResumeSessionID: "b8b263a4-4b2f-4339-acc9-78b248e2b606"},
		quietRuntimeALogger(),
	)

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--conversation b8b263a4-4b2f-4339-acc9-78b248e2b606") {
		t.Fatalf("expected --conversation flag with id; got %v", args)
	}
}

func TestBuildRuntimeAArgsFiltersBlockedCustomArgs(t *testing.T) {
	t.Parallel()

	args := buildRuntimeAArgs(
		"go",
		"/tmp/agy.log",
		time.Minute,
		ExecOptions{
			ExtraArgs: []string{
				"--settings", "/tmp/biz-gate.json",
			},

			CustomArgs: []string{
				"-p", "hijacked-prompt",
				"-i",
				"--prompt-interactive",
				"--continue",
				"-c",
				"--conversation", "bad-id",
				"--model", "sneaky-model",
				"--dangerously-skip-permissions",
				"--print-timeout", "1h",
				"--log-file", "/elsewhere.log",
				"--settings=/tmp/agent-settings.json",
				"--add-dir", "/extra",
			},
		},
		quietRuntimeALogger(),
	)

	joined := strings.Join(args, " ")

	pCount := 0
	for _, a := range args {
		if a == "-p" {
			pCount++
		}
	}
	if pCount != 1 {
		t.Errorf("expected exactly one -p flag, got args=%v", args)
	}
	if strings.Contains(joined, "hijacked-prompt") {
		t.Errorf("custom -p value leaked through filter: %v", args)
	}
	if strings.Contains(joined, "-i") || strings.Contains(joined, "--prompt-interactive") {
		t.Errorf("interactive-mode flags leaked through filter: %v", args)
	}
	if strings.Contains(joined, "bad-id") {
		t.Errorf("custom --conversation value leaked through filter: %v", args)
	}
	if strings.Contains(joined, "sneaky-model") {
		t.Errorf("custom --model value leaked through filter: %v", args)
	}
	if strings.Contains(joined, "/elsewhere.log") {
		t.Errorf("custom --log-file value leaked through filter: %v", args)
	}
	if strings.Contains(joined, "--settings") || strings.Contains(joined, "biz-gate.json") || strings.Contains(joined, "agent-settings.json") {
		t.Errorf("Antigravity-incompatible --settings flag leaked through filter: %v", args)
	}
	if !strings.Contains(joined, "--add-dir /extra") {
		t.Errorf("non-blocked --add-dir flag should pass through: %v", args)
	}
}

func TestRuntimeAFormatTimeoutClampsSubSecond(t *testing.T) {
	t.Parallel()
	if got := runtimeAFormatTimeout(0); got != "1s" {
		t.Errorf("antigravityFormatTimeout(0) = %q, want 1s", got)
	}
	if got := runtimeAFormatTimeout(20 * time.Minute); got != "20m0s" {
		t.Errorf("antigravityFormatTimeout(20m) = %q, want 20m0s", got)
	}
}

func TestReadRuntimeAConversationID(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	logPath := filepath.Join(dir, "agy.log")

	logBody := strings.Join([]string{
		`I0528 13:36:19.959748 73304 printmode.go:71] Print mode: starting (promptLength=18, model="", conversationID="")`,
		`I0528 13:36:23.318877 73304 printmode.go:130] Print mode: conversation=b8b263a4-4b2f-4339-acc9-78b248e2b606, sending message`,
		`I0528 13:36:23.318892 73304 server.go:1083] Sending user message to conversation b8b263a4-4b2f-4339-acc9-78b248e2b606 (items=1, media=0)`,
	}, "\n")
	if err := os.WriteFile(logPath, []byte(logBody), 0o644); err != nil {
		t.Fatal(err)
	}

	got := readRuntimeAConversationID(logPath)
	want := "b8b263a4-4b2f-4339-acc9-78b248e2b606"
	if got != want {
		t.Fatalf("readAntigravityConversationID = %q, want %q", got, want)
	}
}

func TestReadRuntimeAConversationIDMissingFile(t *testing.T) {
	t.Parallel()
	if got := readRuntimeAConversationID("/nonexistent/path"); got != "" {
		t.Errorf("expected empty string for missing file, got %q", got)
	}
	if got := readRuntimeAConversationID(""); got != "" {
		t.Errorf("expected empty string for empty path, got %q", got)
	}
}

func fakeRuntimeAPrintTimeoutScript() string {
	return `#!/bin/sh
log=""
while [ $# -gt 0 ]; do
  case "$1" in
    --log-file) log="$2"; shift 2 ;;
    *) shift ;;
  esac
done
echo "I will run the Go unit tests to verify the build."
echo "I will wait for the Go unit tests to complete."
if [ -n "$log" ]; then
  printf 'I0623 17:17:38.930400 1 printmode.go:156] Print mode: conversation=ea49cf41-4156-425a-a2f7-4238335d4c8b, sending message\n' >> "$log"
  printf 'E0623 17:17:59.017212 1 printmode.go:289] Print mode: timed out after 100 polls (printed=2)\n' >> "$log"
fi
echo "Error: timed out waiting for response"
exit 0
`
}

func fakeRuntimeAProviderErrorScript() string {
	return `#!/bin/sh
log=""
while [ $# -gt 0 ]; do
  case "$1" in
    --log-file) log="$2"; shift 2 ;;
    *) shift ;;
  esac
done
if [ -n "$log" ]; then
  printf 'I0624 12:34:24.652899 1 printmode.go:156] Print mode: conversation=44a57718-801c-41e7-9691-3225be2b1cb8, sending message\n' >> "$log"
  printf 'E0624 12:34:25.944050 1 log.go:398] agent executor error: FAILED_PRECONDITION (code 400): User location is not supported for the API use.: FAILED_PRECONDITION (code 400): User location is not supported for the API use.\n' >> "$log"
fi
exit 0
`
}

func TestRuntimeABackendPrintTimeoutSurfacesAsTimeout(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "agy")
	writeTestExecutable(t, fakePath, []byte(fakeRuntimeAPrintTimeoutScript()))

	backend, err := New("runtime-a", Config{ExecutablePath: fakePath, Logger: quietRuntimeALogger()})
	if err != nil {
		t.Fatalf("new antigravity backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	go func() {
		for range session.Messages {
		}
	}()

	select {
	case result, ok := <-session.Result:
		if !ok {
			t.Fatal("result channel closed without a value")
		}
		if result.Status != "timeout" {
			t.Fatalf("expected status=timeout, got %q (error=%q)", result.Status, result.Error)
		}
		if !strings.Contains(result.Error, "agy --print-timeout elapsed") {
			t.Errorf("expected error to explain the agy print timeout, got %q", result.Error)
		}

		if !strings.Contains(result.Output, "I will wait for the Go unit tests to complete") {
			t.Errorf("expected partial narration to be preserved in output, got %q", result.Output)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestRuntimeABackendProviderErrorSurfacesAsFailed(t *testing.T) {
	t.Parallel()

	fakePath := filepath.Join(t.TempDir(), "agy")
	writeTestExecutable(t, fakePath, []byte(fakeRuntimeAProviderErrorScript()))

	backend, err := New("runtime-a", Config{ExecutablePath: fakePath, Logger: quietRuntimeALogger()})
	if err != nil {
		t.Fatalf("new antigravity backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	select {
	case result, ok := <-session.Result:
		if !ok {
			t.Fatal("result channel closed without a value")
		}
		if result.Status != "failed" {
			t.Fatalf("expected status=failed, got %q (error=%q)", result.Status, result.Error)
		}
		if !strings.Contains(result.Error, "User location is not supported") {
			t.Errorf("expected provider error to be surfaced, got %q", result.Error)
		}
		if result.Output != "" {
			t.Errorf("expected empty stdout to remain empty, got %q", result.Output)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestRuntimeAModelError(t *testing.T) {
	t.Parallel()

	catalog := []Model{
		{ID: "Gemini 3.5 Flash (Medium)", Label: "Gemini 3.5 Flash (Medium)", Provider: "antigravity"},
		{ID: "Claude Opus 4.6 (Thinking)", Label: "Claude Opus 4.6 (Thinking)", Provider: "antigravity"},
	}

	if err := runtimeAModelError("Claude Opus 4.6 (Thinking)", catalog); err != nil {
		t.Errorf("valid model rejected: %v", err)
	}

	if err := runtimeAModelError("", catalog); err != nil {
		t.Errorf("empty model should not error: %v", err)
	}

	if err := runtimeAModelError("anything at all", nil); err != nil {
		t.Errorf("empty catalog should fail open, got: %v", err)
	}

	err := runtimeAModelError("Totally Made Up Model", catalog)
	if err == nil {
		t.Fatal("unknown model should be rejected, not silently accepted")
	}
	if !strings.Contains(err.Error(), "Totally Made Up Model") {
		t.Errorf("error should name the rejected model: %v", err)
	}
	if !strings.Contains(err.Error(), "agy models") {
		t.Errorf("error should point the user at `agy models`: %v", err)
	}

	if err := runtimeAModelError("Claude Opus 4.6 (Thinking) ", catalog); err == nil {
		t.Error("near-miss model (trailing space) should be rejected")
	}
	if err := runtimeAModelError("Claude Opus 4.6", catalog); err == nil {
		t.Error("near-miss model (dropped suffix) should be rejected")
	}
}

func seedRuntimeATranscript(t *testing.T, appDataDir, conversationID string, records []string) {
	t.Helper()
	dir := filepath.Join(appDataDir, "brain", conversationID, ".system_generated", "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := strings.Join(records, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "transcript.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadRuntimeATranscriptOutput(t *testing.T) {
	t.Parallel()

	appDataDir := t.TempDir()
	cid := "d1637a93-20c7-4d90-8edb-7395e71280d2"

	logPath := filepath.Join(t.TempDir(), "agy.log")
	if err := os.WriteFile(logPath, []byte(strings.Join([]string{
		`I0630 14:19:40.582492 1 common.go:156] CLI app data directory: ` + appDataDir,
		`I0630 14:19:46.755801 1 printmode.go:179] Print mode: conversation=` + cid + `, sending message`,
	}, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}

	seedRuntimeATranscript(t, appDataDir, cid, []string{

		`{"type":"USER_INPUT","source":"USER_EXPLICIT","status":"DONE","step_index":0,"content":"the user's prompt — must be ignored"}`,

		`{"type":"PLANNER_RESPONSE","source":"MODEL","status":"DONE","step_index":2,"content":null}`,
		`{"type":"PLANNER_RESPONSE","source":"MODEL","status":"DONE","step_index":3,"content":"First I will read the file."}`,

		`{"type":"VIEW_FILE","source":"MODEL","status":"DONE","step_index":4}`,

		`{"type":"PLANNER_RESPONSE","source":"SYSTEM","status":"DONE","step_index":5,"content":"non-model planner text — must be ignored"}`,

		`{"type":"PLANNER_RESPONSE","source":"MODEL","status":"IN_PROGRESS","step_index":6,"content":"partial streaming text — must be ignored"}`,
		`{"type":"CODE_ACTION","source":"MODEL","status":"DONE","step_index":7}`,
		`{"type":"PLANNER_RESPONSE","source":"MODEL","status":"DONE","step_index":8,"content":"Done: created result.txt and verified it."}`,
	})

	got := readRuntimeATranscriptOutput(logPath, cid)
	want := "First I will read the file.\n\nDone: created result.txt and verified it."
	if got != want {
		t.Fatalf("readAntigravityTranscriptOutput mismatch\n got: %q\nwant: %q", got, want)
	}

	if got := readRuntimeATranscriptOutput(logPath, "ffffffff-0000-0000-0000-000000000000"); got != "" {
		t.Errorf("unknown conversation id should yield empty, got %q", got)
	}
	if got := readRuntimeATranscriptOutput("/nonexistent/agy.log", cid); got != "" {
		t.Errorf("missing log (no app data dir) should yield empty, got %q", got)
	}
	if got := readRuntimeATranscriptOutput(logPath, ""); got != "" {
		t.Errorf("empty conversation id should yield empty, got %q", got)
	}
	if got := readRuntimeATranscriptOutput("", cid); got != "" {
		t.Errorf("empty log path should yield empty, got %q", got)
	}
}

func TestReadRuntimeATranscriptOutputResumeReturnsCurrentTurnOnly(t *testing.T) {
	t.Parallel()

	appDataDir := t.TempDir()
	cid := "9e18418b-a431-4523-9616-75a94904554e"

	logPath := filepath.Join(t.TempDir(), "agy.log")
	if err := os.WriteFile(logPath, []byte(strings.Join([]string{
		`I0630 14:19:40.582492 1 common.go:156] CLI app data directory: ` + appDataDir,
		`I0630 14:19:46.755801 1 printmode.go:179] Print mode: conversation=` + cid + `, sending message`,
	}, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}

	seedRuntimeATranscript(t, appDataDir, cid, []string{

		`{"type":"USER_INPUT","source":"USER_EXPLICIT","status":"DONE","step_index":0,"content":"read marker.txt"}`,
		`{"type":"VIEW_FILE","source":"MODEL","status":"DONE","step_index":1}`,
		`{"type":"PLANNER_RESPONSE","source":"MODEL","status":"DONE","step_index":2,"content":"The two values are alpha-1 and beta-2."}`,

		`{"type":"USER_INPUT","source":"USER_EXPLICIT","status":"DONE","step_index":3,"content":"what is 7 times 8?"}`,
		`{"type":"PLANNER_RESPONSE","source":"MODEL","status":"DONE","step_index":4,"content":"56"}`,
	})

	got := readRuntimeATranscriptOutput(logPath, cid)
	if got != "56" {
		t.Fatalf("expected only the current turn's reply %q, got %q (prior turn leaked?)", "56", got)
	}
}

func fakeRuntimeAEmptyStdoutScript(appDataDir, conversationID string) string {
	return `#!/bin/sh
log=""
while [ $# -gt 0 ]; do
  case "$1" in
    --log-file) log="$2"; shift 2 ;;
    *) shift ;;
  esac
done
if [ -n "$log" ]; then
  printf 'I0630 14:19:40.582492 1 common.go:156] CLI app data directory: ` + appDataDir + `\n' >> "$log"
  printf 'I0630 14:19:46.755801 1 printmode.go:179] Print mode: conversation=` + conversationID + `, sending message\n' >> "$log"
fi
exit 0
`
}

func TestRuntimeABackendRecoversEmptyStdoutFromTranscript(t *testing.T) {
	t.Parallel()

	appDataDir := t.TempDir()
	cid := "44a57718-801c-41e7-9691-3225be2b1cb8"
	seedRuntimeATranscript(t, appDataDir, cid, []string{
		`{"type":"USER_INPUT","source":"USER_EXPLICIT","status":"DONE","step_index":0,"content":"create result.txt"}`,
		`{"type":"PLANNER_RESPONSE","source":"MODEL","status":"DONE","step_index":2,"content":null}`,
		`{"type":"VIEW_FILE","source":"MODEL","status":"DONE","step_index":3}`,
		`{"type":"PLANNER_RESPONSE","source":"MODEL","status":"DONE","step_index":4,"content":"I read marker.txt and created result.txt with VERIFIED=yes."}`,
	})

	fakePath := filepath.Join(t.TempDir(), "agy")
	writeTestExecutable(t, fakePath, []byte(fakeRuntimeAEmptyStdoutScript(appDataDir, cid)))

	backend, err := New("runtime-a", Config{ExecutablePath: fakePath, Logger: quietRuntimeALogger()})
	if err != nil {
		t.Fatalf("new antigravity backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	select {
	case result, ok := <-session.Result:
		if !ok {
			t.Fatal("result channel closed without a value")
		}
		if result.Status != "completed" {
			t.Fatalf("expected status=completed, got %q (error=%q)", result.Status, result.Error)
		}
		if !strings.Contains(result.Output, "created result.txt with VERIFIED=yes") {
			t.Fatalf("expected transcript reply recovered into output, got %q", result.Output)
		}

		if strings.Contains(result.Output, "null") {
			t.Errorf("null tool-step content leaked into output: %q", result.Output)
		}
		if result.SessionID != cid {
			t.Errorf("expected session id %q recovered from log, got %q", cid, result.SessionID)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}

	var messages []Message
	for сообщение := range session.Messages {
		messages = append(messages, сообщение)
	}
	var recoveredMessage bool
	for _, сообщение := range messages {
		if сообщение.Type == MessageText && strings.Contains(сообщение.Content, "created result.txt with VERIFIED=yes") {
			recoveredMessage = true
			break
		}
	}
	if !recoveredMessage {
		t.Fatalf("expected recovered transcript reply to be emitted as MessageText, got %#v", messages)
	}
}
