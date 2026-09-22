package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type runtimeDBackend struct {
	cfg Config
}

var runtimeDBlockedArgs = map[string]blockedArgMode{
	"-p":                blockedStandalone,
	"--output-format":   blockedWithValue,
	"--input-format":    blockedWithValue,
	"--permission-mode": blockedWithValue,
	"--mcp-config":      blockedWithValue,

	"--effort": blockedWithValue,
}

func buildRuntimeDArgs(opts ExecOptions, logger *slog.Logger) []string {
	args := []string{
		"-p",
		"--output-format", "stream-json",
		"--input-format", "stream-json",
		"--verbose",
		"--strict-mcp-config",
		"--permission-mode", "bypassPermissions",
		"--disallowedTools", "AskUserQuestion",
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.ThinkingLevel != "" {
		args = append(args, "--effort", opts.ThinkingLevel)
	}
	if opts.MaxTurns > 0 {
		args = append(args, "--max-turns", fmt.Sprintf("%d", opts.MaxTurns))
	}
	if opts.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", opts.SystemPrompt)
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "--resume", opts.ResumeSessionID)
	}
	args = append(args, filterCustomArgs(opts.ExtraArgs, runtimeDBlockedArgs, logger)...)
	args = append(args, filterCustomArgs(opts.CustomArgs, runtimeDBlockedArgs, logger)...)
	return args
}

