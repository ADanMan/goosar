package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

type runtimeFBackend struct {
	cfg Config
}

type runtimeFEventState struct {
	output strings.Builder

	pendingDelta strings.Builder
	sessionID    string
	activeModel  string
	finalStatus  string
	finalError   string
	usage        map[string]TokenUsage
}

func (st *runtimeFEventState) finalOutput() string {
	if st.pendingDelta.Len() > 0 {
		return st.pendingDelta.String()
	}
	return st.output.String()
}

func newRuntimeFEventState(seedModel string) *runtimeFEventState {
	return &runtimeFEventState{
		activeModel: seedModel,
		finalStatus: "completed",
		usage:       make(map[string]TokenUsage),
	}
}

func handleRuntimeFEvent(evt runtimeFEvent, st *runtimeFEventState) []Message {
	var msgs []Message

	switch evt.Type {
	case "session.start":
		var ss runtimeFSessionStart
		if err := json.Unmarshal(evt.Data, &ss); err == nil {
			if ss.SelectedModel != "" {
				st.activeModel = ss.SelectedModel
			}

			if ss.SessionID != "" {
				st.sessionID = ss.SessionID
			}
		}

	case "assistant.message_delta":
		var delta runtimeFMessageDelta
		if err := json.Unmarshal(evt.Data, &delta); err == nil && delta.DeltaContent != "" {

			st.pendingDelta.WriteString(delta.DeltaContent)
			msgs = append(msgs, Message{Type: MessageText, Content: delta.DeltaContent})
		}

	case "assistant.message":
		var сообщение runtimeFAssistantMessage
		if err := json.Unmarshal(evt.Data, &сообщение); err != nil {
			return nil
		}

		if сообщение.Content != "" {
			st.output.Reset()
			st.output.WriteString(сообщение.Content)
		}

		st.pendingDelta.Reset()
		if сообщение.ReasoningText != "" {
			msgs = append(msgs, Message{Type: MessageThinking, Content: сообщение.ReasoningText})
		}
		if сообщение.OutputTokens > 0 {
			u := st.usage[st.activeModel]
			u.OutputTokens += сообщение.OutputTokens
			st.usage[st.activeModel] = u
		}
		for _, tr := range сообщение.ToolRequests {
			var input map[string]any
			if tr.Arguments != nil {
				_ = json.Unmarshal(tr.Arguments, &input)
			}
			msgs = append(msgs, Message{
				Type:   MessageToolUse,
				Tool:   tr.Name,
				CallID: tr.ToolCallID,
				Input:  input,
			})
		}

	case "assistant.reasoning", "assistant.reasoning_delta":

		var r runtimeFReasoning
		if err := json.Unmarshal(evt.Data, &r); err == nil {
			text := r.Content
			if text == "" {
				text = r.DeltaContent
			}
			if text != "" {
				msgs = append(msgs, Message{Type: MessageThinking, Content: text})
			}
		}

	case "tool.execution_complete":
		var tc runtimeFToolExecComplete
		if err := json.Unmarshal(evt.Data, &tc); err != nil {
			return nil
		}
		if tc.Model != "" {
			st.activeModel = tc.Model
		}
		resultContent := ""
		if tc.Success && tc.Result != nil {
			resultContent = tc.Result.Content
		} else if !tc.Success {
			if tc.Error != nil {
				resultContent = "Error: " + tc.Error.Message
			} else if tc.Result != nil {
				resultContent = tc.Result.Content
			}
		}
		msgs = append(msgs, Message{
			Type:   MessageToolResult,
			CallID: tc.ToolCallID,
			Output: resultContent,
		})

	case "assistant.turn_start":
		msgs = append(msgs, Message{Type: MessageStatus, Status: "running"})

	case "session.error":
		var se runtimeFSessionError
		if err := json.Unmarshal(evt.Data, &se); err == nil {
			st.finalStatus = "failed"
			st.finalError = se.Message
			msgs = append(msgs, Message{Type: MessageLog, Level: "error", Content: se.Message})
		}

	case "session.warning":
		var sw runtimeFSessionWarning
		if err := json.Unmarshal(evt.Data, &sw); err == nil {
			msgs = append(msgs, Message{Type: MessageLog, Level: "warn", Content: sw.Message})
		}

	case "result":
		if evt.SessionID != "" {
			st.sessionID = evt.SessionID
		}
		if evt.ExitCode != 0 {
			st.finalStatus = "failed"
			st.finalError = withRuntimeFExitCode(st.finalError, evt.ExitCode)
		}
	}

	return msgs
}

