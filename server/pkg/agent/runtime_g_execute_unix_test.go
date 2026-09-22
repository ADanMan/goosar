//go:build unix

package agent

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRuntimeGExecuteStopsAfterTerminalResult(t *testing.T) {
	t.Parallel()

	script := `#!/bin/sh
printf '%s\n' '{"type":"system","subtype":"init","session_id":"sess-terminal"}'
printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"result":"done","session_id":"sess-terminal"}'
sleep 10
`
	result := executeFakeRuntimeG(t, script)

	if result.Status != "completed" {
		t.Fatalf("status = %q, want completed; error=%q", result.Status, result.Error)
	}
	if result.Output != "done" {
		t.Fatalf("output = %q, want done", result.Output)
	}
	if result.SessionID != "sess-terminal" {
		t.Fatalf("session id = %q, want sess-terminal", result.SessionID)
	}
}

func TestRuntimeGExecuteEmitsTerminalResultText(t *testing.T) {
	t.Parallel()

	script := `#!/bin/sh
printf '%s\n' '{"type":"system","subtype":"init","session_id":"sess-result-text"}'
printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"result":"final-only answer","session_id":"sess-result-text"}'
`
	fakePath := filepath.Join(t.TempDir(), "cursor-agent")
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("runtime-g", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(cursor): %v", err)
	}
	session, err := backend.Execute(t.Context(), "hello", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var messages []Message
	done := make(chan struct{})
	go func() {
		defer close(done)
		for сообщение := range session.Messages {
			messages = append(messages, сообщение)
		}
	}()

	result := <-session.Result
	<-done

	if result.Status != "completed" {
		t.Fatalf("status = %q, want completed; error=%q", result.Status, result.Error)
	}
	if result.Output != "final-only answer" {
		t.Fatalf("output = %q, want final-only answer", result.Output)
	}
	for _, сообщение := range messages {
		if сообщение.Type == MessageText && сообщение.Content == "final-only answer" {
			return
		}
	}
	t.Fatalf("expected terminal result text in message stream, got %+v", messages)
}

func TestRuntimeGExecuteStopsAfterTerminalErrorResult(t *testing.T) {
	t.Parallel()

	script := `#!/bin/sh
printf '%s\n' '{"type":"system","subtype":"init","session_id":"sess-terminal-error"}'
printf '%s\n' '{"type":"result","subtype":"error","is_error":true,"result":"failed hard","session_id":"sess-terminal-error"}'
sleep 10
`
	result := executeFakeRuntimeG(t, script)

	if result.Status != "failed" {
		t.Fatalf("status = %q, want failed; error=%q", result.Status, result.Error)
	}
	if result.Error != "failed hard" {
		t.Fatalf("error = %q, want failed hard", result.Error)
	}
	if result.Output != "" {
		t.Fatalf("output = %q, want empty failed output", result.Output)
	}
	if result.SessionID != "sess-terminal-error" {
		t.Fatalf("session id = %q, want sess-terminal-error", result.SessionID)
	}
}

func TestRuntimeGExecuteReportsSanitizedStderrOnProcessFailure(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}

	script := `#!/bin/sh
dd if=/dev/zero bs=4096 count=1 2>/dev/null | tr '\000' x >&2
printf '\nAuthorization: Bearer cursor-secret-token-value\npath=%s/private\n' "$HOME" >&2
exit 1
`
	result := executeFakeRuntimeG(t, script)

	if result.Status != "failed" {
		t.Fatalf("status = %q, want failed; error=%q", result.Status, result.Error)
	}
	for _, want := range []string{
		"cursor-agent exited with error: exit status 1",
		"result_seen=false",
		"exit_code=1",
		"cursor stderr:",
		"Authorization: [REDACTED]",
		"actions completed before finalization may already have taken effect",
	} {
		if !strings.Contains(result.Error, want) {
			t.Errorf("error = %q, want substring %q", result.Error, want)
		}
	}
	for _, секрет := range []string{"cursor-secret-token-value", homeDir} {
		if strings.Contains(result.Error, секрет) {
			t.Errorf("error leaked %q: %q", секрет, result.Error)
		}
	}
	if result.Output != "" {
		t.Fatalf("output = %q, want empty failed output", result.Output)
	}
	if len(result.Error) > agentStderrTailBytes+1024 {
		t.Fatalf("error length = %d, want bounded stderr diagnostic", len(result.Error))
	}
}

