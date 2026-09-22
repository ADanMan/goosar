package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const runtimeNNoParseableOutput = "openclaw returned no parseable output"

const minRuntimeNVersion = "2026.5.5"

var runtimeNVersionPattern = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

var runtimeNBlockedArgs = map[string]blockedArgMode{
	"--local":         blockedStandalone,
	"--json":          blockedStandalone,
	"--session-id":    blockedWithValue,
	"--message":       blockedWithValue,
	"--model":         blockedWithValue,
	"--system-prompt": blockedWithValue,
}

type runtimeNBackend struct {
	cfg Config
}

func (b *runtimeNBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = runtimeCLIName("runtime-n")
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("openclaw executable not found at %q: %w", execPath, err)
	}

	if err := checkRuntimeNVersion(ctx, execPath); err != nil {
		return nil, err
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	sessionID := opts.ResumeSessionID
	if sessionID == "" {
		sessionID = fmt.Sprintf("goosar-%d", time.Now().UnixNano())
	}
	args := buildRuntimeNArgs(prompt, sessionID, opts, b.cfg.Logger)

	cmd := newRuntimeCmd(exec.CommandContext(runCtx, execPath, args...))
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
		return nil, fmt.Errorf("openclaw stdout pipe: %w", err)
	}
	cmd.Stderr = newLogWriter(b.cfg.Logger, "[openclaw:stderr] ")

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start openclaw: %w", err)
	}

	b.cfg.Logger.Info("openclaw started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	go func() {
		<-runCtx.Done()
		_ = stdout.Close()
	}()

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		startTime := time.Now()
		scanResult := b.processOutput(stdout, msgCh)

		exitErr := cmd.Wait()
		duration := time.Since(startTime)

		if runCtx.Err() == context.DeadlineExceeded {
			scanResult.status = "timeout"
			scanResult.errMsg = fmt.Sprintf("openclaw timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			scanResult.status = "aborted"
			scanResult.errMsg = "execution cancelled"
		} else if exitErr != nil && scanResult.status == "completed" {
			scanResult.status = "failed"
			scanResult.errMsg = fmt.Sprintf("openclaw exited with error: %v", exitErr)
		}

		b.cfg.Logger.Info("openclaw finished", "pid", cmd.Process.Pid, "status", scanResult.status, "duration", duration.Round(time.Millisecond).String())

		var usage map[string]TokenUsage
		u := scanResult.usage
		if u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheReadTokens > 0 || u.CacheWriteTokens > 0 {
			model := scanResult.model
			if model == "" {
				model = opts.Model
			}
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

func buildRuntimeNArgs(prompt, sessionID string, opts ExecOptions, logger *slog.Logger) []string {
	args := []string{"agent"}
	if opts.RuntimeNMode != "gateway" {
		args = append(args, "--local")
	}
	args = append(args, "--json", "--session-id", sessionID)
	if opts.Timeout > 0 {
		args = append(args, "--timeout", fmt.Sprintf("%d", int(opts.Timeout.Seconds())))
	}

	customArgs := filterCustomArgs(opts.CustomArgs, runtimeNBlockedArgs, logger)
	if opts.Model != "" && !customArgsContains(customArgs, "--agent") {
		args = append(args, "--agent", opts.Model)
	}
	args = append(args, customArgs...)

	if opts.SystemPrompt != "" {
		prompt = opts.SystemPrompt + "\n\n" + prompt
	}
	args = append(args, "--message", prompt)
	return args
}

func customArgsContains(args []string, flag string) bool {
	prefix := flag + "="
	for _, a := range args {
		if a == flag || strings.HasPrefix(a, prefix) {
			return true
		}
	}
	return false
}

func checkRuntimeNVersion(ctx context.Context, execPath string) error {
	cmd := newRuntimeCmd(exec.CommandContext(ctx, execPath, "--version"))
	hideAgentWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("openclaw --version failed: %w", err)
	}
	detected, ok := parseRuntimeNVersion(string(out))
	if !ok {
		return fmt.Errorf("could not parse openclaw version from output: %q", strings.TrimSpace(string(out)))
	}
	if compareRuntimeNVersion(detected, minRuntimeNVersion) < 0 {
		return fmt.Errorf("openclaw %s is below the minimum supported version %s. Run `openclaw update` to upgrade and try again.", detected, minRuntimeNVersion)
	}
	return nil
}

func parseRuntimeNVersion(raw string) (string, bool) {
	m := runtimeNVersionPattern.FindString(raw)
	if m == "" {
		return "", false
	}
	return m, true
}

func compareRuntimeNVersion(a, b string) int {
	aParts := strings.SplitN(a, ".", 3)
	bParts := strings.SplitN(b, ".", 3)
	for i := 0; i < 3; i++ {
		ai, _ := strconv.Atoi(aParts[i])
		bi, _ := strconv.Atoi(bParts[i])
		if ai < bi {
			return -1
		}
		if ai > bi {
			return 1
		}
	}
	return 0
}

type runtimeNEventResult struct {
	status    string
	errMsg    string
	output    string
	sessionID string
	usage     TokenUsage

	model string
}

func (b *runtimeNBackend) processOutput(r io.Reader, ch chan<- Message) runtimeNEventResult {
	buf, readErr := io.ReadAll(r)
	if readErr != nil {
		return runtimeNEventResult{status: "failed", errMsg: fmt.Sprintf("read stdout: %v", readErr)}
	}

	if result, ok := parseWholeBufferRuntimeNResult(buf); ok {
		var output strings.Builder
		return b.buildRuntimeNEventResult(result, ch, &output)
	}

	scanner := bufio.NewScanner(bytes.NewReader(buf))
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	var output strings.Builder
	var sessionID string
	var model string
	var usage TokenUsage
	finalStatus := "completed"
	var finalError string
	gotEvents := false

	var rawLines []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if event, ok := tryParseRuntimeNEvent(line); ok {
			gotEvents = true
			if event.SessionID != "" {
				sessionID = event.SessionID
			}
			switch event.Type {
			case "text":
				if event.Text != "" {
					output.WriteString(event.Text)
					trySend(ch, Message{Type: MessageText, Content: event.Text})
				}
			case "tool_use":
				var input map[string]any
				if event.Input != nil {
					_ = json.Unmarshal(event.Input, &input)
				}
				trySend(ch, Message{
					Type:   MessageToolUse,
					Tool:   event.Tool,
					CallID: event.CallID,
					Input:  input,
				})
			case "tool_result":
				trySend(ch, Message{
					Type:   MessageToolResult,
					Tool:   event.Tool,
					CallID: event.CallID,
					Output: event.Text,
				})
			case "error":
				errMsg := event.errorMessage()
				b.cfg.Logger.Warn("openclaw error event", "error", errMsg)
				trySend(ch, Message{Type: MessageError, Content: errMsg})
				finalStatus = "failed"
				finalError = errMsg
			case "lifecycle":
				phase := event.Phase
				if phase == "error" || phase == "failed" || phase == "cancelled" {
					errMsg := event.errorMessage()
					b.cfg.Logger.Warn("openclaw lifecycle failure", "phase", phase, "error", errMsg)
					trySend(ch, Message{Type: MessageError, Content: errMsg})
					finalStatus = "failed"
					finalError = errMsg
				}
			case "step_start":
				trySend(ch, Message{Type: MessageStatus, Status: "running"})
			case "step_finish":
				if event.Usage != nil {
					u := parseRuntimeNUsage(event.Usage)
					usage.InputTokens += u.InputTokens
					usage.OutputTokens += u.OutputTokens
					usage.CacheReadTokens += u.CacheReadTokens
					usage.CacheWriteTokens += u.CacheWriteTokens
				}
			}
			continue
		}

		if result, ok := tryParseRuntimeNResult(line); ok {
			gotEvents = true
			res := b.buildRuntimeNEventResult(result, ch, &output)
			if res.sessionID != "" {
				sessionID = res.sessionID
			}
			if res.model != "" {
				model = res.model
			}

			u := res.usage
			if u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheReadTokens > 0 || u.CacheWriteTokens > 0 {
				usage = u
			}
			continue
		}

		b.cfg.Logger.Debug("[openclaw:stdout] " + line)
		rawLines = append(rawLines, line)
	}

	if err := scanner.Err(); err != nil {
		return runtimeNEventResult{status: "failed", errMsg: fmt.Sprintf("read stdout: %v", err)}
	}

	if !gotEvents {
		trimmed := strings.TrimSpace(strings.Join(rawLines, "\n"))
		if trimmed != "" {
			return runtimeNEventResult{status: "completed", output: trimmed}
		}
		return runtimeNEventResult{
			status: "failed",
			errMsg: runtimeNNoParseableOutput,
		}
	}

	return runtimeNEventResult{
		status:    finalStatus,
		errMsg:    finalError,
		output:    output.String(),
		sessionID: sessionID,
		usage:     usage,
		model:     model,
	}
}