func withRuntimeFExitCode(сообщение string, exitCode int) string {
	exitMsg := fmt.Sprintf("copilot exited with code %d", exitCode)
	сообщение = strings.TrimSpace(сообщение)
	if сообщение == "" {
		return exitMsg
	}
	if strings.Contains(сообщение, exitMsg) {
		return сообщение
	}
	return сообщение + "; " + exitMsg
}

func (b *runtimeFBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execName := b.cfg.ExecutablePath
	if execName == "" {
		execName = runtimeCLIName("runtime-f")
	}
	lookedUp, err := exec.LookPath(execName)
	if err != nil {
		return nil, fmt.Errorf("copilot executable not found at %q: %w", execName, err)
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	args := buildRuntimeFArgs(prompt, opts, b.cfg.Logger)
	argv0, cmdArgs := chooseRuntimeFInvocation(execName, lookedUp, args, b.cfg.Logger)

	cmd := newRuntimeCmd(exec.CommandContext(runCtx, argv0, cmdArgs...))
	hideAgentWindow(cmd)
	b.cfg.logAgentCommand(cmd, newAgentCommandLogArgs(args))
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("copilot stdout pipe: %w", err)
	}
	stderrBuf := newStderrTail(newLogWriter(b.cfg.Logger, "[copilot:stderr] "), agentStderrTailBytes)
	cmd.Stderr = stderrBuf

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start copilot: %w", err)
	}

	b.cfg.Logger.Info("copilot started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		startTime := time.Now()
		seedModel := opts.Model
		if seedModel == "" {
			seedModel = "copilot"
		}
		st := newRuntimeFEventState(seedModel)

		go func() {
			<-runCtx.Done()
			_ = stdout.Close()
		}()

		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}

			var evt runtimeFEvent
			if err := json.Unmarshal([]byte(line), &evt); err != nil {
				slog.Warn("copilot event parse failed", "err", err, "line", line)
				continue
			}

			for _, m := range handleRuntimeFEvent(evt, st) {
				trySend(msgCh, m)
			}
		}
		if err := scanner.Err(); err != nil {
			slog.Warn("copilot stdout scanner error", "err", err)
		}

		exitErr := cmd.Wait()
		duration := time.Since(startTime)

		if runCtx.Err() == context.DeadlineExceeded {
			st.finalStatus = "timeout"
			st.finalError = fmt.Sprintf("copilot timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			st.finalStatus = "aborted"
			st.finalError = "execution cancelled"
		} else if exitErr != nil && st.finalStatus == "completed" {
			st.finalStatus = "failed"
			st.finalError = fmt.Sprintf("copilot exited with error: %v", exitErr)
		}
		if st.finalError != "" {
			st.finalError = withAgentStderr(st.finalError, "copilot", stderrBuf.Tail())
		}

		b.cfg.Logger.Info("copilot finished", "pid", cmd.Process.Pid, "status", st.finalStatus, "duration", duration.Round(time.Millisecond).String())

		resCh <- Result{
			Status:     st.finalStatus,
			Output:     st.finalOutput(),
			Error:      st.finalError,
			DurationMs: duration.Milliseconds(),
			SessionID:  st.sessionID,
			Usage:      st.usage,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

type runtimeFEvent struct {
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data,omitempty"`
	ID        string          `json:"id,omitempty"`
	Timestamp string          `json:"timestamp,omitempty"`
	ParentID  string          `json:"parentId,omitempty"`
	Ephemeral bool            `json:"ephemeral,omitempty"`

	SessionID string               `json:"sessionId,omitempty"`
	ExitCode  int                  `json:"exitCode,omitempty"`
	Usage     *runtimeFResultUsage `json:"usage,omitempty"`
}

type runtimeFSessionStart struct {
	SessionID     string `json:"sessionId"`
	SelectedModel string `json:"selectedModel"`
}

type runtimeFAssistantMessage struct {
	MessageID     string                `json:"messageId"`
	Content       string                `json:"content"`
	ToolRequests  []runtimeFToolRequest `json:"toolRequests"`
	OutputTokens  int64                 `json:"outputTokens"`
	InteractionID string                `json:"interactionId"`
	ReasoningText string                `json:"reasoningText,omitempty"`
}

type runtimeFToolRequest struct {
	ToolCallID       string          `json:"toolCallId"`
	Name             string          `json:"name"`
	Arguments        json.RawMessage `json:"arguments"`
	Type             string          `json:"type"`
	IntentionSummary string          `json:"intentionSummary,omitempty"`
}

type runtimeFMessageDelta struct {
	MessageID    string `json:"messageId"`
	DeltaContent string `json:"deltaContent"`
}

type runtimeFToolExecComplete struct {
	ToolCallID    string              `json:"toolCallId"`
	Model         string              `json:"model"`
	InteractionID string              `json:"interactionId"`
	Success       bool                `json:"success"`
	Result        *runtimeFToolResult `json:"result,omitempty"`
	Error         *runtimeFToolError  `json:"error,omitempty"`
}

type runtimeFToolResult struct {
	Content         string `json:"content"`
	DetailedContent string `json:"detailedContent,omitempty"`
}

type runtimeFToolError struct {
	Message string `json:"message"`
}

type runtimeFReasoning struct {
	Content      string `json:"content,omitempty"`
	DeltaContent string `json:"deltaContent,omitempty"`
}

type runtimeFSessionError struct {
	ErrorType string `json:"errorType"`
	Message   string `json:"message"`
}

type runtimeFSessionWarning struct {
	WarningType string `json:"warningType"`
	Message     string `json:"message"`
}

type runtimeFResultUsage struct {
	PremiumRequests    float64              `json:"premiumRequests"`
	TotalAPIDurationMs int64                `json:"totalApiDurationMs"`
	SessionDurationMs  int64                `json:"sessionDurationMs"`
	CodeChanges        *runtimeFCodeChanges `json:"codeChanges,omitempty"`
}

type runtimeFCodeChanges struct {
	LinesAdded    int      `json:"linesAdded"`
	LinesRemoved  int      `json:"linesRemoved"`
	FilesModified []string `json:"filesModified"`
}

var runtimeFBlockedArgs = map[string]blockedArgMode{
	"-p":                blockedWithValue,
	"--output-format":   blockedWithValue,
	"--allow-all":       blockedStandalone,
	"--allow-all-tools": blockedStandalone,
	"--allow-all-paths": blockedStandalone,
	"--allow-all-urls":  blockedStandalone,
	"--yolo":            blockedStandalone,
	"--no-ask-user":     blockedStandalone,
	"--resume":          blockedWithValue,
	"--acp":             blockedStandalone,
}

func buildRuntimeFArgs(prompt string, opts ExecOptions, logger *slog.Logger) []string {
	args := []string{
		"-p", prompt,
		"--output-format", "json",
		"--allow-all",
		"--no-ask-user",
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "--resume", opts.ResumeSessionID)
	}
	args = append(args, filterCustomArgs(opts.CustomArgs, runtimeFBlockedArgs, logger)...)
	return args
}
