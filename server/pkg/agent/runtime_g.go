package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type runtimeGBackend struct {
	cfg Config
}

func (b *runtimeGBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execName := b.cfg.ExecutablePath
	if execName == "" {
		execName = runtimeCLIName("runtime-g")
	}
	lookedUp, err := exec.LookPath(execName)
	if err != nil {
		return nil, fmt.Errorf("cursor-agent executable not found at %q: %w", execName, err)
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	args := buildRuntimeGArgs(opts, b.cfg.Logger)
	argv0, cmdArgs := chooseRuntimeGInvocation(execName, lookedUp, args, b.cfg.Logger)

	cmd := newRuntimeCmd(exec.CommandContext(runCtx, argv0, cmdArgs...))
	hideAgentWindow(cmd)
	b.cfg.logAgentCommand(cmd, newAgentCommandLogArgs(args))
	cmd.WaitDelay = 500 * time.Millisecond
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("cursor stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("cursor stdin pipe: %w", err)
	}
	var closeStdinOnce sync.Once
	closeStdin := func() { closeStdinOnce.Do(func() { _ = stdin.Close() }) }
	stderrBuf := newStderrTail(newLogWriter(b.cfg.Logger, "[cursor:stderr] "), agentStderrTailBytes)
	cmd.Stderr = stderrBuf

	if err := cmd.Start(); err != nil {
		closeStdin()
		cancel()
		return nil, fmt.Errorf("start cursor-agent: %w", err)
	}

	b.cfg.Logger.Info("cursor-agent started", "pid", cmd.Process.Pid, "cwd", opts.Cwd, "model", opts.Model)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	writeErrCh := make(chan error, 1)
	go func() {
		_, err := io.WriteString(stdin, prompt)
		closeStdin()
		writeErrCh <- err
	}()

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		go func() {
			<-runCtx.Done()
			closeStdin()
			_ = stdout.Close()
		}()

		startTime := time.Now()
		configuredModel := strings.TrimSpace(opts.Model)
		var output strings.Builder
		var sessionID string
		finalStatus := "completed"
		var finalError string
		var protocolError string
		resultSeen := false
		resultIsError := false
		resultBytes := 0
		eventCount := 0
		invalidEventCount := 0
		assistantEventCount := 0
		toolUseCount := 0

		unknownSubtypeCount := 0
		lastEventType := "none"

		stepUsage := make(map[string]TokenUsage)
		resultUsage := make(map[string]TokenUsage)
		hasResultUsage := false
		var thinking runtimeGThinkingStream

		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

		for scanner.Scan() {
			raw := scanner.Text()
			line := normalizeRuntimeGStreamLine(raw)
			if line == "" {
				continue
			}

			var evt runtimeGStreamEvent
			if err := json.Unmarshal([]byte(line), &evt); err != nil {
				invalidEventCount++
				continue
			}
			eventCount++
			lastEventType = observedRuntimeGEventType(evt.Type)

			if sid := evt.readSessionID(); sid != "" {
				sessionID = sid
			}

			switch evt.Type {
			case "system":
				if evt.Subtype == "init" {
					trySend(msgCh, Message{Type: MessageStatus, Status: "running"})
				}
				if evt.Subtype == "error" {
					errMsg := runtimeGErrorText(&evt)
					if errMsg != "" {
						protocolError = errMsg
						trySend(msgCh, Message{Type: MessageError, Content: errMsg})
					}
				}

			case "assistant":
				assistantEventCount++
				b.handleRuntimeGAssistant(&evt, msgCh, &output)

			case "thinking":

				switch evt.Subtype {
				case "delta":
					if content := thinking.delta(evt.Text); content != "" {
						trySend(msgCh, Message{Type: MessageThinking, Content: content})
					}
				case "completed":
					thinking.complete()
				default:
					unknownSubtypeCount++
				}

			case "tool_call":

				switch evt.Subtype {
				case "started":
					call := parseRuntimeGToolCall(&evt)
					toolUseCount++
					trySend(msgCh, Message{
						Type:   MessageToolUse,
						Tool:   call.Name,
						CallID: call.CallID,
						Input:  call.Input,
					})
				case "completed":
					call := parseRuntimeGToolCall(&evt)
					trySend(msgCh, Message{
						Type:   MessageToolResult,
						Tool:   call.Name,
						CallID: call.CallID,
						Output: call.Result,
					})
				default:
					unknownSubtypeCount++
				}

			case "tool_use":
				toolUseCount++
				var params map[string]any
				if evt.Parameters != nil {
					_ = json.Unmarshal(evt.Parameters, &params)
				}
				trySend(msgCh, Message{
					Type:   MessageToolUse,
					Tool:   evt.ToolName,
					CallID: evt.ToolID,
					Input:  params,
				})

			case "tool_result":
				trySend(msgCh, Message{
					Type:   MessageToolResult,
					CallID: evt.ToolID,
					Output: evt.Output,
				})

			case "result":
				resultSeen = true
				if evt.IsError || evt.Subtype == "error" {
					finalStatus = "failed"
					finalError = runtimeGErrorText(&evt)
					resultIsError = true
				}
				resultBytes = len(evt.ResultText)
				if evt.ResultText != "" && output.Len() == 0 {
					output.WriteString(evt.ResultText)
					trySend(msgCh, Message{Type: MessageText, Content: evt.ResultText})
				}
				b.accumulateResultUsage(resultUsage, &evt, configuredModel)
				if evt.hasResultUsage() {
					hasResultUsage = true
				}

				cancel()

			case "error":
				errMsg := runtimeGErrorText(&evt)
				if errMsg != "" {
					protocolError = errMsg
				}
				trySend(msgCh, Message{Type: MessageError, Content: errMsg})

			case "text":
				if evt.Part != nil {
					var part runtimeGTextPart
					_ = json.Unmarshal(evt.Part, &part)
					if part.Text != "" {
						output.WriteString(part.Text)
						trySend(msgCh, Message{Type: MessageText, Content: part.Text})
					}
				}

			case "step_finish":
				if evt.Part != nil {
					var part runtimeGStepFinishPart
					_ = json.Unmarshal(evt.Part, &part)
					model := runtimeGUsageModel(evt.Model, configuredModel)
					u := stepUsage[model]
					u.InputTokens += int64(part.Tokens.Input)
					u.OutputTokens += int64(part.Tokens.Output)
					u.CacheReadTokens += int64(part.Tokens.Cache.Read)
					stepUsage[model] = u
				}
			}
		}
		scanErr := scanner.Err()
		if scanErr != nil {

			_ = stdout.Close()
		}

		if !hasResultUsage {
			resultUsage = stepUsage
		}

		exitErr := cmd.Wait()
		duration := time.Since(startTime)

		writeErr := <-writeErrCh

		if resultSeen {

			if finalStatus == "failed" && finalError == "" {
				finalError = "cursor-agent returned an error result without details"
			}
		} else {
			switch {
			case runCtx.Err() == context.DeadlineExceeded:
				finalStatus = "timeout"
				finalError = fmt.Sprintf("cursor-agent timed out after %s", timeout)
			case runCtx.Err() == context.Canceled:
				finalStatus = "aborted"
				finalError = "execution cancelled"
			case scanErr != nil:
				finalStatus = "failed"
				finalError = fmt.Sprintf("cursor-agent stdout read error: %v", scanErr)
			case protocolError != "":
				finalStatus = "failed"
				finalError = protocolError
			case writeErr != nil && exitErr != nil:

				finalStatus = "failed"
				finalError = fmt.Sprintf("cursor-agent prompt write failed: %v", writeErr)
			case exitErr != nil:
				finalStatus = "failed"
				finalError = fmt.Sprintf("cursor-agent exited with error: %v", exitErr)
			default:

				finalStatus = "failed"
				finalError = "cursor-agent stream ended without terminal result"
				if writeErr != nil {
					finalError += fmt.Sprintf(" (prompt write: %v)", writeErr)
				}
			}
		}

		if finalError != "" {
			finalError = sanitizeAgentDiagnostic(finalError)
		}
		if finalStatus == "failed" && !resultSeen {
			finalError = runtimeGFailureDiagnostic(
				finalError,
				exitErr,
				scanErr,
				eventCount,
				invalidEventCount,
				lastEventType,
			)
			finalError = withAgentStderr(finalError, "cursor", sanitizeAgentDiagnostic(stderrBuf.Tail()))
		}

		logStreamProtocolObservation(b.cfg.Logger, streamProtocolObservation{
			provider:            "cursor-agent",
			cliVersion:          b.cfg.CLIVersion,
			model:               opts.Model,
			exitCode:            streamProcessExitCode(exitErr),
			eventCount:          eventCount,
			invalidEventCount:   invalidEventCount,
			assistantEventCount: assistantEventCount,
			toolUseCount:        toolUseCount,
			sawResult:           resultSeen,
			resultIsError:       resultIsError,
			resultBytes:         resultBytes,
			lastAssistantBytes:  output.Len(),
			scannerError:        scanErr != nil && !resultSeen,
			lastEventType:       lastEventType,
		})

		if unknownSubtypeCount > 0 {

			b.cfg.Logger.Warn("cursor-agent ignored unknown event subtypes", "count", unknownSubtypeCount)
		}

		b.cfg.Logger.Info("cursor-agent finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		finalOutput := output.String()
		if finalStatus != "completed" {

			finalOutput = ""
		}

		resCh <- Result{
			Status:     finalStatus,
			Output:     finalOutput,
			Error:      finalError,
			DurationMs: duration.Milliseconds(),
			SessionID:  sessionID,
			Usage:      resultUsage,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

const runtimeGIncompleteFinalizationWarning = "actions completed before finalization may already have taken effect"

func runtimeGFailureDiagnostic(сообщение string, exitErr, scanErr error, eventCount, invalidEventCount int, lastEventType string) string {
	return fmt.Sprintf(
		"%s (result_seen=false, exit_code=%d, scanner_error=%t, event_count=%d, invalid_event_count=%d, last_event_type=%s); %s",
		сообщение,
		streamProcessExitCode(exitErr),
		scanErr != nil,
		eventCount,
		invalidEventCount,
		lastEventType,
		runtimeGIncompleteFinalizationWarning,
	)
}

func observedRuntimeGEventType(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	if len(value) > 64 {
		return "invalid"
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return "invalid"
	}
	return value
}

func (b *runtimeGBackend) handleRuntimeGAssistant(evt *runtimeGStreamEvent, ch chan<- Message, output *strings.Builder) {
	if evt.Message == nil {
		return
	}

	var content runtimeGAssistantMessage
	if err := json.Unmarshal(evt.Message, &content); err != nil {
		return
	}

	for _, block := range content.Content {
		switch block.Type {
		case "output_text", "text":
			if block.Text != "" {
				output.WriteString(block.Text)
				trySend(ch, Message{Type: MessageText, Content: block.Text})
			}
		case "thinking":
			if block.Text != "" {
				trySend(ch, Message{Type: MessageThinking, Content: block.Text})
			}
		case "tool_use":
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
}

type runtimeGThinkingStream struct {
	blockOpen bool
	anySent   bool
}

func (t *runtimeGThinkingStream) delta(text string) string {
	if text == "" {
		return ""
	}
	if !t.blockOpen && t.anySent {
		text = "\n\n" + text
	}
	t.blockOpen = true
	t.anySent = true
	return text
}

func (t *runtimeGThinkingStream) complete() {
	t.blockOpen = false
}

type runtimeGToolCall struct {
	Name   string
	CallID string
	Input  map[string]any
	Result string
}

const runtimeGToolCallKeySuffix = "ToolCall"

func parseRuntimeGToolCall(evt *runtimeGStreamEvent) runtimeGToolCall {
	call := runtimeGToolCall{CallID: runtimeGCallID(evt.CallID)}

	var envelope map[string]json.RawMessage
	if len(evt.ToolCall) == 0 || json.Unmarshal(evt.ToolCall, &envelope) != nil {
		return call
	}
	if call.CallID == "" {
		var nestedID string
		if err := json.Unmarshal(envelope["toolCallId"], &nestedID); err == nil {
			call.CallID = runtimeGCallID(nestedID)
		}
	}

	key := runtimeGToolPayloadKey(envelope)
	if key == "" {
		return call
	}
	call.Name = strings.TrimSuffix(key, runtimeGToolCallKeySuffix)

	var payload struct {
		Args   map[string]any  `json:"args"`
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(envelope[key], &payload); err != nil {
		return call
	}
	call.Input = payload.Args
	if len(payload.Result) > 0 {
		call.Result = string(payload.Result)
	}
	return call
}

func runtimeGToolPayloadKey(envelope map[string]json.RawMessage) string {
	var keys []string
	for key := range envelope {
		if len(key) > len(runtimeGToolCallKeySuffix) && strings.HasSuffix(key, runtimeGToolCallKeySuffix) {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return ""
	}
	sort.Strings(keys)
	return keys[0]
}

func runtimeGCallID(raw string) string {
	id := strings.TrimSpace(raw)
	if idx := strings.IndexByte(id, '\n'); idx >= 0 {
		id = id[:idx]
	}
	return strings.TrimSpace(id)
}

func runtimeGUsageModel(evtModel, configuredModel string) string {
	if model := strings.TrimSpace(evtModel); model != "" {
		return model
	}
	if model := strings.TrimSpace(configuredModel); model != "" {
		return model
	}
	return "cursor"
}

func (b *runtimeGBackend) accumulateResultUsage(usage map[string]TokenUsage, evt *runtimeGStreamEvent, configuredModel string) {
	model := runtimeGUsageModel(evt.Model, configuredModel)
	u := usage[model]

	if evt.InputTokens != 0 || evt.OutputTokens != 0 || evt.CacheReadTokens != 0 || evt.CacheWriteTokens != 0 {
		u.InputTokens += evt.InputTokens
		u.OutputTokens += evt.OutputTokens
		u.CacheReadTokens += evt.CacheReadTokens
		u.CacheWriteTokens += evt.CacheWriteTokens
	} else if evt.Usage != nil {
		u.InputTokens += evt.Usage.InputTokens
		u.OutputTokens += evt.Usage.OutputTokens
		u.CacheReadTokens += evt.Usage.CacheReadInputTokens
		u.CacheWriteTokens += evt.Usage.CacheWriteInputTokens
	} else {
		return
	}

	usage[model] = u
}

type runtimeGStreamEvent struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Model     string `json:"model,omitempty"`

	Message json.RawMessage `json:"message,omitempty"`

	Text string `json:"text,omitempty"`

	ToolCall json.RawMessage `json:"tool_call,omitempty"`
	CallID   string          `json:"call_id,omitempty"`

	ToolName   string          `json:"tool_name,omitempty"`
	ToolID     string          `json:"tool_id,omitempty"`
	Parameters json.RawMessage `json:"parameters,omitempty"`

	Output string `json:"output,omitempty"`

	ResultText       string         `json:"result,omitempty"`
	IsError          bool           `json:"is_error,omitempty"`
	InputTokens      int64          `json:"inputTokens,omitempty"`
	OutputTokens     int64          `json:"outputTokens,omitempty"`
	CacheReadTokens  int64          `json:"cacheReadTokens,omitempty"`
	CacheWriteTokens int64          `json:"cacheWriteTokens,omitempty"`
	Usage            *runtimeGUsage `json:"usage,omitempty"`

	ErrorMsg string `json:"error,omitempty"`
	Detail   string `json:"detail,omitempty"`

	Part json.RawMessage `json:"part,omitempty"`
}

func (evt *runtimeGStreamEvent) readSessionID() string {
	if s := strings.TrimSpace(evt.SessionID); s != "" {
		return s
	}
	return ""
}

func (evt *runtimeGStreamEvent) hasResultUsage() bool {
	return evt.Usage != nil || evt.InputTokens != 0 || evt.OutputTokens != 0 || evt.CacheReadTokens != 0 || evt.CacheWriteTokens != 0
}

type runtimeGUsage struct {
	InputTokens           int64 `json:"input_tokens"`
	OutputTokens          int64 `json:"output_tokens"`
	CacheReadInputTokens  int64 `json:"cached_input_tokens"`
	CacheWriteInputTokens int64
}

func (u *runtimeGUsage) UnmarshalJSON(data []byte) error {
	var raw struct {
		InputTokensSnake              int64 `json:"input_tokens"`
		InputTokensCamel              int64 `json:"inputTokens"`
		OutputTokensSnake             int64 `json:"output_tokens"`
		OutputTokensCamel             int64 `json:"outputTokens"`
		CachedInputTokensSnake        int64 `json:"cached_input_tokens"`
		CachedInputTokensCamel        int64 `json:"cachedInputTokens"`
		CacheReadTokensCamel          int64 `json:"cacheReadTokens"`
		CacheReadInputTokensSnake     int64 `json:"cache_read_input_tokens"`
		CacheReadInputTokensCamel     int64 `json:"cacheReadInputTokens"`
		CacheWriteTokensCamel         int64 `json:"cacheWriteTokens"`
		CacheCreationInputTokensSnake int64 `json:"cache_creation_input_tokens"`
		CacheCreationInputTokensCamel int64 `json:"cacheCreationInputTokens"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	u.InputTokens = firstNonZeroInt64(raw.InputTokensSnake, raw.InputTokensCamel)
	u.OutputTokens = firstNonZeroInt64(raw.OutputTokensSnake, raw.OutputTokensCamel)
	u.CacheReadInputTokens = firstNonZeroInt64(
		raw.CachedInputTokensSnake,
		raw.CachedInputTokensCamel,
		raw.CacheReadTokensCamel,
		raw.CacheReadInputTokensSnake,
		raw.CacheReadInputTokensCamel,
	)
	u.CacheWriteInputTokens = firstNonZeroInt64(
		raw.CacheWriteTokensCamel,
		raw.CacheCreationInputTokensSnake,
		raw.CacheCreationInputTokensCamel,
	)
	return nil
}

func firstNonZeroInt64(values ...int64) int64 {
	for _, v := range values {
		if v != 0 {
			return v
		}
	}
	return 0
}

type runtimeGAssistantMessage struct {
	Model   string                 `json:"model"`
	Content []runtimeGContentBlock `json:"content"`
	Usage   *runtimeGUsage         `json:"usage,omitempty"`
}

type runtimeGContentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

type runtimeGTextPart struct {
	Text string `json:"text"`
}

type runtimeGStepFinishPart struct {
	Tokens struct {
		Input  int `json:"input"`
		Output int `json:"output"`
		Cache  struct {
			Read int `json:"read"`
		} `json:"cache"`
	} `json:"tokens"`
}

func normalizeRuntimeGStreamLine(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	if idx := runtimeGStreamPrefixRe.FindStringIndex(trimmed); idx != nil {
		return strings.TrimSpace(trimmed[idx[1]:])
	}
	return trimmed
}

var runtimeGStreamPrefixRe = regexp.MustCompile(`^(?i)(stdout|stderr)\s*[:=]?\s*`)

func runtimeGErrorText(evt *runtimeGStreamEvent) string {
	if evt.ErrorMsg != "" {
		return evt.ErrorMsg
	}
	if evt.Detail != "" {
		return evt.Detail
	}
	if evt.ResultText != "" {
		return evt.ResultText
	}
	return ""
}

var runtimeGBlockedArgs = map[string]blockedArgMode{
	"-p":              blockedStandalone,
	"--output-format": blockedWithValue,
	"--yolo":          blockedStandalone,
}

func buildRuntimeGArgs(opts ExecOptions, logger *slog.Logger) []string {
	args := []string{
		"-p",
		"--output-format", "stream-json",
		"--yolo",
	}
	if opts.Cwd != "" {
		args = append(args, "--workspace", opts.Cwd)
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}

	if opts.ResumeSessionID != "" {
		args = append(args, "--resume", opts.ResumeSessionID)
	}
	args = append(args, filterCustomArgs(opts.CustomArgs, runtimeGBlockedArgs, logger)...)
	return args
}