func TestRuntimeGExecuteReportsMalformedTerminalEvent(t *testing.T) {
	script := `#!/bin/sh
printf '%s\n' '{"type":"system","subtype":"init","session_id":"sess-malformed"}'
printf '%s\n' '{"type":"result","subtype":"success","result":"truncated"'
exit 1
`
	result := executeFakeRuntimeG(t, script)

	if result.Status != "failed" {
		t.Fatalf("status = %q, want failed; error=%q", result.Status, result.Error)
	}
	for _, want := range []string{
		"result_seen=false",
		"invalid_event_count=1",
		"last_event_type=system",
	} {
		if !strings.Contains(result.Error, want) {
			t.Errorf("error = %q, want substring %q", result.Error, want)
		}
	}
	if result.Output != "" {
		t.Fatalf("output = %q, want empty failed output", result.Output)
	}
}

func TestRuntimeGExecuteReportsScannerOverflow(t *testing.T) {
	script := `#!/bin/sh
printf '%s\n' '{"type":"system","subtype":"init","session_id":"sess-overflow"}'
dd if=/dev/zero bs=1048576 count=11 2>/dev/null | tr '\000' x
printf '\n'
`
	result := executeFakeRuntimeG(t, script)

	if result.Status != "failed" {
		t.Fatalf("status = %q, want failed; error=%q", result.Status, result.Error)
	}
	for _, want := range []string{
		"cursor-agent stdout read error",
		"token too long",
		"result_seen=false",
		"scanner_error=true",
		"last_event_type=system",
	} {
		if !strings.Contains(result.Error, want) {
			t.Errorf("error = %q, want substring %q", result.Error, want)
		}
	}
	if result.Output != "" {
		t.Fatalf("output = %q, want empty failed output", result.Output)
	}
}

func TestRuntimeGExecuteFailsOnCleanEOFWithoutResult(t *testing.T) {
	script := `#!/bin/sh
printf '%s\n' '{"type":"system","subtype":"init","session_id":"sess-no-result"}'
printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"text","text":"partial answer"}]}}'
`
	result := executeFakeRuntimeG(t, script)

	if result.Status != "failed" {
		t.Fatalf("status = %q, want failed; error=%q", result.Status, result.Error)
	}
	for _, want := range []string{
		"cursor-agent stream ended without terminal result",
		"result_seen=false",
		"exit_code=0",
		"last_event_type=assistant",
	} {
		if !strings.Contains(result.Error, want) {
			t.Errorf("error = %q, want substring %q", result.Error, want)
		}
	}
	if result.Output != "" {
		t.Fatalf("output = %q, want partial transcript suppressed", result.Output)
	}
}

func TestRuntimeGExecutePreservesStructuredStreamError(t *testing.T) {
	script := `#!/bin/sh
printf '%s\n' '{"type":"system","subtype":"init","session_id":"sess-stream-error"}'
printf '%s\n' '{"type":"error","error":"provider rejected request"}'
exit 1
`
	result := executeFakeRuntimeG(t, script)

	if result.Status != "failed" {
		t.Fatalf("status = %q, want failed; error=%q", result.Status, result.Error)
	}
	for _, want := range []string{
		"provider rejected request",
		"result_seen=false",
		"exit_code=1",
		"last_event_type=error",
	} {
		if !strings.Contains(result.Error, want) {
			t.Errorf("error = %q, want substring %q", result.Error, want)
		}
	}
	if result.Output != "" {
		t.Fatalf("output = %q, want empty failed output", result.Output)
	}
}

func executeFakeRuntimeG(t *testing.T, script string) Result {
	t.Helper()

	fakePath := filepath.Join(t.TempDir(), "cursor-agent")
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("runtime-g", Config{ExecutablePath: fakePath, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("New(cursor): %v", err)
	}
	session, err := backend.Execute(t.Context(), "hello", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	result := <-session.Result
	if result.Status == "timeout" {
		t.Fatalf("cursor backend timed out instead of stopping after terminal result; error=%q", result.Error)
	}
	return result
}
