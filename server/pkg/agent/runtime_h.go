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

var runtimeHTerminateGraceNanos atomic.Int64

func runtimeHTerminateGrace() time.Duration {
	if n := runtimeHTerminateGraceNanos.Load(); n > 0 {
		return time.Duration(n)
	}
	return 5 * time.Second
}

var runtimeHBlockedArgs = map[string]blockedArgMode{
	"--format":                       blockedWithValue,
	"--dir":                          blockedWithValue,
	"--variant":                      blockedWithValue,
	"--dangerously-skip-permissions": blockedStandalone,
}

type runtimeHBackend struct {
	cfg Config
}

func (b *runtimeHBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = runtimeCLIName("runtime-h")
	}
	resolved, err := exec.LookPath(execPath)
	if err != nil {
		return nil, fmt.Errorf("deveco executable not found at %q: %w", execPath, err)
	}
	if runtime.GOOS == "windows" {
		if native := resolveRuntimeHNativeFromShim(resolved, os.Stat); native != "" {
			b.cfg.Logger.Info("deveco resolved to native binary to avoid .cmd shim argv truncation", "shim", resolved, "native", native)
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

	if opts.MaxTurns > 0 {
		b.cfg.Logger.Warn("deveco does not support --max-turns; ignoring", "maxTurns", opts.MaxTurns)
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "--session", opts.ResumeSessionID)
	}
	args = append(args, filterCustomArgs(opts.CustomArgs, runtimeHBlockedArgs, b.cfg.Logger)...)
	args = append(args, prompt)

	cmd := newRuntimeCmd(exec.CommandContext(runCtx, execPath, args...))
	hideAgentWindow(cmd)

	configureProcessGroup(cmd)

	cmd.Cancel = func() error { return nil }
	b.cfg.logAgentCommand(cmd, newAgentCommandLogArgs(args))
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}

	env := buildEnv(b.cfg.Env)

	if opts.Cwd != "" {
		env = append(env, "PWD="+opts.Cwd)
	}
	cmd.Env = env

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("deveco stdout pipe: %w", err)
	}
	cmd.Stderr = newLogWriter(b.cfg.Logger, "[deveco:stderr] ")

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start deveco: %w", err)
	}

	b.cfg.Logger.Info("deveco started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

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
			case <-time.After(runtimeHTerminateGrace()):
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
			scanResult.errMsg = fmt.Sprintf("deveco timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			scanResult.status = "aborted"
			scanResult.errMsg = "execution cancelled"
		} else if exitErr != nil && scanResult.status == "completed" {
			scanResult.status = "failed"
			scanResult.errMsg = fmt.Sprintf("deveco exited with error: %v", exitErr)
		}

		b.cfg.Logger.Info("deveco finished", "pid", cmd.Process.Pid, "status", scanResult.status, "duration", duration.Round(time.Millisecond).String())

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

func resolveRuntimeHNativeFromShim(shimPath string, statFn func(string) (os.FileInfo, error)) string {
	if !strings.EqualFold(filepath.Ext(shimPath), ".cmd") {
		return ""
	}
	prefix := filepath.Dir(shimPath)
	scope := filepath.Join(prefix, "node_modules", "@deveco")
	candidates := []string{

		filepath.Join(scope, "deveco-code", "bin", "deveco.exe"),

		filepath.Join(scope, "deveco-code-windows-x64", "bin", "deveco.exe"),
		filepath.Join(scope, "deveco-code-windows-x64-baseline", "bin", "deveco.exe"),
	}
	for _, candidate := range candidates {
		if _, err := statFn(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

type runtimeHEventResult struct {
	status    string
	errMsg    string
	output    string
	sessionID string
	usage     TokenUsage
}

func (b *runtimeHBackend) processEvents(r io.Reader, ch chan<- Message) runtimeHEventResult {
	var output strings.Builder
	var sessionID string
	var usage TokenUsage
	finalStatus := "completed"
	var finalError string

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var event runtimeHEvent
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
		case "error":
			b.handleErrorEvent(event, ch, &finalStatus, &finalError)
		case "step_start":
			trySend(ch, Message{Type: MessageStatus, Status: "running"})
		case "step_finish":

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
		b.cfg.Logger.Warn("deveco stdout scanner error", "error", scanErr)
		if finalStatus == "completed" {
			finalStatus = "failed"
			finalError = fmt.Sprintf("stdout read error: %v", scanErr)
		}
	}

	return runtimeHEventResult{
		status:    finalStatus,
		errMsg:    finalError,
		output:    output.String(),
		sessionID: sessionID,
		usage:     usage,
	}
}

func (b *runtimeHBackend) handleTextEvent(event runtimeHEvent, ch chan<- Message, output *strings.Builder) {
	text := event.Part.Text
	if text != "" {
		output.WriteString(text)
		trySend(ch, Message{Type: MessageText, Content: text})
	}
}

func (b *runtimeHBackend) handleToolUseEvent(event runtimeHEvent, ch chan<- Message) {
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
		outputStr := extractRuntimeHToolOutput(event.Part.State.Output)
		trySend(ch, Message{
			Type:   MessageToolResult,
			Tool:   event.Part.Tool,
			CallID: event.Part.CallID,
			Output: outputStr,
		})
	}
}

func (b *runtimeHBackend) handleErrorEvent(event runtimeHEvent, ch chan<- Message, finalStatus, finalError *string) {
	errMsg := ""
	if event.Error != nil {
		errMsg = event.Error.Message()
	}
	if errMsg == "" {
		errMsg = "unknown deveco error"
	}

	b.cfg.Logger.Warn("deveco error event", "error", errMsg)
	trySend(ch, Message{Type: MessageError, Content: errMsg})

	*finalStatus = "failed"
	*finalError = errMsg
}

func extractRuntimeHToolOutput(output any) string {
	if output == nil {
		return ""
	}
	if s, ok := output.(string); ok {
		return s
	}
	data, _ := json.Marshal(output)
	return string(data)
}

type runtimeHEvent struct {
	Type      string            `json:"type"`
	Timestamp int64             `json:"timestamp,omitempty"`
	SessionID string            `json:"sessionID,omitempty"`
	Part      runtimeHEventPart `json:"part"`
	Error     *runtimeHError    `json:"error,omitempty"`
}

type runtimeHEventPart struct {
	ID        string `json:"id,omitempty"`
	MessageID string `json:"messageID,omitempty"`
	SessionID string `json:"sessionID,omitempty"`
	Type      string `json:"type,omitempty"`

	Text string `json:"text,omitempty"`

	Tool   string             `json:"tool,omitempty"`
	CallID string             `json:"callID,omitempty"`
	State  *runtimeHToolState `json:"state,omitempty"`

	Tokens *runtimeHTokens `json:"tokens,omitempty"`
}

type runtimeHTokens struct {
	Input  int64                `json:"input"`
	Output int64                `json:"output"`
	Cache  *runtimeHCacheTokens `json:"cache,omitempty"`
}

type runtimeHCacheTokens struct {
	Read  int64 `json:"read"`
	Write int64 `json:"write"`
}

type runtimeHToolState struct {
	Status string          `json:"status,omitempty"`
	Input  json.RawMessage `json:"input,omitempty"`
	Output any             `json:"output,omitempty"`
}

type runtimeHError struct {
	Name string           `json:"name,omitempty"`
	Data *runtimeHErrData `json:"data,omitempty"`
}

func (e *runtimeHError) Message() string {
	if e.Data != nil && e.Data.Message != "" {
		return e.Data.Message
	}
	if e.Name != "" {
		return e.Name
	}
	return ""
}

type runtimeHErrData struct {
	Message string `json:"message,omitempty"`
}
