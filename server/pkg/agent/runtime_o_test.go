package agent

import (
	"context"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestBuildRuntimeOArgsNoToolAllowlist(t *testing.T) {

	args := buildRuntimeOArgs("test prompt", "/tmp/session.jsonl", ExecOptions{}, slog.Default())
	for i, arg := range args {
		if arg == "--tools" {
			t.Errorf("buildPiArgs emits --tools %q; should not restrict tool registry (see #2379)", args[i+1])
		}
	}
}

func TestBuildRuntimeOArgsBasicFlags(t *testing.T) {
	args := buildRuntimeOArgs("hello world", "/tmp/s.jsonl", ExecOptions{
		Model:        "anthropic/claude-sonnet-4-20250514",
		SystemPrompt: "be helpful",
	}, slog.Default())

	joined := strings.Join(args, " ")
	for _, want := range []string{"-p", "--mode json", "--session /tmp/s.jsonl", "--provider anthropic", "--model claude-sonnet-4-20250514", "--append-system-prompt"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %q in args, got: %v", want, args)
		}
	}

	if args[len(args)-1] != "hello world" {
		t.Errorf("prompt should be last arg, got %q", args[len(args)-1])
	}
}

func TestBuildRuntimeOArgsCustomArgsAppended(t *testing.T) {

	args := buildRuntimeOArgs("prompt", "/tmp/s.jsonl", ExecOptions{
		CustomArgs: []string{"--tools", "read,bash"},
	}, slog.Default())

	found := false
	for i, arg := range args {
		if arg == "--tools" && i+1 < len(args) && args[i+1] == "read,bash" {
			found = true
		}
	}
	if !found {
		t.Errorf("custom --tools should pass through via custom_args, got: %v", args)
	}
}

func TestRuntimeOExecuteAttachesStdinPipe(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "linux" {

		t.Skip("stdin fd inspection relies on /proc/self/fd/0")
	}

	fakePath := filepath.Join(t.TempDir(), "pi")
	script := "#!/bin/sh\n" +
		"kind=$(stat -c '%F' -L /proc/self/fd/0 2>/dev/null || echo unknown)\n" +
		"case \"$kind\" in\n" +
		"  fifo|*pipe*)\n" +
		"    printf '%s\\n' '{\"type\":\"agent_start\"}'\n" +
		"    printf '%s\\n' '{\"type\":\"turn_end\",\"message\":{\"role\":\"assistant\",\"model\":\"test\",\"usage\":{\"input\":1,\"output\":1,\"cacheRead\":0,\"cacheWrite\":0,\"totalTokens\":2}}}'\n" +
		"    exit 0\n" +
		"    ;;\n" +
		"esac\n" +
		"printf 'stdin was %s; expected fifo\\n' \"$kind\" >&2\n" +
		"exit 1\n"
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("runtime-o", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new pi backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{Timeout: 5 * time.Second})
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
		if result.Status != "completed" {
			t.Fatalf("expected status=completed (stdin attached as fifo), got %q (error=%q)", result.Status, result.Error)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func runtimeOEventStreamScript(events []string) string {
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	for _, e := range events {
		b.WriteString("printf '%s\\n' '")
		b.WriteString(e)
		b.WriteString("'\n")
	}
	return b.String()
}

func TestRuntimeOExecuteRetainsOnlyLastTurnOutput(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	events := []string{
		`{"type":"agent_start"}`,
		`{"type":"turn_start"}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"intermediate"}}`,
		`{"type":"tool_execution_start","toolCallId":"call_1","toolName":"bash","args":{"command":"echo hi"}}`,
		`{"type":"tool_execution_end","toolCallId":"call_1","toolName":"bash","result":{"content":[{"type":"text","text":"hi"}]},"isError":false}`,
		`{"type":"turn_end","message":{"role":"assistant","model":"test","usage":{"input":1,"output":1}}}`,
		`{"type":"turn_start"}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"final"}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":" "}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"answer"}}`,
		`{"type":"turn_end","message":{"role":"assistant","model":"test","usage":{"input":2,"output":2}}}`,
	}
	fakePath := filepath.Join(t.TempDir(), "pi")
	writeTestExecutable(t, fakePath, []byte(runtimeOEventStreamScript(events)))

	backend, err := New("runtime-o", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new pi backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt-ignored", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	go func() {
		for range session.Messages {
		}
	}()

	select {
	case result := <-session.Result:
		if result.Status != "completed" {
			t.Fatalf("expected status=completed, got %q (error=%q)", result.Status, result.Error)
		}
		if result.Output != "final answer" {
			t.Fatalf("Output: got %q, want %q", result.Output, "final answer")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestStripRuntimeOToolCallMarkup(t *testing.T) {
	tests := map[string]string{
		`before call:bash{command:<|"|>cd repo/path && ls -F<|"|>}<tool_call|> after`:                          "before  after",
		`before call:read{path:<|"|>repo/path/roles/example/verify.yml<|"|>} after`:                            "before  after",
		`before response:bash{command:<|"|>goosar issue comment list issue-id --all --output json<|"|>} after`: "before  after",
		`before call:bash{command:<|"|>printf '{"key":"value"}'<|"|>} after`:                                   "before  after",
		`before <|turn>model after`: "before  after",
	}
	for in, want := range tests {
		got := stripRuntimeOToolCallMarkup(in)
		if got != want {
			t.Fatalf("unexpected stripped text: %q, want %q", got, want)
		}
	}
}

func TestDrainRuntimeOTextBufferSplitToolCall(t *testing.T) {
	chunks := []string{
		"before ca",
		`ll:bash{command:<|"|>ls -R repo/path`,
		`/roles/example<|"|>}`,
		" after",
	}
	var buf strings.Builder
	var got strings.Builder
	for _, chunk := range chunks {
		got.WriteString(drainRuntimeOTextBuffer(&buf, chunk))
	}
	got.WriteString(flushRuntimeOTextBuffer(&buf))
	if got.String() != "before  after" {
		t.Fatalf("unexpected streamed text: %q", got.String())
	}
}

func TestDrainRuntimeOTextBufferSplitControlToken(t *testing.T) {
	chunks := []string{"before <|tu", "rn>model after"}
	var buf strings.Builder
	var got strings.Builder
	for _, chunk := range chunks {
		got.WriteString(drainRuntimeOTextBuffer(&buf, chunk))
	}
	got.WriteString(flushRuntimeOTextBuffer(&buf))
	if got.String() != "before  after" {
		t.Fatalf("unexpected streamed text: %q", got.String())
	}
}

func TestFlushRuntimeOTextBufferKeepsUnmatchedToolPrefixes(t *testing.T) {
	tests := []string{
		"plain response: see below",
		"plain call: see below",
		`plain call:bash{command:<|"|>unterminated`,
	}
	for _, want := range tests {
		var buf strings.Builder
		got := drainRuntimeOTextBuffer(&buf, want)
		got += flushRuntimeOTextBuffer(&buf)
		if got != want {
			t.Fatalf("unexpected flushed text: %q, want %q", got, want)
		}
	}
}
