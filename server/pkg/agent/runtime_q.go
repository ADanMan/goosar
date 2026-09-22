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

type runtimeQBackend struct {
	cfg Config
}

var runtimeQBlockedArgs = map[string]blockedArgMode{
	"-p":                   blockedWithValue,
	"--prompt":             blockedWithValue,
	"-i":                   blockedWithValue,
	"--prompt-interactive": blockedWithValue,
	"-o":                   blockedWithValue,
	"--output-format":      blockedWithValue,
	"-m":                   blockedWithValue,
	"--model":              blockedWithValue,
	"-r":                   blockedWithValue,
	"--resume":             blockedWithValue,
	"-c":                   blockedStandalone,
	"--continue":           blockedStandalone,
	"--chat-recording":     blockedWithValue,
	"--mcp-config":         blockedWithValue,
	"--safe-mode":          blockedStandalone,
	"--yolo":               blockedStandalone,
	"-y":                   blockedStandalone,
	"--approval-mode":      blockedWithValue,
	"--core-tools":         blockedWithValue,
}

func buildRuntimeQArgs(prompt string, opts ExecOptions, logger *slog.Logger) []string {
	args := []string{"-p", prompt, "--output-format", "stream-json"}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "--resume", opts.ResumeSessionID)
	}

	args = append(args, "--yolo")
	args = append(args, filterCustomArgs(opts.ExtraArgs, runtimeQBlockedArgs, logger)...)
	args = append(args, filterCustomArgs(opts.CustomArgs, runtimeQBlockedArgs, logger)...)
	return args
}

func (b *runtimeQBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = runtimeCLIName("runtime-q")
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("qwen executable not found at %q: %w", execPath, err)
	}
	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)
	args := buildRuntimeQArgs(prompt, opts, b.cfg.Logger)

	var mcpConfigPath string
	var mcpFileCleanup func()
	if hasManagedMcpConfig(opts.McpConfig) {
		path, err := writeMcpConfigToTemp(opts.McpConfig, b.cfg.Logger)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("write qwen mcp_config: %w", err)
		}
		mcpConfigPath = path
		mcpFileCleanup = func() { cleanupMcpConfigTemp(mcpConfigPath) }
		args = append(args, "--mcp-config", mcpConfigPath)
	}

	defer func() {
		if mcpFileCleanup != nil {
			mcpFileCleanup()
		}
	}()
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
		return nil, fmt.Errorf("qwen stdout pipe: %w", err)
	}
	stderrBuf := newStderrTail(newLogWriter(b.cfg.Logger, "[qwen:stderr] "), agentStderrTailBytes)
	cmd.Stderr = stderrBuf
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start qwen: %w", err)
	}

	mcpFileCleanup = nil
	b.cfg.Logger.Info("qwen started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)
	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)
		if mcpConfigPath != "" {
			defer cleanupMcpConfigTemp(mcpConfigPath)
		}

		started := time.Now()
		state := runtimeQStreamState{model: opts.Model, usage: make(map[string]TokenUsage)}
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
			var event runtimeQStreamEvent
			if err := json.Unmarshal([]byte(line), &event); err != nil {
				state.invalidEventCount++
				continue
			}
			state.eventCount++
			state.lastEventType = event.Type
			handleRuntimeQEvent(event, msgCh, &state)
		}
		scanErr := scanner.Err()
		if scanErr != nil {
			_ = stdout.Close()
		}
		exitErr := cmd.Wait()
		duration := time.Since(started)

		status, output, errMsg := finalizeStreamResult("qwen", timeout, runCtx.Err(), nil, exitErr, state.sessionID, streamTerminalState{
			lastAssistantText: state.lastAssistantText,
			finalResultText:   state.finalResultText,
			sawResult:         state.sawResult,
			resultIsError:     state.resultIsError,
			scanErr:           scanErr,
		}, "")
		if errMsg != "" {
			errMsg = withAgentStderr(errMsg, "qwen", stderrBuf.Tail())
		}
		logStreamProtocolObservation(b.cfg.Logger, streamProtocolObservation{
			provider: "qwen", cliVersion: b.cfg.CLIVersion, model: state.model,
			exitCode: streamProcessExitCode(exitErr), eventCount: state.eventCount,
			invalidEventCount: state.invalidEventCount, assistantEventCount: state.assistantEventCount,
			toolUseCount: state.toolUseCount, sawResult: state.sawResult, resultIsError: state.resultIsError,
			resultBytes: len(state.finalResultText), lastAssistantBytes: len(state.lastAssistantText),
			scannerError: scanErr != nil, lastEventType: state.lastEventType,
		})
		b.cfg.Logger.Info("qwen finished", "pid", cmd.Process.Pid, "status", status, "duration", duration.Round(time.Millisecond).String())
		resCh <- Result{
			Status: status, Output: output, Error: errMsg, DurationMs: duration.Milliseconds(),
			SessionID: resolveSessionID(opts.ResumeSessionID, state.sessionID, status == "failed", errMsg), Usage: state.usage,
			ResumeRejected: resumeWasRejected(opts.ResumeSessionID, state.sessionID, status == "failed", errMsg),
		}
	}()
	return &Session{Messages: msgCh, Result: resCh}, nil
}

