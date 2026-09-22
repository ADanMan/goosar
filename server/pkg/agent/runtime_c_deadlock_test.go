package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	switch mode := os.Getenv("CLAUDE_FAKE_MODE"); mode {
	case "":
		os.Exit(m.Run())
	case "startup_stdout_burst":
		runFakeRuntimeCStartupStdoutBurst()
		os.Exit(0)
	case "control_request":
		runFakeRuntimeCControlRequest()
		os.Exit(0)
	case "background_control_request":
		runFakeRuntimeCBackgroundControlRequest()
		os.Exit(0)
	case "async_launched_tool_result":
		runFakeRuntimeCAsyncLaunchedToolResult()
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "unknown CLAUDE_FAKE_MODE: %q\n", mode)
		os.Exit(2)
	}
}

func runFakeRuntimeCStartupStdoutBurst() {
	line := strings.Repeat("x", 1020)
	bw := bufio.NewWriter(os.Stdout)
	for i := 0; i < 256; i++ {
		if _, err := fmt.Fprintf(bw, `{"type":"log","log":{"level":"info","message":"%s"}}`+"\n", line); err != nil {
			os.Exit(11)
		}
	}
	if err := bw.Flush(); err != nil {
		os.Exit(12)
	}
	if _, err := bufio.NewReader(os.Stdin).ReadString('\n'); err != nil {
		os.Exit(13)
	}
	fmt.Println(`{"type":"result","subtype":"success","is_error":false,"session_id":"sess-deadlock","result":"done"}`)
}

func runFakeRuntimeCControlRequest() {
	reader := bufio.NewReader(os.Stdin)
	if _, err := reader.ReadString('\n'); err != nil {
		fmt.Fprintf(os.Stderr, "read prompt: %v\n", err)
		os.Exit(21)
	}
	fmt.Println(`{"type":"system","session_id":"sess-control"}`)
	fmt.Println(`{"type":"control_request","request_id":"req-42","request":{"subtype":"tool_use","tool_name":"Bash","input":{"command":"pwd"}}}`)

	line, err := reader.ReadString('\n')
	if err != nil {
		fmt.Fprintf(os.Stderr, "read control response: %v\n", err)
		os.Exit(22)
	}
	var resp struct {
		Type     string `json:"type"`
		Response struct {
			RequestID string `json:"request_id"`
		} `json:"response"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &resp); err != nil {
		fmt.Fprintf(os.Stderr, "decode control response: %v\n", err)
		os.Exit(23)
	}
	if resp.Type != "control_response" || resp.Response.RequestID != "req-42" {
		fmt.Fprintf(os.Stderr, "unexpected control response: %s\n", line)
		os.Exit(24)
	}
	fmt.Println(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"approved"}]}}`)
	fmt.Println(`{"type":"result","subtype":"success","is_error":false,"session_id":"sess-control","result":"done after control"}`)
}

