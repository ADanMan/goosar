package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type runtimeOBackend struct {
	cfg Config
}

var runtimeOControlTokenRE = regexp.MustCompile(`<\|[A-Za-z0-9_-]+>[A-Za-z0-9_-]*|<[A-Za-z0-9_-]+\|>`)

func stripRuntimeOToolCallMarkup(s string) string {
	s = stripRuntimeOStructuredToolMarkup(s)
	return runtimeOControlTokenRE.ReplaceAllString(s, "")
}

func drainRuntimeOTextBuffer(buf *strings.Builder, delta string) string {
	buf.WriteString(delta)
	emit, pending := drainRuntimeOSanitizedText(buf.String())
	buf.Reset()
	buf.WriteString(pending)
	return emit
}

func flushRuntimeOTextBuffer(buf *strings.Builder) string {
	s := buf.String()
	buf.Reset()
	emit, pending := drainRuntimeOSanitizedText(s)
	emit += runtimeOControlTokenRE.ReplaceAllString(pending, "")
	return emit
}

func drainRuntimeOSanitizedText(s string) (string, string) {
	var out strings.Builder
	for i := 0; i < len(s); {
		start, prefixLen := nextRuntimeOToolMarkupPrefix(s, i)
		if start == -1 {
			safeLen := safeRuntimeOTextEmitLen(s[i:])
			out.WriteString(s[i : i+safeLen])
			return runtimeOControlTokenRE.ReplaceAllString(out.String(), ""), s[i+safeLen:]
		}
		out.WriteString(s[i:start])
		end, ok := scanRuntimeOToolMarkupEnd(s, start+prefixLen)
		if !ok {
			return runtimeOControlTokenRE.ReplaceAllString(out.String(), ""), s[start:]
		}
		i = end
	}
	return runtimeOControlTokenRE.ReplaceAllString(out.String(), ""), ""
}

func stripRuntimeOStructuredToolMarkup(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); {
		start, prefixLen := nextRuntimeOToolMarkupPrefix(s, i)
		if start == -1 {
			out.WriteString(s[i:])
			break
		}
		out.WriteString(s[i:start])
		end, ok := scanRuntimeOToolMarkupEnd(s, start+prefixLen)
		if !ok {
			out.WriteString(s[start:])
			break
		}
		i = end
	}
	return out.String()
}

func safeRuntimeOTextEmitLen(s string) int {
	hold := 0
	for _, prefix := range []string{"call:", "response:"} {
		for n := 1; n < len(prefix) && n <= len(s); n++ {
			if strings.HasSuffix(s, prefix[:n]) && n > hold {
				hold = n
			}
		}
	}
	if i := strings.LastIndexByte(s, '<'); i >= 0 && looksLikeRuntimeOControlTokenPrefix(s[i:]) {
		if len(s)-i > hold {
			hold = len(s) - i
		}
	}
	return len(s) - hold
}

func looksLikeRuntimeOControlTokenPrefix(s string) bool {
	if len(s) == 0 || s[0] != '<' || len(s) > 64 {
		return false
	}
	for i := 1; i < len(s); i++ {
		b := s[i]
		if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_' || b == '-' || b == '|' || b == '>' {
			continue
		}
		return false
	}
	return true
}

func nextRuntimeOToolMarkupPrefix(s string, from int) (int, int) {
	best := -1
	bestLen := 0
	for _, prefix := range []string{"call:", "response:"} {
		if i := strings.Index(s[from:], prefix); i >= 0 {
			abs := from + i
			if best == -1 || abs < best {
				best = abs
				bestLen = len(prefix)
			}
		}
	}
	return best, bestLen
}

func scanRuntimeOToolMarkupEnd(s string, i int) (int, bool) {
	nameStart := i
	for i < len(s) && isRuntimeOToolNameByte(s[i]) {
		i++
	}
	if i == nameStart || i >= len(s) || s[i] != '{' {
		return 0, false
	}

	const quoteMarker = `<|"|>`
	depth := 0
	inQuote := false
	for i < len(s) {
		if strings.HasPrefix(s[i:], quoteMarker) {
			inQuote = !inQuote
			i += len(quoteMarker)
			continue
		}

		if !inQuote {
			switch s[i] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					i++
					if strings.HasPrefix(s[i:], "<tool_call|>") {
						i += len("<tool_call|>")
					}
					return i, true
				}
			}
		}
		i++
	}
	return 0, false
}

func isRuntimeOToolNameByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_' || b == '-'
}

func (b *runtimeOBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execName := b.cfg.ExecutablePath
	if execName == "" {
		execName = runtimeCLIName("runtime-o")
	}
	lookedUp, err := exec.LookPath(execName)
	if err != nil {
		return nil, fmt.Errorf("pi executable not found at %q: %w", execName, err)
	}

	timeout := opts.Timeout

	sessionPath := opts.ResumeSessionID
	if sessionPath == "" {
		p, err := newRuntimeOSessionPath()
		if err != nil {
			return nil, fmt.Errorf("pi session path: %w", err)
		}
		sessionPath = p
	}
	if err := ensureRuntimeOSessionFile(sessionPath); err != nil {
		return nil, fmt.Errorf("pi session file: %w", err)
	}

	runCtx, cancel := runContext(ctx, timeout)

	args := buildRuntimeOArgs(prompt, sessionPath, opts, b.cfg.Logger)
	argv0, cmdArgs := chooseRuntimeOInvocation(execName, lookedUp, args, b.cfg.Logger)

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
		return nil, fmt.Errorf("pi stdout pipe: %w", err)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("pi stdin pipe: %w", err)
	}
	cmd.Stderr = newLogWriter(b.cfg.Logger, "[pi:stderr] ")

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		cancel()
		return nil, fmt.Errorf("start pi: %w", err)
	}
	_ = stdin.Close()

	b.cfg.Logger.Info("pi started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

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
		var output strings.Builder
		finalStatus := "completed"
		var finalError string
		usage := make(map[string]TokenUsage)

		scanner := bufio.NewScanner(stdout)

		scanner.Buffer(make([]byte, 0, 1024*1024), 32*1024*1024)
		var textBuffer strings.Builder

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var evt runtimeOStreamEvent
			if err := json.Unmarshal([]byte(line), &evt); err != nil {
				continue
			}

			switch evt.Type {
			case "agent_start":
				trySend(msgCh, Message{Type: MessageStatus, Status: "running"})

			case "turn_start":
				output.Reset()
				textBuffer.Reset()

			case "message_update":
				if evt.AssistantMessageEvent == nil {
					continue
				}
				switch evt.AssistantMessageEvent.Type {
				case "text_delta":
					if d := drainRuntimeOTextBuffer(&textBuffer, evt.AssistantMessageEvent.Delta); d != "" {
						output.WriteString(d)
						trySend(msgCh, Message{Type: MessageText, Content: d})
					}
				case "thinking_delta":
					if d := evt.AssistantMessageEvent.Delta; d != "" {
						trySend(msgCh, Message{Type: MessageThinking, Content: d})
					}
				}

			case "tool_execution_start":
				var params map[string]any
				if len(evt.Args) > 0 {
					_ = json.Unmarshal(evt.Args, &params)
				}
				trySend(msgCh, Message{
					Type:   MessageToolUse,
					Tool:   evt.ToolName,
					CallID: evt.ToolCallID,
					Input:  params,
				})

			case "tool_execution_end":
				trySend(msgCh, Message{
					Type:   MessageToolResult,
					CallID: evt.ToolCallID,
					Output: decodeRuntimeOResult(evt.Result),
				})

			case "turn_end":
				if сообщение := decodeRuntimeOMessage(evt.Message); сообщение != nil && сообщение.Usage != nil {
					model := сообщение.Model
					if model == "" {
						model = opts.Model
					}
					if model == "" {
						model = "unknown"
					}
					u := usage[model]
					u.InputTokens += сообщение.Usage.Input
					u.OutputTokens += сообщение.Usage.Output
					u.CacheReadTokens += сообщение.Usage.CacheRead
					u.CacheWriteTokens += сообщение.Usage.CacheWrite
					usage[model] = u
				}

			case "error":
				errText := decodeRuntimeOString(evt.Message)
				trySend(msgCh, Message{Type: MessageError, Content: errText})
				if finalStatus == "completed" {
					finalStatus = "failed"
					finalError = errText
				}

			case "auto_retry_end":
				if !evt.Success && finalStatus == "completed" {
					finalStatus = "failed"
					if evt.FinalError != "" {
						finalError = evt.FinalError
					} else {
						finalError = "pi exhausted automatic retries"
					}
				}
			}
		}
		if d := flushRuntimeOTextBuffer(&textBuffer); d != "" {
			output.WriteString(d)
			trySend(msgCh, Message{Type: MessageText, Content: d})
		}

		waitErr := cmd.Wait()
		duration := time.Since(startTime)

		if runCtx.Err() == context.DeadlineExceeded {
			finalStatus = "timeout"
			finalError = fmt.Sprintf("pi timed out after %s", timeout)
		} else if runCtx.Err() == context.Canceled {
			finalStatus = "aborted"
			finalError = "execution cancelled"
		} else if waitErr != nil && finalStatus == "completed" {
			finalStatus = "failed"
			finalError = fmt.Sprintf("pi exited with error: %v", waitErr)
		}

		b.cfg.Logger.Info("pi finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		resCh <- Result{
			Status:     finalStatus,
			Output:     output.String(),
			Error:      finalError,
			DurationMs: duration.Milliseconds(),
			SessionID:  sessionPath,
			Usage:      usage,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

type runtimeOStreamEvent struct {
	Type string `json:"type"`

	AssistantMessageEvent *runtimeOAssistantMessageEvent `json:"assistantMessageEvent,omitempty"`

	ToolCallID string          `json:"toolCallId,omitempty"`
	ToolName   string          `json:"toolName,omitempty"`
	Args       json.RawMessage `json:"args,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"`
	IsError    bool            `json:"isError,omitempty"`

	Message json.RawMessage `json:"message,omitempty"`

	Success    bool   `json:"success,omitempty"`
	FinalError string `json:"finalError,omitempty"`
}

type runtimeOAssistantMessageEvent struct {
	Type  string `json:"type"`
	Delta string `json:"delta,omitempty"`
}

type runtimeOMessage struct {
	Role  string         `json:"role,omitempty"`
	Model string         `json:"model,omitempty"`
	Usage *runtimeOUsage `json:"usage,omitempty"`
}

type runtimeOUsage struct {
	Input       int64 `json:"input"`
	Output      int64 `json:"output"`
	CacheRead   int64 `json:"cacheRead"`
	CacheWrite  int64 `json:"cacheWrite"`
	TotalTokens int64 `json:"totalTokens"`
}

func decodeRuntimeOMessage(raw json.RawMessage) *runtimeOMessage {
	if len(raw) == 0 {
		return nil
	}
	var m runtimeOMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return &m
}

func decodeRuntimeOString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return strings.Trim(string(raw), `"`)
}