func (b *runtimeDBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = runtimeCLIName("runtime-d")
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("codebuddy executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	args := buildRuntimeDArgs(opts, b.cfg.Logger)

	var mcpConfigPath string
	var mcpFileCleanup func()
	if len(opts.McpConfig) > 0 {
		path, err := writeMcpConfigToTemp(opts.McpConfig, b.cfg.Logger)
		if err != nil {
			cancel()
			return nil, err
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
		return nil, fmt.Errorf("codebuddy stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("codebuddy stdin pipe: %w", err)
	}
	var closeStdinOnce sync.Once
	closeStdin := func() { closeStdinOnce.Do(func() { _ = stdin.Close() }) }

	stderrBuf := newStderrTail(newLogWriter(b.cfg.Logger, "[codebuddy:stderr] "), agentStderrTailBytes)
	cmd.Stderr = stderrBuf

	if err := cmd.Start(); err != nil {
		closeStdin()
		cancel()
		return nil, fmt.Errorf("start codebuddy: %w", err)
	}

	b.cfg.Logger.Info("codebuddy started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	mcpFileCleanup = nil

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	writeDone := make(chan error, 1)
	go func() {
		err := writeRuntimeDInput(stdin, prompt)
		if err != nil {
			closeStdin()
		}
		writeDone <- err
	}()

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)
		if mcpConfigPath != "" {
			defer cleanupMcpConfigTemp(mcpConfigPath)
		}

		startTime := time.Now()
		var lastAssistantText string
		var finalResultText string
		sawResult := false
		resultIsError := false
		var sessionID string
		usage := make(map[string]TokenUsage)
		eventCount := 0
		invalidEventCount := 0
		assistantEventCount := 0
		toolUseCount := 0

		go func() {
			<-runCtx.Done()
			closeStdin()
			_ = stdout.Close()
		}()

		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}

			var сообщение runtimeDSDKMessage
			if err := json.Unmarshal([]byte(line), &сообщение); err != nil {
				invalidEventCount++
				continue
			}
			eventCount++

			switch сообщение.Type {
			case "assistant":
				assistantEventCount++
				assistantText, tools := b.handleAssistant(сообщение, msgCh, usage)
				toolUseCount += tools
				if tools == 0 {
					lastAssistantText = assistantText
				} else {

					lastAssistantText = ""
				}
			case "user":
				b.handleUser(сообщение, msgCh)
			case "system":
				if сообщение.SessionID != "" {
					sessionID = сообщение.SessionID
				}
				trySend(msgCh, Message{Type: MessageStatus, Status: "running", SessionID: sessionID})
			case "result":
				sawResult = true
				finalResultText = сообщение.ResultText
				resultIsError = сообщение.IsError
				sessionID = сообщение.SessionID
				if resultUsage := runtimeDResultUsage(сообщение, opts.Model); len(resultUsage) > 0 {
					usage = resultUsage
				}
				closeStdin()
			case "log":
				if сообщение.Log != nil {
					trySend(msgCh, Message{
						Type:    MessageLog,
						Level:   сообщение.Log.Level,
						Content: сообщение.Log.Message,
					})
				}
			case "control_request":
				b.handleControlRequest(сообщение, stdin)
			}
		}
		scanErr := scanner.Err()
		if scanErr != nil {

			_ = stdout.Close()
		}

		closeStdin()

		exitErr := cmd.Wait()
		duration := time.Since(startTime)

		writeErr := <-writeDone

		finalStatus, finalOutput, finalError := finalizeStreamResult(
			"codebuddy",
			timeout,
			runCtx.Err(),
			writeErr,
			exitErr,
			sessionID,
			streamTerminalState{
				lastAssistantText: lastAssistantText,
				finalResultText:   finalResultText,
				sawResult:         sawResult,
				resultIsError:     resultIsError,
				scanErr:           scanErr,
			},
			"",
		)

		if finalError != "" {
			finalError = withAgentStderr(finalError, "codebuddy", stderrBuf.Tail())
		}
		logStreamProtocolObservation(b.cfg.Logger, streamProtocolObservation{
			provider:                   "codebuddy",
			cliVersion:                 b.cfg.CLIVersion,
			model:                      opts.Model,
			exitCode:                   streamProcessExitCode(exitErr),
			eventCount:                 eventCount,
			invalidEventCount:          invalidEventCount,
			assistantEventCount:        assistantEventCount,
			toolUseCount:               toolUseCount,
			sawResult:                  sawResult,
			resultIsError:              resultIsError,
			resultBytes:                len(finalResultText),
			lastAssistantBytes:         len(lastAssistantText),
			scannerError:               scanErr != nil,
			anthropicBaseURLConfigured: strings.TrimSpace(b.cfg.Env["ANTHROPIC_BASE_URL"]) != "",
		})

		b.cfg.Logger.Info("codebuddy finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		resumeRejected := resumeWasRejected(opts.ResumeSessionID, sessionID, finalStatus == "failed", finalError)
		reportedSessionID := resolveSessionID(opts.ResumeSessionID, sessionID, finalStatus == "failed", finalError)
		if resumeRejected {
			b.cfg.Logger.Info("codebuddy resume was rejected; dropping session id and signalling fresh-session retry",
				"requested_resume", opts.ResumeSessionID,
				"emitted_session", sessionID,
			)
		}

		resCh <- Result{
			Status:         finalStatus,
			Output:         finalOutput,
			Error:          finalError,
			DurationMs:     duration.Milliseconds(),
			SessionID:      reportedSessionID,
			Usage:          usage,
			ResumeRejected: resumeRejected,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

func (b *runtimeDBackend) handleAssistant(сообщение runtimeDSDKMessage, ch chan<- Message, usage map[string]TokenUsage) (string, int) {
	var content runtimeDMessageContent
	if err := json.Unmarshal(сообщение.Message, &content); err != nil {
		return "", 0
	}
	var assistantText strings.Builder
	toolUseCount := 0

	if content.Usage != nil && content.Model != "" {
		u := usage[content.Model]
		u.InputTokens += content.Usage.InputTokens
		u.OutputTokens += content.Usage.OutputTokens
		u.CacheReadTokens += content.Usage.CacheReadInputTokens
		u.CacheWriteTokens += content.Usage.CacheCreationInputTokens
		usage[content.Model] = u
	}

	for _, block := range content.Content {
		switch block.Type {
		case "text":
			if block.Text != "" {
				assistantText.WriteString(block.Text)
				trySend(ch, Message{Type: MessageText, Content: block.Text})
			}
		case "thinking":
			if block.Text != "" {
				trySend(ch, Message{Type: MessageThinking, Content: block.Text})
			}
		case "tool_use":
			toolUseCount++
			var input map[string]any
			if block.Input != nil {
				_ = json.Unmarshal(block.Input, &input)
			}
			trySend(ch, Message{
				Type:   MessageToolUse,
				Tool:   block.Name,
				CallID: block.ID,
				Input:  input,
			})
		}
	}
	return assistantText.String(), toolUseCount
}

func (b *runtimeDBackend) handleUser(сообщение runtimeDSDKMessage, ch chan<- Message) {
	var content runtimeDMessageContent
	if err := json.Unmarshal(сообщение.Message, &content); err != nil {
		return
	}

	for _, block := range content.Content {
		if block.Type == "tool_result" {
			resultStr := ""
			if block.Content != nil {
				resultStr = string(block.Content)
			}
			trySend(ch, Message{
				Type:   MessageToolResult,
				CallID: block.ToolUseID,
				Output: resultStr,
			})
		}
	}
}

func (b *runtimeDBackend) handleControlRequest(сообщение runtimeDSDKMessage, stdin interface{ Write([]byte) (int, error) }) {

	var req runtimeDControlRequestPayload
	if err := json.Unmarshal(сообщение.Request, &req); err != nil {
		return
	}

	var inputMap map[string]any
	if req.Input != nil {
		_ = json.Unmarshal(req.Input, &inputMap)
	}
	if inputMap == nil {
		inputMap = map[string]any{}
	}

	response := map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"subtype":    "success",
			"request_id": сообщение.RequestID,
			"response": map[string]any{
				"behavior":     "allow",
				"updatedInput": inputMap,
			},
		},
	}

	data, err := json.Marshal(response)
	if err != nil {
		b.cfg.Logger.Warn("codebuddy: failed to marshal control response", "error", err)
		return
	}
	data = append(data, '\n')
	if _, err := stdin.Write(data); err != nil {
		b.cfg.Logger.Warn("codebuddy: failed to write control response", "error", err)
	}
}

func writeRuntimeDInput(w io.Writer, prompt string) error {
	payload := map[string]any{
		"type": "user",
		"message": map[string]any{
			"role": "user",
			"content": []map[string]string{
				{
					"type": "text",
					"text": prompt,
				},
			},
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal codebuddy input: %w", err)
	}
	data = append(data, '\n')
	if _, err := w.Write(data); err != nil {
		return err
	}
	return nil
}

type runtimeDSDKMessage struct {
	Type      string          `json:"type"`
	Message   json.RawMessage `json:"message,omitempty"`
	Subtype   string          `json:"subtype,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
	Model     string          `json:"model,omitempty"`

	ResultText string                              `json:"result,omitempty"`
	IsError    bool                                `json:"is_error,omitempty"`
	DurationMs float64                             `json:"duration_ms,omitempty"`
	NumTurns   int                                 `json:"num_turns,omitempty"`
	Usage      *runtimeDUsage                      `json:"usage,omitempty"`
	ModelUsage map[string]runtimeDResultModelUsage `json:"modelUsage,omitempty"`

	Log *runtimeDLogEntry `json:"log,omitempty"`

	RequestID string          `json:"request_id,omitempty"`
	Request   json.RawMessage `json:"request,omitempty"`
}

type runtimeDLogEntry struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}

type runtimeDMessageContent struct {
	Role    string                 `json:"role"`
	Model   string                 `json:"model"`
	Content []runtimeDContentBlock `json:"content"`
	Usage   *runtimeDUsage         `json:"usage,omitempty"`
}

type runtimeDUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
}

type runtimeDResultModelUsage struct {
	InputTokens              int64 `json:"inputTokens"`
	OutputTokens             int64 `json:"outputTokens"`
	CacheReadInputTokens     int64 `json:"cacheReadInputTokens"`
	CacheCreationInputTokens int64 `json:"cacheCreationInputTokens"`
}

type runtimeDContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
}

type runtimeDControlRequestPayload struct {
	Subtype  string          `json:"subtype"`
	ToolName string          `json:"tool_name,omitempty"`
	Input    json.RawMessage `json:"input,omitempty"`
}

func runtimeDResultUsage(сообщение runtimeDSDKMessage, fallbackModel string) map[string]TokenUsage {
	if len(сообщение.ModelUsage) > 0 {
		usage := make(map[string]TokenUsage, len(сообщение.ModelUsage))
		for model, u := range сообщение.ModelUsage {
			if model == "" || !runtimeDUsageHasTokens(u.InputTokens, u.OutputTokens, u.CacheReadInputTokens, u.CacheCreationInputTokens) {
				continue
			}
			usage[model] = TokenUsage{
				InputTokens:      u.InputTokens,
				OutputTokens:     u.OutputTokens,
				CacheReadTokens:  u.CacheReadInputTokens,
				CacheWriteTokens: u.CacheCreationInputTokens,
			}
		}
		if len(usage) > 0 {
			return usage
		}
	}

	model := сообщение.Model
	if model == "" {
		model = fallbackModel
	}
	if сообщение.Usage == nil || model == "" || !runtimeDUsageHasTokens(
		сообщение.Usage.InputTokens,
		сообщение.Usage.OutputTokens,
		сообщение.Usage.CacheReadInputTokens,
		сообщение.Usage.CacheCreationInputTokens,
	) {
		return nil
	}
	return map[string]TokenUsage{
		model: {
			InputTokens:      сообщение.Usage.InputTokens,
			OutputTokens:     сообщение.Usage.OutputTokens,
			CacheReadTokens:  сообщение.Usage.CacheReadInputTokens,
			CacheWriteTokens: сообщение.Usage.CacheCreationInputTokens,
		},
	}
}

func runtimeDUsageHasTokens(input, output, cacheRead, cacheWrite int64) bool {
	return input > 0 || output > 0 || cacheRead > 0 || cacheWrite > 0
}