func runFakeRuntimeCBackgroundControlRequest() {
	reader := bufio.NewReader(os.Stdin)
	if _, err := reader.ReadString('\n'); err != nil {
		fmt.Fprintf(os.Stderr, "read prompt: %v\n", err)
		os.Exit(31)
	}
	fmt.Println(`{"type":"system","session_id":"sess-background-control"}`)
	fmt.Println(`{"type":"control_request","request_id":"req-bg","request":{"subtype":"tool_use","tool_name":"Bash","input":{"command":"sleep 60","run_in_background":true}}}`)

	line, err := reader.ReadString('\n')
	if err != nil {
		fmt.Fprintf(os.Stderr, "read control response: %v\n", err)
		os.Exit(32)
	}
	var resp struct {
		Type     string `json:"type"`
		Response struct {
			RequestID string `json:"request_id"`
			Response  struct {
				UpdatedInput map[string]any `json:"updatedInput"`
			} `json:"response"`
		} `json:"response"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &resp); err != nil {
		fmt.Fprintf(os.Stderr, "decode control response: %v\n", err)
		os.Exit(33)
	}
	if resp.Type != "control_response" || resp.Response.RequestID != "req-bg" {
		fmt.Fprintf(os.Stderr, "unexpected control response: %s\n", line)
		os.Exit(34)
	}
	if runInBackground, ok := resp.Response.Response.UpdatedInput["run_in_background"].(bool); !ok || runInBackground {
		fmt.Fprintf(os.Stderr, "expected foreground updatedInput, got: %s\n", line)
		os.Exit(35)
	}

	fmt.Println(`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"call-bg","content":"foreground completed"}]}}`)
	fmt.Println(`{"type":"result","subtype":"success","is_error":false,"session_id":"sess-background-control","result":"done after foreground rewrite"}`)
}

func runFakeRuntimeCAsyncLaunchedToolResult() {
	reader := bufio.NewReader(os.Stdin)
	if _, err := reader.ReadString('\n'); err != nil {
		fmt.Fprintf(os.Stderr, "read prompt: %v\n", err)
		os.Exit(41)
	}
	fmt.Println(`{"type":"system","session_id":"sess-async-launched"}`)
	fmt.Println(`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"call-async","content":{"status":"async_launched","message":"background task launched"}}]}}`)
	fmt.Println(`{"type":"result","subtype":"success","is_error":false,"session_id":"sess-async-launched","result":"parent turn completed early"}`)
}

func TestRuntimeCExecuteDoesNotDeadlockOnStartupStdoutBurst(t *testing.T) {
	t.Parallel()

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	backend, err := New("runtime-c", Config{
		ExecutablePath: self,
		Env:            map[string]string{"CLAUDE_FAKE_MODE": "startup_stdout_burst", "IS_SANDBOX": "1"},
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("new claude backend: %v", err)
	}

	prompt := strings.Repeat("p", 128*1024)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, prompt, ExecOptions{Timeout: 20 * time.Second})
	if err != nil {
		t.Fatalf("execute returned error: %v", err)
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
			t.Fatalf("expected status=completed, got %q (error=%q)", result.Status, result.Error)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("timeout waiting for result — claude backend is deadlocked on writeClaudeInput because stdout is not being drained concurrently")
	}
}

func TestRuntimeCExecuteRespondsToControlRequest(t *testing.T) {
	t.Parallel()

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	backend, err := New("runtime-c", Config{
		ExecutablePath: self,
		Env:            map[string]string{"CLAUDE_FAKE_MODE": "control_request", "IS_SANDBOX": "1"},
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("new claude backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "run a command", ExecOptions{Timeout: 8 * time.Second})
	if err != nil {
		t.Fatalf("execute returned error: %v", err)
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
			t.Fatalf("expected status=completed, got %q (error=%q)", result.Status, result.Error)
		}
		if result.Output != "done after control" {
			t.Fatalf("expected result output from fake claude, got %q", result.Output)
		}
		if result.SessionID != "sess-control" {
			t.Fatalf("expected session id sess-control, got %q", result.SessionID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for result — claude backend did not answer control_request")
	}
}

func TestRuntimeCExecuteForcesBackgroundControlRequestForeground(t *testing.T) {
	t.Parallel()

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	backend, err := New("runtime-c", Config{
		ExecutablePath: self,
		Env:            map[string]string{"CLAUDE_FAKE_MODE": "background_control_request", "IS_SANDBOX": "1"},
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("new claude backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "run a background command", ExecOptions{Timeout: 8 * time.Second})
	if err != nil {
		t.Fatalf("execute returned error: %v", err)
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
			t.Fatalf("expected status=completed, got %q (error=%q)", result.Status, result.Error)
		}
		if result.Output != "done after foreground rewrite" {
			t.Fatalf("expected foreground rewrite result, got %q", result.Output)
		}
		if result.SessionID != "sess-background-control" {
			t.Fatalf("expected session id sess-background-control, got %q", result.SessionID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for result — claude backend did not foreground background control_request")
	}
}

func TestRuntimeCExecuteFailsLoudlyOnAsyncLaunchedToolResult(t *testing.T) {
	t.Parallel()

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	backend, err := New("runtime-c", Config{
		ExecutablePath: self,
		Env:            map[string]string{"CLAUDE_FAKE_MODE": "async_launched_tool_result", "IS_SANDBOX": "1"},
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("new claude backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "launch async work", ExecOptions{Timeout: 8 * time.Second})
	if err != nil {
		t.Fatalf("execute returned error: %v", err)
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
		if !strings.Contains(result.Error, "async background task") {
			t.Fatalf("expected async background task error, got %q", result.Error)
		}
		if result.SessionID != "sess-async-launched" {
			t.Fatalf("expected session id sess-async-launched, got %q", result.SessionID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for result — claude backend did not fail async_launched tool result")
	}
}
