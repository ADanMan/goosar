package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

var runtimeCTerminateGraceNanos atomic.Int64

func runtimeCTerminateGrace() time.Duration {
	if n := runtimeCTerminateGraceNanos.Load(); n > 0 {
		return time.Duration(n)
	}
	return 5 * time.Second
}

type runtimeCBackend struct {
	cfg Config
}

func (b *runtimeCBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = runtimeCLIName("runtime-c")
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("claude executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	args := buildRuntimeCArgs(opts, b.cfg.Logger)

	var mcpConfigPath string
	var mcpFileCleanup func()
	if hasManagedMcpConfig(opts.McpConfig) {
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

	configureProcessGroup(cmd)

	cmd.Cancel = func() error { return nil }
	b.cfg.logAgentCommand(cmd, newAgentCommandLogArgs(args))
	cmd.WaitDelay = 10 * time.Second
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)
	if err := runtimeCRootSudoPreflight(args, cmd.Env); err != nil {
		cancel()
		return nil, err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("claude stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("claude stdin pipe: %w", err)
	}
	var closeStdinOnce sync.Once
	closeStdin := func() { closeStdinOnce.Do(func() { _ = stdin.Close() }) }

	stderrBuf := newStderrTail(newLogWriter(b.cfg.Logger, "[claude:stderr] "), agentStderrTailBytes)
	cmd.Stderr = stderrBuf

	if err := cmd.Start(); err != nil {
		closeStdin()
		cancel()
		return nil, fmt.Errorf("start claude: %w", err)
	}

	b.cfg.Logger.Info("claude started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	mcpFileCleanup = nil

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	procDone := make(chan struct{})

	writeDone := make(chan error, 1)
	go func() {
		err := writeRuntimeCInput(stdin, prompt)
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
		sawAsyncLaunch := false
		usage := make(map[string]TokenUsage)
		eventCount := 0
		invalidEventCount := 0
		assistantEventCount := 0
		toolUseCount := 0

		go func() {
			select {
			case <-procDone:
				return
			case <-runCtx.Done():
			}
			closeStdin()
			if cmd.Process != nil {
				signalProcessGroup(cmd.Process, syscall.SIGTERM)

				if !waitProcessGroupGone(cmd.Process, runtimeCTerminateGrace()) {
					signalProcessGroup(cmd.Process, syscall.SIGKILL)
				}
			}
			_ = stdout.Close()
		}()

		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}

			var сообщение runtimeCSDKMessage
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
				if b.handleUser(сообщение, msgCh) {
					sawAsyncLaunch = true
				}
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
				if resultUsage := runtimeCResultUsage(сообщение, opts.Model); len(resultUsage) > 0 {
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
		close(procDone)
		duration := time.Since(startTime)

		writeErr := <-writeDone

		completionGuardError := ""
		if sawAsyncLaunch {
			completionGuardError = "claude launched an async background task; Goosar-managed runs require foreground execution"
		}
		finalStatus, finalOutput, finalError := finalizeStreamResult(
			"claude",
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
			completionGuardError,
		)

		stderrTail := stderrBuf.Tail()
		if finalError != "" {
			finalError = withAgentStderr(finalError, "claude", stderrTail)
		}
		logStreamProtocolObservation(b.cfg.Logger, streamProtocolObservation{
			provider:                   "claude",
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

		b.cfg.Logger.Info("claude finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		resumeRejected := resumeWasRejected(opts.ResumeSessionID, sessionID, finalStatus == "failed", finalError, stderrTail)
		reportedSessionID := resolveSessionID(opts.ResumeSessionID, sessionID, finalStatus == "failed", finalError, stderrTail)
		if resumeRejected {
			b.cfg.Logger.Info("claude resume was rejected; dropping session id and signalling fresh-session retry",
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

func (b *runtimeCBackend) handleAssistant(сообщение runtimeCSDKMessage, ch chan<- Message, usage map[string]TokenUsage) (string, int) {
	var content runtimeCMessageContent
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

func (b *runtimeCBackend) handleUser(сообщение runtimeCSDKMessage, ch chan<- Message) bool {
	var content runtimeCMessageContent
	if err := json.Unmarshal(сообщение.Message, &content); err != nil {
		return false
	}

	sawAsyncLaunch := false
	for _, block := range content.Content {
		if block.Type == "tool_result" {
			resultStr := ""
			if block.Content != nil {
				resultStr = string(block.Content)
				if runtimeCToolResultHasAsyncLaunch(block.Content) {
					sawAsyncLaunch = true
				}
			}
			trySend(ch, Message{
				Type:   MessageToolResult,
				CallID: block.ToolUseID,
				Output: resultStr,
			})
		}
	}
	return sawAsyncLaunch
}

func (b *runtimeCBackend) handleControlRequest(сообщение runtimeCSDKMessage, stdin interface{ Write([]byte) (int, error) }) {

	var req runtimeCControlRequestPayload
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
	if forceRuntimeCToolInputForeground(inputMap) {
		b.cfg.Logger.Info("claude: forced foreground tool execution",
			"request_id", сообщение.RequestID,
			"tool", req.ToolName,
		)
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
		b.cfg.Logger.Warn("claude: failed to marshal control response", "error", err)
		return
	}
	data = append(data, '\n')
	if _, err := stdin.Write(data); err != nil {
		b.cfg.Logger.Warn("claude: failed to write control response", "error", err)
	}
}

func forceRuntimeCToolInputForeground(input map[string]any) bool {
	if runInBackground, ok := input["run_in_background"].(bool); ok && runInBackground {
		input["run_in_background"] = false
		return true
	}
	return false
}

func runtimeCToolResultHasAsyncLaunch(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return false
	}
	switch v := value.(type) {
	case map[string]any:
		if runtimeCMapHasAsyncLaunchStatus(v) {
			return true
		}
		if content, ok := v["content"].([]any); ok {
			return runtimeCArrayHasAsyncLaunchStatus(content)
		}
	case []any:
		return runtimeCArrayHasAsyncLaunchStatus(v)
	}
	return false
}

func runtimeCArrayHasAsyncLaunchStatus(values []any) bool {
	for _, value := range values {
		if item, ok := value.(map[string]any); ok && runtimeCMapHasAsyncLaunchStatus(item) {
			return true
		}
	}
	return false
}

func runtimeCMapHasAsyncLaunchStatus(value map[string]any) bool {
	status, ok := value["status"].(string)
	return ok && status == "async_launched"
}

type runtimeCSDKMessage struct {
	Type      string          `json:"type"`
	Message   json.RawMessage `json:"message,omitempty"`
	Subtype   string          `json:"subtype,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
	Model     string          `json:"model,omitempty"`

	ResultText string                              `json:"result,omitempty"`
	IsError    bool                                `json:"is_error,omitempty"`
	DurationMs float64                             `json:"duration_ms,omitempty"`
	NumTurns   int                                 `json:"num_turns,omitempty"`
	Usage      *runtimeCUsage                      `json:"usage,omitempty"`
	ModelUsage map[string]runtimeCResultModelUsage `json:"modelUsage,omitempty"`

	Log *runtimeCLogEntry `json:"log,omitempty"`

	RequestID string          `json:"request_id,omitempty"`
	Request   json.RawMessage `json:"request,omitempty"`
}

type runtimeCLogEntry struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}

type runtimeCMessageContent struct {
	Role    string                 `json:"role"`
	Model   string                 `json:"model"`
	Content []runtimeCContentBlock `json:"content"`
	Usage   *runtimeCUsage         `json:"usage,omitempty"`
}

type runtimeCUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
}

type runtimeCResultModelUsage struct {
	InputTokens              int64 `json:"inputTokens"`
	OutputTokens             int64 `json:"outputTokens"`
	CacheReadInputTokens     int64 `json:"cacheReadInputTokens"`
	CacheCreationInputTokens int64 `json:"cacheCreationInputTokens"`
}

func runtimeCResultUsage(сообщение runtimeCSDKMessage, fallbackModel string) map[string]TokenUsage {
	if len(сообщение.ModelUsage) > 0 {
		usage := make(map[string]TokenUsage, len(сообщение.ModelUsage))
		for model, u := range сообщение.ModelUsage {
			if model == "" || !runtimeCUsageHasTokens(u.InputTokens, u.OutputTokens, u.CacheReadInputTokens, u.CacheCreationInputTokens) {
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
	if сообщение.Usage == nil || model == "" || !runtimeCUsageHasTokens(
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

func runtimeCUsageHasTokens(input, output, cacheRead, cacheWrite int64) bool {
	return input > 0 || output > 0 || cacheRead > 0 || cacheWrite > 0
}

type runtimeCContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
}

type runtimeCControlRequestPayload struct {
	Subtype  string          `json:"subtype"`
	ToolName string          `json:"tool_name,omitempty"`
	Input    json.RawMessage `json:"input,omitempty"`
}

func trySend(ch chan<- Message, сообщение Message) bool {
	select {
	case ch <- сообщение:
		return true
	default:
		return false
	}
}

var runtimeCBlockedArgs = map[string]blockedArgMode{
	"-p":                blockedStandalone,
	"--output-format":   blockedWithValue,
	"--input-format":    blockedWithValue,
	"--permission-mode": blockedWithValue,
	"--mcp-config":      blockedWithValue,

	"--effort": blockedWithValue,

	"--settings": blockedWithValue,
}

func buildRuntimeCArgs(opts ExecOptions, logger *slog.Logger) []string {
	args := []string{
		"-p",
		"--output-format", "stream-json",
		"--input-format", "stream-json",
		"--verbose",
		"--permission-mode", "bypassPermissions",

		"--disallowedTools", "AskUserQuestion",
	}
	if hasManagedMcpConfig(opts.McpConfig) {

		args = append(args, "--strict-mcp-config")
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

	args = append(args, filterCustomArgs(opts.ExtraArgs, runtimeCBlockedArgs, logger)...)
	args = append(args, filterCustomArgs(opts.CustomArgs, runtimeCBlockedArgs, logger)...)
	if opts.RuntimeCSettingsPath != "" {
		args = append(args, "--settings", opts.RuntimeCSettingsPath)
	}
	return args
}

func writeRuntimeCInput(w io.Writer, prompt string) error {
	data, err := buildRuntimeCInput(prompt)
	if err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		return err
	}
	return nil
}

func buildRuntimeCInput(prompt string) ([]byte, error) {
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
		return nil, fmt.Errorf("marshal claude input: %w", err)
	}
	return append(data, '\n'), nil
}

var resumeRejectedPhrases = []string{

	"no conversation found",

	"no saved session found",

	"已绑定另外",

	"bound to another account",
	"bound to a different account",
}

func resumeWasRejected(requestedResume, emitted string, failed bool, texts ...string) bool {
	if !failed || requestedResume == "" {
		return false
	}
	for _, text := range texts {
		lower := strings.ToLower(text)
		for _, phrase := range resumeRejectedPhrases {
			if strings.Contains(lower, phrase) {
				return true
			}
		}
	}

	return emitted != "" && emitted != requestedResume
}

func resolveSessionID(requestedResume, emitted string, failed bool, texts ...string) string {
	if resumeWasRejected(requestedResume, emitted, failed, texts...) {
		return ""
	}
	return emitted
}

func buildEnv(extra map[string]string) []string {
	return mergeEnv(os.Environ(), extra)
}

func runtimeCRootSudoPreflight(args, env []string) error {
	if !argsRequestBypassPermissions(args) || os.Geteuid() != 0 || envHasSandbox(env) {
		return nil
	}
	return fmt.Errorf("Claude Code refuses bypassPermissions under root/sudo privileges. Run the Goosar daemon as a non-root user, or set IS_SANDBOX=1 if running in a genuine container/sandbox")
}

func argsRequestBypassPermissions(args []string) bool {
	for i, arg := range args {
		if arg == "--dangerously-skip-permissions" {
			return true
		}
		if arg == "--permission-mode" && i+1 < len(args) && args[i+1] == "bypassPermissions" {
			return true
		}
	}
	return false
}

func envHasSandbox(env []string) bool {
	for i := len(env) - 1; i >= 0; i-- {
		key, value, ok := strings.Cut(env[i], "=")
		if key != "IS_SANDBOX" {
			continue
		}
		if !ok {
			return false
		}
		switch strings.ToLower(value) {
		case "1", "true", "yes", "on":
			return true
		default:
			return false
		}
	}
	return false
}

func mergeEnv(base []string, extra map[string]string) []string {
	env := make([]string, 0, len(base)+len(extra))
	position := make(map[string]int, len(base)+len(extra))
	set := func(key, entry string) {
		if i, ok := position[key]; ok {
			env[i] = entry
			return
		}
		position[key] = len(env)
		env = append(env, entry)
	}
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")

		if isFilteredChildEnvKey(key) || strings.HasPrefix(strings.ToUpper(key), "GOOSAR_") {
			continue
		}
		set(key, entry)
	}
	for k, v := range extra {
		set(k, k+"="+v)
	}
	return env
}

func isFilteredChildEnvKey(key string) bool {
	switch key {
	case "CLAUDECODE",
		"CLAUDE_CODE_ENTRYPOINT",
		"CLAUDE_CODE_EXECPATH",
		"CLAUDE_CODE_SESSION_ID",
		"CLAUDE_CODE_SSE_PORT":
		return true
	}

	return strings.HasPrefix(key, "CLAUDECODE_")
}

type blockedArgMode int

const (
	blockedWithValue blockedArgMode = iota
	blockedStandalone
	blockedOptionalValue
)

func filterCustomArgs(args []string, blocked map[string]blockedArgMode, logger *slog.Logger) []string {
	if len(args) == 0 {
		return args
	}
	filtered := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		raw := args[i]
		arg := unshellQuoteArg(raw)
		flag := arg
		hasInlineValue := false
		if idx := strings.Index(arg, "="); idx > 0 {
			flag = arg[:idx]
			hasInlineValue = true
		}
		mode, isBlocked := blocked[flag]
		if isBlocked {
			logger.Warn("custom_args: blocked protocol-critical flag, skipping", "flag", flag)
			if mode == blockedWithValue && !hasInlineValue {

				i++
			} else if mode == blockedOptionalValue && !hasInlineValue && i+1 < len(args) &&
				!strings.HasPrefix(unshellQuoteArg(args[i+1]), "-") {

				i++
			}
			continue
		}
		filtered = append(filtered, arg)
	}
	return filtered
}

func unshellQuoteArg(arg string) string {
	if strings.HasPrefix(arg, "-") {
		if idx := strings.Index(arg, "="); idx > 0 {
			value := arg[idx+1:]
			if unquoted, ok := stripSurroundingQuotes(value); ok {
				return arg[:idx+1] + unquoted
			}
			return arg
		}
	}
	if unquoted, ok := stripSurroundingQuotes(arg); ok {
		return unquoted
	}
	return arg
}

func stripSurroundingQuotes(s string) (string, bool) {
	if len(s) >= 2 {
		if (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"') {
			return s[1 : len(s)-1], true
		}
	}
	return s, false
}

func writeMcpConfigToTemp(raw json.RawMessage, logger *slog.Logger) (string, error) {
	dir, err := os.MkdirTemp("", "goosar-mcp-*")
	if err != nil {
		return "", fmt.Errorf("create mcp config temp dir: %w", err)
	}
	data, err := hardenBrowserMcpConfig(filterDisabledMcpServers(raw, logger), dir)
	if err != nil {
		cleanupMcpConfigTemp(filepath.Join(dir, "mcp-config.json"))
		return "", err
	}
	path := filepath.Join(dir, "mcp-config.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		cleanupMcpConfigTemp(path)
		return "", fmt.Errorf("write mcp config temp file: %w", err)
	}
	return path, nil
}

func cleanupMcpConfigTemp(path string) {
	if path == "" {
		return
	}
	dir := filepath.Dir(path)
	if strings.HasPrefix(filepath.Base(dir), "goosar-mcp-") {
		_ = os.RemoveAll(dir)
		return
	}
	_ = os.Remove(path)
}

var detectVersionTimeout = 10 * time.Second

func detectCLIVersion(ctx context.Context, execPath string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, detectVersionTimeout)
	defer cancel()

	cmd := newRuntimeCmd(exec.CommandContext(ctx, execPath, "--version"))
	hideAgentWindow(cmd)

	cmd.WaitDelay = 2 * time.Second
	data, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("detect version for %s: %w", execPath, err)
	}
	return extractVersionLine(string(data)), nil
}

func extractVersionLine(raw string) string {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if versionRe.MatchString(line) {
			return line
		}
	}
	return strings.TrimSpace(raw)
}

type logWriter struct {
	logger *slog.Logger
	prefix string
}

func newLogWriter(logger *slog.Logger, prefix string) *logWriter {
	return &logWriter{logger: logger, prefix: prefix}
}

func (w *logWriter) Write(p []byte) (int, error) {
	text := strings.TrimSpace(string(p))
	if text != "" {
		w.logger.Debug(w.prefix + text)
	}
	return len(p), nil
}