func decodeRuntimeOResult(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}

var runtimeOBlockedArgs = map[string]blockedArgMode{
	"-p":        blockedStandalone,
	"--print":   blockedStandalone,
	"--mode":    blockedWithValue,
	"--session": blockedWithValue,
}

func buildRuntimeOArgs(prompt, sessionPath string, opts ExecOptions, logger *slog.Logger) []string {
	args := []string{
		"-p",
		"--mode", "json",
	}
	if sessionPath != "" {
		args = append(args, "--session", sessionPath)
	}
	if opts.Model != "" {
		provider, model := splitRuntimeOModel(opts.Model)
		if provider != "" {
			args = append(args, "--provider", provider)
		}
		if model != "" {
			args = append(args, "--model", model)
		}
	}

	if opts.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", opts.SystemPrompt)
	}
	args = append(args, filterCustomArgs(opts.CustomArgs, runtimeOBlockedArgs, logger)...)
	args = append(args, prompt)
	return args
}

func splitRuntimeOModel(s string) (provider, model string) {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "/"); i >= 0 {
		return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:])
	}
	return "", s
}

func runtimeOSessionDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".goosar", "pi-sessions"), nil
}

func newRuntimeOSessionPath() (string, error) {
	dir, err := runtimeOSessionDir()
	if err != nil {
		return "", err
	}
	name := fmt.Sprintf("%s.jsonl", time.Now().UTC().Format("20060102T150405.000000000"))
	return filepath.Join(dir, name), nil
}

func ensureRuntimeOSessionFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	return f.Close()
}

func RuntimeOSessionDir() (string, error) {
	return runtimeOSessionDir()
}
