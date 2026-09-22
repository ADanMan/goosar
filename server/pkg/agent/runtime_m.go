package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

var runtimeMTerminateGraceNanos atomic.Int64

func runtimeMTerminateGrace() time.Duration {
	if n := runtimeMTerminateGraceNanos.Load(); n > 0 {
		return time.Duration(n)
	}
	return 5 * time.Second
}

var runtimeMBlockedArgs = map[string]blockedArgMode{
	"--format":                       blockedWithValue,
	"--dir":                          blockedWithValue,
	"--variant":                      blockedWithValue,
	"--dangerously-skip-permissions": blockedStandalone,
}

type runtimeMBackend struct {
	cfg Config
}

func (b *runtimeMBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = runtimeCLIName("runtime-m")
	}
	resolved, err := exec.LookPath(execPath)
	if err != nil {
		return nil, fmt.Errorf("opencode executable not found at %q: %w", execPath, err)
	}
	if runtime.GOOS == "windows" {
		if native := resolveRuntimeMNativeFromShim(resolved, os.Stat); native != "" {
			b.cfg.Logger.Info("opencode resolved to native binary to avoid .cmd shim argv truncation", "shim", resolved, "native", native)
			resolved = native
		}
	}
	execPath = resolved

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	args := []string{"run", "--format", "json", "--dangerously-skip-permissions"}

	if opts.Cwd != "" {
		args = append(args, "--dir", opts.Cwd)
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.ThinkingLevel != "" {
		args = append(args, "--variant", opts.ThinkingLevel)
	}
	if opts.SystemPrompt != "" {
		args = append(args, "--prompt", opts.SystemPrompt)
	}
	if opts.MaxTurns > 0 {
		b.cfg.Logger.Warn("opencode does not support --max-turns; ignoring", "maxTurns", opts.MaxTurns)
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "--session", opts.ResumeSessionID)
	}
	args = append(args, filterCustomArgs(opts.CustomArgs, runtimeMBlockedArgs, b.cfg.Logger)...)
	args = append(args, prompt)

	cmd := newRuntimeCmd(exec.CommandContext(runCtx, execPath, args...))
	hideAgentWindow(cmd)

	configureProcessGroup(cmd)

	cmd.Cancel = func() error { return nil }
	b.cfg.logAgentCommand(cmd, newAgentCommandLogArgs(args, trustAgentCommandPositional(0, "run")))
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}

	env := buildEnv(b.cfg.Env)

	if opts.Cwd != "" {
		env = append(env, "PWD="+opts.Cwd)
	}

	mcpContent, err := buildRuntimeMMCPConfigContent(opts.McpConfig)
	if err != nil {
		cancel()
		return nil, err
	}
	if mcpContent != "" {
		if _, dup := b.cfg.Env["OPENCODE_CONFIG_CONTENT"]; dup {
			b.cfg.Logger.Warn("agent.custom_env sets OPENCODE_CONFIG_CONTENT but agent.mcp_config takes precedence and overrides it")
		}
		env = append(env, "OPENCODE_CONFIG_CONTENT="+mcpContent)
	}
	cmd.Env = env

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("opencode stdout pipe: %w", err)
	}
	cmd.Stderr = newLogWriter(b.cfg.Logger, "[opencode:stderr] ")

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start opencode: %w", err)
	}

	b.cfg.Logger.Info("opencode started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	procDone := make(chan struct{})

	go func() {
		select {
		case <-procDone:
			return
		case <-runCtx.Done():
		}
		if cmd.Process != nil {
			signalProcessGroup(cmd.Process, syscall.SIGTERM)
			select {
			case <-procDone:
			case <-time.After(runtimeMTerminateGrace()):
				signalProcessGroup(cmd.Process, syscall.SIGKILL)
			}
		}
		_ = stdout.Close()
	}()

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		startTime := time.Now()
		scanResult := b.processEvents(stdout, msgCh)

		exitErr := cmd.Wait()
		close(procDone)
		duration := time.Since(startTime)

		if runCtx.Err() == context.DeadlineExceeded {
			scanResult.status = "timeout"
			scanResult.errMsg = fmt.Sprintf("opencode timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			scanResult.status = "aborted"
			scanResult.errMsg = "execution cancelled"
		} else if exitErr != nil && scanResult.status == "completed" {
			scanResult.status = "failed"
			scanResult.errMsg = fmt.Sprintf("opencode exited with error: %v", exitErr)
		} else if exitErr != nil && scanResult.noTerminalSignal {

			scanResult.errMsg = fmt.Sprintf("%s; opencode exited with error: %v", scanResult.errMsg, exitErr)
		}

		b.cfg.Logger.Info("opencode finished", "pid", cmd.Process.Pid, "status", scanResult.status, "duration", duration.Round(time.Millisecond).String())

		var usage map[string]TokenUsage
		u := scanResult.usage
		if u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheReadTokens > 0 || u.CacheWriteTokens > 0 {
			model := opts.Model
			if model == "" {
				model = "unknown"
			}
			usage = map[string]TokenUsage{model: u}
		}

		resCh <- Result{
			Status:     scanResult.status,
			Output:     scanResult.output,
			Error:      scanResult.errMsg,
			DurationMs: duration.Milliseconds(),
			SessionID:  scanResult.sessionID,
			Usage:      usage,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

type eventResult struct {
	status           string
	errMsg           string
	output           string
	sessionID        string
	usage            TokenUsage
	noTerminalSignal bool
}

func (b *runtimeMBackend) processEvents(r io.Reader, ch chan<- Message) eventResult {
	var output strings.Builder
	var sessionID string
	var usage TokenUsage
	finalStatus := "completed"
	var finalError string

	openStep := false
	stepHasContinuationTool := false
	awaitingContinuation := false

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var event runtimeMEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		if event.SessionID != "" {
			sessionID = event.SessionID
		}

		switch event.Type {
		case "text":
			b.handleTextEvent(event, ch, &output)
		case "tool_use":
			b.handleToolUseEvent(event, ch)
			if event.Part.Metadata == nil || !event.Part.Metadata.ProviderExecuted {
				stepHasContinuationTool = true
			}
		case "error":
			b.handleErrorEvent(event, ch, &finalStatus, &finalError)
		case "step_start":
			openStep = true
			stepHasContinuationTool = false
			awaitingContinuation = false
			trySend(ch, Message{Type: MessageStatus, Status: "running"})
		case "step_finish":
			openStep = false
			awaitingContinuation = event.Part.Reason == "tool-calls" ||
				(event.Part.Reason != "" && stepHasContinuationTool)
			stepHasContinuationTool = false

			if t := event.Part.Tokens; t != nil {
				usage.InputTokens += t.Input
				usage.OutputTokens += t.Output
				if t.Cache != nil {
					usage.CacheReadTokens += t.Cache.Read
					usage.CacheWriteTokens += t.Cache.Write
				}
			}
		}
	}

	if scanErr := scanner.Err(); scanErr != nil {
		b.cfg.Logger.Warn("opencode stdout scanner error", "error", scanErr)
		if finalStatus == "completed" {
			finalStatus = "failed"
			finalError = fmt.Sprintf("stdout read error: %v", scanErr)
		}
	}

	noTerminalSignal := false
	if finalStatus == "completed" && (openStep || awaitingContinuation) {
		finalStatus = "failed"
		if openStep {
			finalError = "opencode stream ended without a terminal signal (step still open at EOF)"
		} else {
			finalError = "opencode stream ended without a terminal signal (last step required a continuation that never started)"
		}
		noTerminalSignal = true
	}

	return eventResult{
		status:           finalStatus,
		errMsg:           finalError,
		output:           output.String(),
		sessionID:        sessionID,
		usage:            usage,
		noTerminalSignal: noTerminalSignal,
	}
}

func (b *runtimeMBackend) handleTextEvent(event runtimeMEvent, ch chan<- Message, output *strings.Builder) {
	text := event.Part.Text
	if text != "" {
		output.WriteString(text)
		trySend(ch, Message{Type: MessageText, Content: text})
	}
}

func (b *runtimeMBackend) handleToolUseEvent(event runtimeMEvent, ch chan<- Message) {

	var input map[string]any
	if event.Part.State != nil && event.Part.State.Input != nil {
		_ = json.Unmarshal(event.Part.State.Input, &input)
	}

	trySend(ch, Message{
		Type:   MessageToolUse,
		Tool:   event.Part.Tool,
		CallID: event.Part.CallID,
		Input:  input,
	})

	if event.Part.State != nil && event.Part.State.Status == "completed" {
		outputStr := extractToolOutput(event.Part.State.Output)
		trySend(ch, Message{
			Type:   MessageToolResult,
			Tool:   event.Part.Tool,
			CallID: event.Part.CallID,
			Output: outputStr,
		})
	}
}

func (b *runtimeMBackend) handleErrorEvent(event runtimeMEvent, ch chan<- Message, finalStatus, finalError *string) {
	errMsg := ""
	if event.Error != nil {
		errMsg = event.Error.Message()
	}
	if errMsg == "" {
		errMsg = "unknown opencode error"
	}

	b.cfg.Logger.Warn("opencode error event", "error", errMsg)
	trySend(ch, Message{Type: MessageError, Content: errMsg})

	*finalStatus = "failed"
	*finalError = errMsg
}

func resolveRuntimeMNativeFromShim(shimPath string, statFn func(string) (os.FileInfo, error)) string {
	if !strings.EqualFold(filepath.Ext(shimPath), ".cmd") {
		return ""
	}
	prefix := filepath.Dir(shimPath)
	for _, pkg := range runtimeMWindowsPackageCandidates(runtime.GOARCH) {
		candidate := filepath.Join(prefix, "node_modules", "opencode-ai", "node_modules", pkg, "bin", "opencode.exe")
		if _, err := statFn(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func runtimeMWindowsPackageCandidates(goarch string) []string {
	switch goarch {
	case "arm64":
		return []string{"opencode-windows-arm64", "opencode-windows-x64", "opencode-windows-x64-baseline"}
	default:
		return []string{"opencode-windows-x64", "opencode-windows-x64-baseline", "opencode-windows-arm64"}
	}
}

func extractToolOutput(output any) string {
	if output == nil {
		return ""
	}
	if s, ok := output.(string); ok {
		return s
	}
	data, _ := json.Marshal(output)
	return string(data)
}

type runtimeMEvent struct {
	Type      string            `json:"type"`
	Timestamp int64             `json:"timestamp,omitempty"`
	SessionID string            `json:"sessionID,omitempty"`
	Part      runtimeMEventPart `json:"part"`
	Error     *runtimeMError    `json:"error,omitempty"`
}

type runtimeMEventPart struct {
	ID        string `json:"id,omitempty"`
	MessageID string `json:"messageID,omitempty"`
	SessionID string `json:"sessionID,omitempty"`
	Type      string `json:"type,omitempty"`

	Text string `json:"text,omitempty"`

	Tool   string             `json:"tool,omitempty"`
	CallID string             `json:"callID,omitempty"`
	State  *runtimeMToolState `json:"state,omitempty"`

	Metadata *runtimeMPartMetadata `json:"metadata,omitempty"`

	Tokens *runtimeMTokens `json:"tokens,omitempty"`

	Reason string `json:"reason,omitempty"`
}

type runtimeMPartMetadata struct {
	ProviderExecuted bool `json:"providerExecuted,omitempty"`
}

type runtimeMTokens struct {
	Input  int64                `json:"input"`
	Output int64                `json:"output"`
	Cache  *runtimeMCacheTokens `json:"cache,omitempty"`
}

type runtimeMCacheTokens struct {
	Read  int64 `json:"read"`
	Write int64 `json:"write"`
}

type runtimeMToolState struct {
	Status string          `json:"status,omitempty"`
	Input  json.RawMessage `json:"input,omitempty"`
	Output any             `json:"output,omitempty"`
}

type runtimeMError struct {
	Name string           `json:"name,omitempty"`
	Data *runtimeMErrData `json:"data,omitempty"`
}

func (e *runtimeMError) Message() string {
	if e.Data != nil && e.Data.Message != "" {
		return e.Data.Message
	}
	if e.Name != "" {
		return e.Name
	}
	return ""
}

type runtimeMErrData struct {
	Message string `json:"message,omitempty"`
}