type runtimeQStreamEvent struct {
	Type      string          `json:"type"`
	Subtype   string          `json:"subtype,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
	Model     string          `json:"model,omitempty"`
	Message   json.RawMessage `json:"message,omitempty"`
	Result    string          `json:"result,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
	Usage     *runtimeQUsage  `json:"usage,omitempty"`
	Error     json.RawMessage `json:"error,omitempty"`
}

type runtimeQMessage struct {
	Model   string                 `json:"model,omitempty"`
	Content []runtimeQContentBlock `json:"content"`
	Usage   *runtimeQUsage         `json:"usage,omitempty"`
}

type runtimeQUsage struct {
	InputTokens          int64 `json:"input_tokens"`
	OutputTokens         int64 `json:"output_tokens"`
	CacheReadInputTokens int64 `json:"cache_read_input_tokens"`
}

type runtimeQContentBlock struct {
	Type      string          `json:"type"`
	Thinking  string          `json:"thinking,omitempty"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
}

type runtimeQStreamState struct {
	sessionID, model, lastAssistantText, finalResultText, lastEventType string
	sawResult, resultIsError                                            bool
	usage                                                               map[string]TokenUsage
	eventCount, invalidEventCount, assistantEventCount, toolUseCount    int
}

func handleRuntimeQEvent(event runtimeQStreamEvent, ch chan<- Message, state *runtimeQStreamState) {
	if event.SessionID != "" {
		state.sessionID = event.SessionID
	}
	if event.Model != "" {
		state.model = event.Model
	}
	switch event.Type {
	case "system":
		trySend(ch, Message{Type: MessageStatus, Status: "running", SessionID: state.sessionID})
	case "assistant":
		state.assistantEventCount++
		text, tools, model := handleRuntimeQAssistant(event.Message, ch, state.usage)
		if model != "" {
			state.model = model
		}
		state.toolUseCount += tools
		if tools == 0 && text != "" {
			state.lastAssistantText = text
		} else if tools > 0 {
			state.lastAssistantText = ""
		}
	case "user":
		handleRuntimeQUser(event.Message, ch)
	case "result":
		state.sawResult = true
		state.resultIsError = event.IsError || event.Subtype == "error" || event.Subtype == "failed"
		if state.resultIsError {

			state.finalResultText = runtimeQErrorText(event)
		} else {
			state.finalResultText = event.Result
		}
		if usage := runtimeQResultUsage(event.Usage, state.model); len(usage) > 0 {
			state.usage = usage
		}
	case "error":

		state.sawResult = true
		state.resultIsError = true
		state.finalResultText = runtimeQErrorText(event)
	}
}

func handleRuntimeQAssistant(raw json.RawMessage, ch chan<- Message, usage map[string]TokenUsage) (string, int, string) {
	var сообщение runtimeQMessage
	if json.Unmarshal(raw, &сообщение) != nil {
		return "", 0, ""
	}
	if сообщение.Usage != nil && сообщение.Model != "" {
		usage[сообщение.Model] = runtimeQTokenUsage(сообщение.Usage)
	}
	var text strings.Builder
	tools := 0
	for _, block := range сообщение.Content {
		switch block.Type {
		case "thinking":
			if block.Thinking != "" {
				trySend(ch, Message{Type: MessageThinking, Content: block.Thinking})
			}
		case "text":
			if block.Text != "" {
				text.WriteString(block.Text)
				trySend(ch, Message{Type: MessageText, Content: block.Text})
			}
		case "tool_use":
			tools++
			var input map[string]any
			if len(block.Input) > 0 {
				_ = json.Unmarshal(block.Input, &input)
			}
			trySend(ch, Message{Type: MessageToolUse, Tool: block.Name, CallID: block.ID, Input: input})
		}
	}
	return text.String(), tools, сообщение.Model
}

func handleRuntimeQUser(raw json.RawMessage, ch chan<- Message) {
	var сообщение runtimeQMessage
	if json.Unmarshal(raw, &сообщение) != nil {
		return
	}
	for _, block := range сообщение.Content {
		if block.Type == "tool_result" {
			trySend(ch, Message{Type: MessageToolResult, CallID: block.ToolUseID, Output: runtimeQToolResultOutput(block.Content)})
		}
	}
}

func runtimeQTokenUsage(usage *runtimeQUsage) TokenUsage {
	return TokenUsage{InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens, CacheReadTokens: usage.CacheReadInputTokens}
}

func runtimeQResultUsage(usage *runtimeQUsage, model string) map[string]TokenUsage {
	if usage == nil || model == "" || (usage.InputTokens == 0 && usage.OutputTokens == 0 && usage.CacheReadInputTokens == 0) {
		return nil
	}
	return map[string]TokenUsage{model: runtimeQTokenUsage(usage)}
}

func runtimeQToolResultOutput(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	return string(raw)
}

func runtimeQErrorText(event runtimeQStreamEvent) string {
	if event.Result != "" {
		return event.Result
	}
	var body struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(event.Error, &body) == nil && body.Message != "" {
		return body.Message
	}
	if len(event.Error) > 0 {
		return string(event.Error)
	}
	return "qwen returned an error event without details"
}
