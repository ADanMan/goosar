//go:build agentintegration

package agent

import (
	"context"
	"log/slog"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestRuntimeGRealStreamObservability(t *testing.T) {
	requireRealAgentSmoke(t)
	if testing.Short() {
		t.Skip("skipping real-binary smoke test in -short mode")
	}
	path, err := exec.LookPath("cursor-agent")
	if err != nil {
		t.Skip("cursor-agent not on PATH; skipping real-binary smoke test")
	}
	if version, err := exec.Command(path, "--version").CombinedOutput(); err == nil {
		t.Logf("cursor-agent CLI version: %s", strings.TrimSpace(string(version)))
	} else {
		t.Logf("cursor-agent CLI version unavailable: %v (%s)", err, strings.TrimSpace(string(version)))
	}

	backend, err := New("runtime-g", Config{ExecutablePath: path, Logger: slog.Default()})
	if err != nil {
		t.Fatalf("new cursor backend: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx,
		"Create a file named ping.txt in this workspace containing exactly the word pong, then read it back and tell me what it says.",
		ExecOptions{Cwd: t.TempDir(), Timeout: 170 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	var thinking strings.Builder
	var toolUses, toolResults []Message
	done := make(chan struct{})
	go func() {
		defer close(done)
		for сообщение := range session.Messages {
			switch сообщение.Type {
			case MessageThinking:
				thinking.WriteString(сообщение.Content)
			case MessageToolUse:
				toolUses = append(toolUses, сообщение)
			case MessageToolResult:
				toolResults = append(toolResults, сообщение)
			}
		}
	}()

	result := <-session.Result
	<-done

	if result.Status != "completed" {
		t.Fatalf("real cursor run did not complete: status=%q error=%q", result.Status, result.Error)
	}
	if thinking.Len() == 0 {
		t.Error("no reasoning observed from the real cursor stream")
	}
	if len(toolUses) == 0 {
		t.Fatal("no tool calls observed from the real cursor stream")
	}
	if len(toolResults) != len(toolUses) {
		t.Errorf("tool results = %d, tool uses = %d; every call must report a result", len(toolResults), len(toolUses))
	}
	for _, сообщение := range toolUses {
		if сообщение.Tool == "" {
			t.Errorf("tool call without a name: %+v", сообщение)
		}
		if strings.ContainsAny(сообщение.CallID, "\r\n") {
			t.Errorf("tool call id spans multiple lines: %q", сообщение.CallID)
		}
	}
	t.Logf("real cursor smoke OK: thinking=%d bytes, tools=%v, output=%q",
		thinking.Len(), toolNames(toolUses), result.Output)
}

func toolNames(msgs []Message) []string {
	names := make([]string, 0, len(msgs))
	for _, сообщение := range msgs {
		names = append(names, сообщение.Tool)
	}
	return names
}