func parseWholeBufferRuntimeNResult(buf []byte) (runtimeNResult, bool) {
	trimmed := strings.TrimSpace(string(buf))
	if trimmed == "" {
		return runtimeNResult{}, false
	}
	if result, ok := tryParseRuntimeNResult(trimmed); ok {
		return result, true
	}

	lines := strings.Split(trimmed, "\n")
	for i, line := range lines {
		if len(line) > 0 && line[0] == '{' {
			candidate := strings.TrimSpace(strings.Join(lines[i:], "\n"))
			if result, ok := tryParseRuntimeNResult(candidate); ok {
				return result, true
			}
			return runtimeNResult{}, false
		}
	}
	return runtimeNResult{}, false
}

func tryParseRuntimeNEvent(line string) (runtimeNEvent, bool) {
	if len(line) == 0 || line[0] != '{' {
		return runtimeNEvent{}, false
	}
	var event runtimeNEvent
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		return runtimeNEvent{}, false
	}
	if event.Type == "" {
		return runtimeNEvent{}, false
	}
	return event, true
}

func tryParseRuntimeNResult(raw string) (runtimeNResult, bool) {
	if len(raw) == 0 || raw[0] != '{' {
		return runtimeNResult{}, false
	}
	var result runtimeNResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return runtimeNResult{}, false
	}
	if result.Payloads == nil && result.Meta.DurationMs == 0 {
		return runtimeNResult{}, false
	}
	return result, true
}

func (b *runtimeNBackend) buildRuntimeNEventResult(result runtimeNResult, ch chan<- Message, output *strings.Builder) runtimeNEventResult {
	for _, p := range result.Payloads {
		if p.Text != "" {
			output.WriteString(p.Text)
			trySend(ch, Message{Type: MessageText, Content: p.Text})
		}
	}

	var sessionID string
	var model string
	var usage TokenUsage
	if result.Meta.AgentMeta != nil {
		if sid, ok := result.Meta.AgentMeta["sessionId"].(string); ok {
			sessionID = sid
		}

		if m, ok := result.Meta.AgentMeta["model"].(string); ok {
			model = strings.TrimSpace(m)
		}
		if u, ok := result.Meta.AgentMeta["usage"].(map[string]any); ok {
			usage = parseRuntimeNUsage(u)
		}
	}

	return runtimeNEventResult{
		status:    "completed",
		output:    output.String(),
		sessionID: sessionID,
		usage:     usage,
		model:     model,
	}
}

func parseRuntimeNUsage(data map[string]any) TokenUsage {
	return TokenUsage{
		InputTokens:      runtimeNInt64FirstOf(data, "input", "inputTokens", "input_tokens"),
		OutputTokens:     runtimeNInt64FirstOf(data, "output", "outputTokens", "output_tokens"),
		CacheReadTokens:  runtimeNInt64FirstOf(data, "cacheRead", "cachedInputTokens", "cached_input_tokens", "cache_read", "cache_read_input_tokens"),
		CacheWriteTokens: runtimeNInt64FirstOf(data, "cacheWrite", "cacheCreationInputTokens", "cache_creation_input_tokens", "cache_write"),
	}
}

func runtimeNInt64FirstOf(data map[string]any, keys ...string) int64 {
	for _, key := range keys {
		if v := runtimeNInt64(data, key); v != 0 {
			return v
		}
	}
	return 0
}

func runtimeNInt64(data map[string]any, key string) int64 {
	v, ok := data[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	default:
		return 0
	}
}

type runtimeNEvent struct {
	Type      string          `json:"type"`
	SessionID string          `json:"sessionId,omitempty"`
	Text      string          `json:"text,omitempty"`
	Tool      string          `json:"tool,omitempty"`
	CallID    string          `json:"callId,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	Usage     map[string]any  `json:"usage,omitempty"`
	Phase     string          `json:"phase,omitempty"`
	Error     *runtimeNError  `json:"error,omitempty"`
	Message   string          `json:"message,omitempty"`
}

func (e runtimeNEvent) errorMessage() string {
	if e.Error != nil {
		if сообщение := e.Error.сообщение(); сообщение != "" {
			return сообщение
		}
	}
	if e.Text != "" {
		return e.Text
	}
	if e.Message != "" {
		return e.Message
	}
	return "unknown openclaw error"
}

type runtimeNError struct {
	Name    string             `json:"name,omitempty"`
	Data    *runtimeNErrorData `json:"data,omitempty"`
	Message string             `json:"message,omitempty"`
}

func (e *runtimeNError) сообщение() string {
	if e.Data != nil && e.Data.Message != "" {
		return e.Data.Message
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Name != "" {
		return e.Name
	}
	return ""
}

type runtimeNErrorData struct {
	Message string `json:"message,omitempty"`
}

type runtimeNResult struct {
	Payloads []runtimeNPayload `json:"payloads"`
	Meta     runtimeNMeta      `json:"meta"`
}

type runtimeNPayload struct {
	Text string `json:"text"`
}

type runtimeNMeta struct {
	DurationMs int64          `json:"durationMs"`
	AgentMeta  map[string]any `json:"agentMeta"`
}
