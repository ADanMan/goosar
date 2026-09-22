package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var runtimeJBlockedArgs = map[string]blockedArgMode{
	"acp": blockedStandalone,
}

var runtimeJArgProfileRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

var runtimeJValueFlags = map[string]struct{}{
	"-z": {}, "--oneshot": {}, "-m": {}, "--model": {}, "--provider": {},
	"-t": {}, "--toolsets": {}, "-r": {}, "--resume": {}, "-s": {},
	"--skills": {}, "--usage-file": {},
}
var runtimeJOptionalValueFlags = map[string]struct{}{"-c": {}, "--continue": {}}

type RuntimeJProfileSelection struct {
	Name    string
	Found   bool
	Inline  bool
	ArgFrom int
	ArgLen  int
}

func ParseRuntimeJProfileArgs(args []string) RuntimeJProfileSelection {
	none := RuntimeJProfileSelection{ArgFrom: -1}
	i := 0
	for i < len(args) {
		arg := unshellQuoteArg(args[i])
		if arg == "--" {
			break
		}
		if arg == "--args" && runtimeJInsideMcpAdd(args, i) {
			break
		}
		if arg == "-p" || arg == "--profile" {
			if i+1 < len(args) {
				val := unshellQuoteArg(args[i+1])
				if !runtimeJArgProfileRe.MatchString(val) {
					return none
				}
				return RuntimeJProfileSelection{Name: val, Found: true, ArgFrom: i, ArgLen: 2}
			}
			return none
		}
		if v, ok := strings.CutPrefix(arg, "--profile="); ok {
			return RuntimeJProfileSelection{Name: v, Found: true, Inline: true, ArgFrom: i, ArgLen: 1}
		}
		if _, ok := runtimeJValueFlags[arg]; ok && i+1 < len(args) {
			i += 2
			continue
		}
		if _, ok := runtimeJOptionalValueFlags[arg]; ok && i+1 < len(args) &&
			!strings.HasPrefix(unshellQuoteArg(args[i+1]), "-") {
			i += 2
			continue
		}
		i++
	}
	return none
}

func runtimeJInsideMcpAdd(args []string, index int) bool {
	mcp := -1
	for j := 0; j < index; j++ {
		if unshellQuoteArg(args[j]) == "mcp" {
			mcp = j
			break
		}
	}
	if mcp < 0 {
		return false
	}
	for j := mcp + 1; j < index; j++ {
		if unshellQuoteArg(args[j]) == "add" {
			return true
		}
	}
	return false
}

func StripRuntimeJProfileArgs(args []string, sel RuntimeJProfileSelection) []string {
	if !sel.Found || sel.ArgFrom < 0 || sel.ArgLen <= 0 {
		return args
	}
	end := sel.ArgFrom + sel.ArgLen
	if end > len(args) {
		end = len(args)
	}
	out := make([]string, 0, len(args)-(end-sel.ArgFrom))
	out = append(out, args[:sel.ArgFrom]...)
	out = append(out, args[end:]...)
	return out
}

type runtimeJBackend struct {
	cfg Config

	provider string
}

func (b *runtimeJBackend) name() string {
	if b.provider == "" {
		return "runtime-j"
	}
	return b.provider
}

var (
	runtimeJReaderDrainGrace      = 2 * time.Second
	runtimeJNotificationQuietTime = 250 * time.Millisecond
)

func (b *runtimeJBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = runtimeCLIName("runtime-j")
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("hermes executable not found at %q: %w", execPath, err)
	}

	mcpServers, err := buildACPMcpServers(opts.McpConfig, b.cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("hermes: invalid mcp_config: %w", err)
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	runtimeJArgs := append([]string{"acp"}, filterCustomArgs(opts.CustomArgs, runtimeJBlockedArgs, b.cfg.Logger)...)
	cmd := newRuntimeCmd(exec.CommandContext(runCtx, execPath, runtimeJArgs...))
	hideAgentWindow(cmd)
	b.cfg.logAgentCommand(cmd, newAgentCommandLogArgs(runtimeJArgs, trustAgentCommandPositional(0, "acp")))
	agentsMDPresent := false
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
		if _, err := os.Stat(filepath.Join(opts.Cwd, "AGENTS.md")); err == nil {
			agentsMDPresent = true
		}
	}
	b.cfg.Logger.Info("hermes acp starting", "cwd", opts.Cwd, "agents_md_present", agentsMDPresent)
	if opts.SystemPrompt != "" {
		b.cfg.Logger.Debug("hermes ignoring ExecOptions.SystemPrompt; using cwd-scoped context files", "cwd", opts.Cwd)
	}

	env := buildEnv(b.cfg.Env)

	env = append(env, "HERMES_YOLO_MODE=1")
	cmd.Env = env

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("hermes stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("hermes stdin pipe: %w", err)
	}

	providerErr := newACPProviderErrorSniffer(b.name())
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("hermes stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start hermes: %w", err)
	}

	stderrSink := io.MultiWriter(newLogWriter(b.cfg.Logger, "["+b.name()+":stderr] "), providerErr)
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		_, _ = io.Copy(stderrSink, stderr)
	}()

	b.cfg.Logger.Info(b.name()+" acp started", "pid", cmd.Process.Pid, "cwd", opts.Cwd)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	var outputMu sync.Mutex
	var output strings.Builder

	var streamingCurrentTurn atomic.Bool

	var turnActivity atomic.Int64

	var droppedMessages atomic.Int64

	promptDone := make(chan runtimeJPromptResult, 1)
	activity := make(chan struct{}, 1)

	c := &runtimeJClient{
		cfg:          b.cfg,
		stdin:        stdin,
		pending:      make(map[int]*pendingRPC),
		pendingTools: make(map[string]*pendingToolCall),
		acceptNotification: func(string) bool {
			return streamingCurrentTurn.Load()
		},
		onActivity: func() {
			select {
			case activity <- struct{}{}:
			default:
			}
		},
		onMessage: func(сообщение Message) {
			if !streamingCurrentTurn.Load() {
				return
			}
			turnActivity.Add(1)
			if сообщение.Type == MessageText {
				outputMu.Lock()
				output.WriteString(сообщение.Content)
				outputMu.Unlock()
			}
			if !trySend(msgCh, сообщение) {

				if droppedMessages.Add(1) == 1 {
					b.cfg.Logger.Warn("live transcript falling behind: message channel full, dropping updates; the board will look stalled while the run continues",
						"backend", b.name(),
						"pid", cmd.Process.Pid,
						"channel_cap", cap(msgCh),
						"first_dropped_type", сообщение.Type,
					)
				}
			}
		},
		onPromptDone: func(result runtimeJPromptResult) {
			if !streamingCurrentTurn.Load() {
				return
			}
			select {
			case promptDone <- result:
			default:
			}
		},
	}

	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			c.handleLine(line)
		}
		c.closeAllPending(fmt.Errorf("hermes process exited"))
	}()

	go func() {
		defer close(msgCh)
		defer close(resCh)
		defer func() {
			stdin.Close()

			cancel()
			_ = cmd.Wait()
		}()

		startTime := time.Now()
		finalStatus := "completed"
		var finalError string
		var sessionID string

		var resumeRejected bool

		var promptStopReason string
		effectiveModel := strings.TrimSpace(opts.Model)

		var sessionCurrentModel string

		initResult, err := c.request(runCtx, "initialize", map[string]any{
			"protocolVersion": 1,
			"clientInfo": map[string]any{
				"name":    "goosar-agent-sdk",
				"version": "0.2.0",
			},
			"clientCapabilities": map[string]any{},
		})
		if err != nil {
			finalStatus = "failed"
			finalError = fmt.Sprintf("hermes initialize failed: %v", err)
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}

		mcpServers = filterACPMcpServersByCapability(mcpServers, extractACPMcpCapabilities(initResult), "hermes", b.cfg.Logger)

		cwd := opts.Cwd
		if cwd == "" {
			cwd = "."
		}

		if opts.ResumeSessionID != "" {

			result, err := c.request(runCtx, "session/resume", map[string]any{
				"cwd":        cwd,
				"sessionId":  opts.ResumeSessionID,
				"mcpServers": mcpServers,
			})
			if err != nil {
				finalStatus = "failed"
				finalError = fmt.Sprintf("hermes session/resume failed: %v", err)
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			var changed bool
			sessionID, changed = resolveResumedSessionID(opts.ResumeSessionID, result)
			if changed {
				b.cfg.Logger.Warn("agent returned a different session id on resume — original was likely lost; continuing with the new id",
					"backend", "hermes",
					"requested", opts.ResumeSessionID,
					"actual", sessionID,
				)
			}
			sessionCurrentModel = extractACPCurrentModelID(result)
			if effectiveModel == "" {
				effectiveModel = sessionCurrentModel
			}
		} else {
			result, err := c.request(runCtx, "session/new", buildRuntimeJSessionParams(cwd, opts.Model, mcpServers))
			if err != nil {
				finalStatus = "failed"
				finalError = fmt.Sprintf("hermes session/new failed: %v", err)
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			sessionID = extractACPSessionID(result)
			if sessionID == "" {
				finalStatus = "failed"
				finalError = "hermes session/new returned no session ID"
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			sessionCurrentModel = extractACPCurrentModelID(result)
			if effectiveModel == "" {
				effectiveModel = sessionCurrentModel
			}
		}

		c.sessionID = sessionID
		b.cfg.Logger.Info("hermes session created", "session_id", sessionID)

		if !trySend(msgCh, Message{Type: MessageStatus, Status: "running", SessionID: sessionID}) {
			b.cfg.Logger.Warn("dropped session-pin status message", "session_id", sessionID)
		}

		if opts.Model != "" && effectiveModel == sessionCurrentModel {
			b.cfg.Logger.Info("hermes session already on requested model; skipping redundant set_model",
				"model", opts.Model,
				"session_id", sessionID,
			)
		} else if opts.Model != "" {
			if _, err := c.request(runCtx, "session/set_model", map[string]any{
				"sessionId": sessionID,
				"modelId":   opts.Model,
			}); err != nil {
				b.cfg.Logger.Warn("hermes set_session_model failed", "error", err, "requested_model", opts.Model)
				finalStatus = "failed"
				finalError = fmt.Sprintf("hermes could not switch to model %q: %v", opts.Model, err)
				if opts.ResumeSessionID != "" && isACPSessionNotFound(err) {

					b.cfg.Logger.Warn("resumed session not found at set_model time; clearing session id so the daemon retries fresh",
						"backend", "hermes",
						"session_id", sessionID,
					)
					sessionID = ""
					resumeRejected = true
				}
				resCh <- Result{
					Status:         finalStatus,
					Error:          finalError,
					DurationMs:     time.Since(startTime).Milliseconds(),
					SessionID:      sessionID,
					ResumeRejected: resumeRejected,
				}
				return
			}
			b.cfg.Logger.Info("hermes session model set", "model", opts.Model)
		}

		streamingCurrentTurn.Store(true)
		_, err = c.request(runCtx, "session/prompt", map[string]any{
			"sessionId": sessionID,
			"prompt": []map[string]any{
				{"type": "text", "text": prompt},
			},
		})
		if err != nil {

			if runCtx.Err() == context.DeadlineExceeded {
				finalStatus = "timeout"
				finalError = fmt.Sprintf("hermes timed out after %s", timeout)
			} else if runCtx.Err() == context.Canceled {
				finalStatus = "aborted"
				finalError = "execution cancelled"
			} else {
				finalStatus = "failed"
				finalError = fmt.Sprintf("hermes session/prompt failed: %v", err)
				if opts.ResumeSessionID != "" {
					switch {
					case isACPSessionNotFound(err):

						b.cfg.Logger.Warn("resumed session not found at prompt time; clearing session id so the daemon retries fresh",
							"backend", "hermes",
							"session_id", sessionID,
						)
						sessionID = ""
						resumeRejected = true
					case isACPProviderBadRequest(err):

						b.cfg.Logger.Warn("provider rejected the resumed conversation (400) at prompt time; clearing session id so the daemon retries fresh",
							"backend", "hermes",
							"session_id", sessionID,
						)
						sessionID = ""
						resumeRejected = true
					}
				}
			}
		} else {

			select {
			case pr := <-promptDone:
				promptStopReason = pr.stopReason
				if pr.stopReason == "cancelled" {
					finalStatus = "aborted"
					finalError = "hermes cancelled the prompt"
				}

				c.usageMu.Lock()
				c.usage.InputTokens += pr.usage.InputTokens
				c.usage.OutputTokens += pr.usage.OutputTokens
				c.usage.CacheReadTokens += pr.usage.CacheReadTokens
				c.usageMu.Unlock()
			default:
			}
			waitForRuntimeJNotificationQuiescence(runCtx, activity, readerDone)
		}

		duration := time.Since(startTime)
		if dropped := droppedMessages.Load(); dropped > 0 {
			b.cfg.Logger.Warn("live transcript lost messages this run: the board showed less than the agent produced",
				"backend", b.name(),
				"pid", cmd.Process.Pid,
				"dropped", dropped,
				"channel_cap", cap(msgCh),
			)
		}
		b.cfg.Logger.Info(b.name()+" finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		stdin.Close()

		if !waitForRuntimeJPipeDrain(readerDone, stderrDone, runtimeJReaderDrainGrace) {
			b.cfg.Logger.Warn("hermes did not close output pipes after stdin EOF; forcing shutdown",
				"pid", cmd.Process.Pid,
				"grace", runtimeJReaderDrainGrace.String(),
			)
			cancel()
			<-readerDone
			<-stderrDone
		}
		streamingCurrentTurn.Store(false)

		outputMu.Lock()
		finalOutput := output.String()
		outputMu.Unlock()

		finalStatus, finalError = promoteACPResultOnProviderError(finalStatus, finalError, finalOutput, providerErr)

		if runtimeJResumeSessionLost(opts.ResumeSessionID, promptStopReason, turnActivity.Load()) {
			b.cfg.Logger.Warn("resumed session refused with no agent activity; treating it as gone and clearing the session id so the daemon retries fresh",
				"backend", "hermes",
				"session_id", sessionID,
			)
			if finalStatus == "completed" {
				finalStatus = "failed"
				finalError = runtimeJResumeLostError
			}
			sessionID = ""
			resumeRejected = true
		}

		c.usageMu.Lock()
		u := c.usage
		c.usageMu.Unlock()

		var usageMap map[string]TokenUsage
		if u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheReadTokens > 0 || u.CacheWriteTokens > 0 {
			model := effectiveModel
			if model == "" {
				model = "unknown"
			}
			usageMap = map[string]TokenUsage{model: u}
		}

		resCh <- Result{
			Status:         finalStatus,
			Output:         finalOutput,
			Error:          finalError,
			DurationMs:     duration.Milliseconds(),
			SessionID:      sessionID,
			ResumeRejected: resumeRejected,
			Usage:          usageMap,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

func waitForRuntimeJNotificationQuiescence(ctx context.Context, activity <-chan struct{}, readerDone <-chan struct{}) {
	quiet := time.NewTimer(runtimeJNotificationQuietTime)
	defer quiet.Stop()
	hard := time.NewTimer(runtimeJReaderDrainGrace)
	defer hard.Stop()

	for {
		select {
		case <-activity:
			if !quiet.Stop() {
				select {
				case <-quiet.C:
				default:
				}
			}
			quiet.Reset(runtimeJNotificationQuietTime)
		case <-quiet.C:
			return
		case <-readerDone:
			return
		case <-hard.C:
			return
		case <-ctx.Done():
			return
		}
	}
}

func waitForRuntimeJPipeDrain(readerDone, stderrDone <-chan struct{}, timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for readerDone != nil || stderrDone != nil {
		select {
		case <-readerDone:
			readerDone = nil
		case <-stderrDone:
			stderrDone = nil
		case <-timer.C:
			return false
		}
	}
	return true
}

type runtimeJPromptResult struct {
	stopReason string
	usage      TokenUsage

	modelID string
}

type runtimeJClient struct {
	cfg          Config
	stdin        interface{ Write([]byte) (int, error) }
	writeMu      sync.Mutex
	mu           sync.Mutex
	nextID       int
	pending      map[int]*pendingRPC
	sessionID    string
	onMessage    func(Message)
	onPromptDone func(runtimeJPromptResult)

	onActivity func()

	acceptNotification func(updateType string) bool

	toolMu       sync.Mutex
	pendingTools map[string]*pendingToolCall

	usageMu sync.Mutex
	usage   TokenUsage
}

type pendingToolCall struct {
	toolName string
	input    map[string]any
	argsText string
	emitted  bool
}

func (c *runtimeJClient) writeLine(data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err := c.stdin.Write(data)
	return err
}

func (c *runtimeJClient) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	id := c.nextID
	c.nextID++
	pr := &pendingRPC{ch: make(chan rpcResult, 1), method: method}
	c.pending[id] = pr
	c.mu.Unlock()

	сообщение := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}
	data, err := json.Marshal(сообщение)
	if err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}
	data = append(data, '\n')
	if err := c.writeLine(data); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("write %s: %w", method, err)
	}

	select {
	case res := <-pr.ch:
		return res.result, res.err
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	}
}

func (c *runtimeJClient) closeAllPending(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, pr := range c.pending {
		pr.ch <- rpcResult{err: err}
		delete(c.pending, id)
	}
}

func (c *runtimeJClient) handleLine(line string) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return
	}

	if _, hasID := raw["id"]; hasID {
		if _, hasResult := raw["result"]; hasResult {
			c.handleResponse(raw)
			return
		}
		if _, hasError := raw["error"]; hasError {
			c.handleResponse(raw)
			return
		}
		if _, hasMethod := raw["method"]; hasMethod {
			c.handleAgentRequest(raw)
			return
		}
	}

	if _, hasMethod := raw["method"]; hasMethod {
		c.handleNotification(raw)
	}
}

func (c *runtimeJClient) handleAgentRequest(raw map[string]json.RawMessage) {
	var method string
	_ = json.Unmarshal(raw["method"], &method)

	rawID, ok := raw["id"]
	if !ok {
		return
	}

	var resp map[string]any
	switch method {
	case "session/request_permission":
		optionID, grant, ok := selectACPPermissionOption(raw["params"])
		if ok {

			resp = map[string]any{
				"jsonrpc": "2.0",
				"id":      json.RawMessage(rawID),
				"result": map[string]any{
					"outcome": map[string]any{
						"outcome":  "selected",
						"optionId": optionID,
					},
				},
			}
			if grant {
				c.cfg.Logger.Debug("auto-approved agent permission request", "method", method, "optionId", optionID)
			} else {
				c.cfg.Logger.Warn("no safe grant offered; selecting offered reject option", "method", method, "optionId", optionID)
			}
		} else {

			resp = map[string]any{
				"jsonrpc": "2.0",
				"id":      json.RawMessage(rawID),
				"error": map[string]any{
					"code":    -32603,
					"message": "no auto-selectable permission option offered",
				},
			}
			c.cfg.Logger.Warn("no safely selectable permission option offered; returning error", "method", method)
		}
	default:

		resp = map[string]any{
			"jsonrpc": "2.0",
			"id":      json.RawMessage(rawID),
			"error": map[string]any{
				"code":    -32601,
				"message": "method not found: " + method,
			},
		}
		c.cfg.Logger.Debug("unhandled agent→client request", "method", method)
	}

	data, err := json.Marshal(resp)
	if err != nil {
		c.cfg.Logger.Warn("marshal agent-request response", "method", method, "error", err)
		return
	}
	data = append(data, '\n')
	if err := c.writeLine(data); err != nil {
		c.cfg.Logger.Warn("write agent-request response", "method", method, "error", err)
	}
}

type acpPermissionOption struct {
	OptionID string `json:"optionId"`
	Kind     string `json:"kind"`
}

const (
	acpKindAllowOnce   = "allow_once"
	acpKindAllowAlways = "allow_always"
	acpKindRejectOnce  = "reject_once"
)

var acpSessionScopedOptionIDs = []string{"allow_session", "approve_for_session"}

func selectACPPermissionOption(params json.RawMessage) (optionID string, grant bool, ok bool) {
	var p struct {
		Options []acpPermissionOption `json:"options"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return "", false, false
		}
	}

	for _, want := range acpSessionScopedOptionIDs {
		for _, opt := range p.Options {
			if opt.OptionID == want && isACPGrantKind(opt.Kind) {
				return opt.OptionID, true, true
			}
		}
	}

	for _, opt := range p.Options {
		if opt.OptionID != "" && strings.EqualFold(strings.TrimSpace(opt.Kind), acpKindAllowOnce) {
			return opt.OptionID, true, true
		}
	}

	for _, opt := range p.Options {
		if opt.OptionID != "" && strings.EqualFold(strings.TrimSpace(opt.Kind), acpKindRejectOnce) {
			return opt.OptionID, false, true
		}
	}

	return "", false, false
}

func isACPGrantKind(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case acpKindAllowOnce, acpKindAllowAlways:
		return true
	default:
		return false
	}
}

type acpRPCError struct {
	Method  string
	Code    int
	Message string
	Data    string
}

func (e *acpRPCError) Error() string {
	if e.Data != "" {
		return fmt.Sprintf("%s: %s (code=%d, data=%s)", e.Method, e.Message, e.Code, e.Data)
	}
	return fmt.Sprintf("%s: %s (code=%d)", e.Method, e.Message, e.Code)
}

func isACPSessionNotFound(err error) bool {
	var rpcErr *acpRPCError
	if !errors.As(err, &rpcErr) {
		return false
	}
	if rpcErr.Code != -32603 && rpcErr.Code != -32602 {
		return false
	}
	text := strings.ToLower(rpcErr.Message + " " + rpcErr.Data)
	return strings.Contains(text, "session not found") ||
		strings.Contains(text, "no session found")
}

func isACPProviderBadRequest(err error) bool {
	var rpcErr *acpRPCError
	if !errors.As(err, &rpcErr) {
		return false
	}
	return rpcErr.Method == "session/prompt" &&
		rpcErr.Code == -32603 &&
		rpcErr.Data == "BadRequestError"
}

const runtimeJResumeLostError = "hermes could not restore the resumed session; it refused the turn without running the agent"

func runtimeJResumeSessionLost(resumeSessionID, stopReason string, turnActivity int64) bool {
	return resumeSessionID != "" && stopReason == "refusal" && turnActivity == 0
}

func (c *runtimeJClient) handleResponse(raw map[string]json.RawMessage) {
	var id int
	if err := json.Unmarshal(raw["id"], &id); err != nil {

		var fid float64
		if err := json.Unmarshal(raw["id"], &fid); err != nil {
			return
		}
		id = int(fid)
	}

	c.mu.Lock()
	pr, ok := c.pending[id]
	if ok {
		delete(c.pending, id)
	}
	c.mu.Unlock()

	if !ok {
		return
	}

	if errData, hasErr := raw["error"]; hasErr {
		var rpcErr struct {
			Code    int             `json:"code"`
			Message string          `json:"message"`
			Data    json.RawMessage `json:"data"`
		}
		_ = json.Unmarshal(errData, &rpcErr)

		detail := ""
		if len(rpcErr.Data) > 0 && string(rpcErr.Data) != "null" {
			var s string
			if err := json.Unmarshal(rpcErr.Data, &s); err == nil {
				detail = s
			} else {
				detail = string(rpcErr.Data)
			}
		}
		pr.ch <- rpcResult{err: &acpRPCError{Method: pr.method, Code: rpcErr.Code, Message: rpcErr.Message, Data: detail}}
	} else {

		if pr.method == "session/prompt" {
			c.extractPromptResult(raw["result"])
		}
		pr.ch <- rpcResult{result: raw["result"]}
	}
}

func (c *runtimeJClient) extractPromptResult(data json.RawMessage) {
	var resp struct {
		StopReason string          `json:"stopReason"`
		Usage      json.RawMessage `json:"usage"`
		Meta       json.RawMessage `json:"_meta"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return
	}

	pr := runtimeJPromptResult{
		stopReason: resp.StopReason,
		modelID:    parseACPModelIDFromMeta(resp.Meta),
	}
	if len(resp.Usage) > 0 && string(resp.Usage) != "null" {
		pr.usage = parseACPTokenUsage(resp.Usage)
	}

	if !acpTokenUsagePresent(pr.usage) {
		if metaUsage := parseACPTokenUsageFromMeta(resp.Meta); acpTokenUsagePresent(metaUsage) {
			pr.usage = metaUsage
		}
	}

	if c.onPromptDone != nil {
		c.onPromptDone(pr)
	}
}

func acpTokenUsagePresent(u TokenUsage) bool {
	return u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheReadTokens > 0 || u.CacheWriteTokens > 0
}

func parseACPTokenUsageFromMeta(meta json.RawMessage) TokenUsage {
	if len(meta) == 0 || string(meta) == "null" {
		return TokenUsage{}
	}
	var envelope struct {
		Usage json.RawMessage `json:"usage"`
	}
	if err := json.Unmarshal(meta, &envelope); err == nil {
		if len(envelope.Usage) > 0 && string(envelope.Usage) != "null" {
			if u := parseACPTokenUsage(envelope.Usage); acpTokenUsagePresent(u) {
				return u
			}
		}
	}
	return parseACPTokenUsage(meta)
}

func parseACPModelIDFromMeta(meta json.RawMessage) string {
	if len(meta) == 0 || string(meta) == "null" {
		return ""
	}
	var r struct {
		ModelID      string `json:"modelId"`
		ModelIDSnake string `json:"model_id"`
	}
	if err := json.Unmarshal(meta, &r); err != nil {
		return ""
	}
	if id := strings.TrimSpace(r.ModelID); id != "" {
		return id
	}
	return strings.TrimSpace(r.ModelIDSnake)
}

func (c *runtimeJClient) handleNotification(raw map[string]json.RawMessage) {
	var method string
	_ = json.Unmarshal(raw["method"], &method)

	if method != "session/update" && method != "session/notification" {
		return
	}

	var params struct {
		SessionID string          `json:"sessionId"`
		Update    json.RawMessage `json:"update"`
	}
	if p, ok := raw["params"]; ok {
		_ = json.Unmarshal(p, &params)
	}
	if len(params.Update) == 0 {
		return
	}

	updateType, updateData := normalizeACPUpdate(params.Update)
	if c.acceptNotification != nil && !c.acceptNotification(updateType) {
		return
	}
	if c.onActivity != nil {
		c.onActivity()
	}

	switch updateType {
	case "agent_message_chunk":
		c.handleAgentMessage(updateData)
	case "agent_thought_chunk":
		c.handleAgentThought(updateData)
	case "tool_call":
		c.handleToolCallStart(updateData)
	case "tool_call_update":
		c.handleToolCallUpdate(updateData)
	case "usage_update":
		c.handleUsageUpdate(updateData)
	case "turn_end":
		c.extractPromptResult(updateData)
	}
}

func normalizeACPUpdate(data json.RawMessage) (string, json.RawMessage) {
	var updateType struct {
		SessionUpdate string `json:"sessionUpdate"`
		Type          string `json:"type"`
	}
	_ = json.Unmarshal(data, &updateType)
	if updateType.SessionUpdate != "" {
		return normalizeACPUpdateType(updateType.SessionUpdate), data
	}
	if updateType.Type != "" {
		return normalizeACPUpdateType(updateType.Type), data
	}

	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(data, &wrapper); err == nil && len(wrapper) == 1 {
		for k, v := range wrapper {
			return normalizeACPUpdateType(k), v
		}
	}

	return "", data
}

func normalizeACPUpdateType(t string) string {
	key := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(t), "_", ""), "-", ""))
	switch key {
	case "agentmessagechunk":
		return "agent_message_chunk"
	case "agentthoughtchunk":
		return "agent_thought_chunk"
	case "toolcall":
		return "tool_call"
	case "toolcallupdate":
		return "tool_call_update"
	case "usageupdate":
		return "usage_update"
	case "turnend", "endturn":
		return "turn_end"
	default:
		return ""
	}
}

func (c *runtimeJClient) handleAgentMessage(data json.RawMessage) {
	var сообщение struct {
		Content struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(data, &сообщение); err != nil || сообщение.Content.Text == "" {
		return
	}
	if c.onMessage != nil {
		c.onMessage(Message{Type: MessageText, Content: сообщение.Content.Text})
	}
}

func (c *runtimeJClient) handleAgentThought(data json.RawMessage) {
	var сообщение struct {
		Content struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(data, &сообщение); err != nil || сообщение.Content.Text == "" {
		return
	}
	if c.onMessage != nil {
		c.onMessage(Message{Type: MessageThinking, Content: сообщение.Content.Text})
	}
}

func (c *runtimeJClient) handleToolCallStart(data json.RawMessage) {
	var сообщение struct {
		ToolCallID string            `json:"toolCallId"`
		Name       string            `json:"name"`
		Title      string            `json:"title"`
		Kind       string            `json:"kind"`
		RawInput   map[string]any    `json:"rawInput"`
		Input      map[string]any    `json:"input"`
		Parameters map[string]any    `json:"parameters"`
		Content    []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(data, &сообщение); err != nil {
		return
	}

	toolName := runtimeJToolNameFromTitle(сообщение.Title, сообщение.Kind)
	if toolName == "" {
		toolName = сообщение.Name
	}
	rawInput := сообщение.RawInput
	if rawInput == nil {
		rawInput = сообщение.Input
	}
	if rawInput == nil {
		rawInput = сообщение.Parameters
	}

	if rawInput != nil {
		c.trackTool(сообщение.ToolCallID, &pendingToolCall{
			toolName: toolName,
			input:    rawInput,
			emitted:  true,
		})
		if c.onMessage != nil {
			c.onMessage(Message{
				Type:   MessageToolUse,
				Tool:   toolName,
				CallID: сообщение.ToolCallID,
				Input:  rawInput,
			})
		}
		return
	}

	c.trackTool(сообщение.ToolCallID, &pendingToolCall{
		toolName: toolName,
		argsText: extractACPToolCallText(сообщение.Content),
		emitted:  false,
	})
}

func (c *runtimeJClient) handleToolCallUpdate(data json.RawMessage) {
	var сообщение struct {
		ToolCallID string            `json:"toolCallId"`
		Status     string            `json:"status"`
		Name       string            `json:"name"`
		Title      string            `json:"title"`
		Kind       string            `json:"kind"`
		RawInput   map[string]any    `json:"rawInput"`
		Input      map[string]any    `json:"input"`
		Parameters map[string]any    `json:"parameters"`
		RawOutput  json.RawMessage   `json:"rawOutput"`
		Output     json.RawMessage   `json:"output"`
		Content    []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(data, &сообщение); err != nil {
		return
	}

	rawInput := сообщение.RawInput
	if rawInput == nil {
		rawInput = сообщение.Input
	}
	if rawInput == nil {
		rawInput = сообщение.Parameters
	}
	title := сообщение.Title
	if title == "" {
		title = сообщение.Name
	}

	if сообщение.Status != "completed" && сообщение.Status != "failed" {
		if pending := c.getPendingTool(сообщение.ToolCallID); pending != nil && !pending.emitted {
			if text := extractACPToolCallText(сообщение.Content); text != "" {

				pending.argsText = text
			}
		}
		return
	}

	pending := c.takePendingTool(сообщение.ToolCallID)
	c.emitDeferredToolUse(pending, сообщение.ToolCallID, title, сообщение.Kind, rawInput)

	output := acpRawText(сообщение.RawOutput)
	if output == "" {
		output = acpRawText(сообщение.Output)
	}
	if output == "" {
		output = extractACPToolCallText(сообщение.Content)
	}
	if c.onMessage != nil {
		c.onMessage(Message{
			Type:   MessageToolResult,
			CallID: сообщение.ToolCallID,
			Output: output,
			Status: сообщение.Status,
		})
	}
}

func (c *runtimeJClient) trackTool(callID string, p *pendingToolCall) {
	c.toolMu.Lock()
	defer c.toolMu.Unlock()
	if c.pendingTools == nil {
		c.pendingTools = make(map[string]*pendingToolCall)
	}
	c.pendingTools[callID] = p
}

func (c *runtimeJClient) getPendingTool(callID string) *pendingToolCall {
	c.toolMu.Lock()
	defer c.toolMu.Unlock()
	if c.pendingTools == nil {
		return nil
	}
	return c.pendingTools[callID]
}

func (c *runtimeJClient) takePendingTool(callID string) *pendingToolCall {
	c.toolMu.Lock()
	defer c.toolMu.Unlock()
	if c.pendingTools == nil {
		return nil
	}
	p := c.pendingTools[callID]
	delete(c.pendingTools, callID)
	return p
}

func (c *runtimeJClient) emitDeferredToolUse(
	p *pendingToolCall,
	callID, updateTitle, updateKind string,
	updateRawInput map[string]any,
) {
	if p != nil && p.emitted {
		return
	}

	var toolName string
	var input map[string]any

	switch {
	case p != nil && p.input != nil:

		toolName = p.toolName
		input = p.input
	case p != nil:
		toolName = p.toolName
		input = parseToolArgsJSON(p.argsText)
	default:

		toolName = runtimeJToolNameFromTitle(updateTitle, updateKind)
		input = updateRawInput
	}

	if c.onMessage == nil {
		return
	}
	c.onMessage(Message{
		Type:   MessageToolUse,
		Tool:   toolName,
		CallID: callID,
		Input:  input,
	})
}

func parseToolArgsJSON(argsText string) map[string]any {
	argsText = strings.TrimSpace(argsText)
	if argsText == "" {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(argsText), &m); err == nil {
		return m
	}
	return map[string]any{"text": argsText}
}

func acpRawText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}

	return string(raw)
}

func extractACPToolCallText(blocks []json.RawMessage) string {
	var b strings.Builder
	appendPiece := func(piece string) {
		if piece == "" {
			return
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(piece)
	}
	for _, raw := range blocks {
		var kind struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &kind); err != nil {
			continue
		}
		switch kind.Type {
		case "content":
			var outer struct {
				Content json.RawMessage `json:"content"`
			}
			if err := json.Unmarshal(raw, &outer); err != nil || len(outer.Content) == 0 {
				continue
			}
			var inner struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if err := json.Unmarshal(outer.Content, &inner); err != nil {
				continue
			}
			if inner.Type != "text" {
				continue
			}
			appendPiece(inner.Text)
		case "diff":
			var diff struct {
				Path    string `json:"path"`
				OldText string `json:"oldText"`
				NewText string `json:"newText"`
			}
			if err := json.Unmarshal(raw, &diff); err != nil || diff.Path == "" {
				continue
			}

			var piece strings.Builder
			piece.WriteString("--- ")
			piece.WriteString(diff.Path)
			piece.WriteString("\n+++ ")
			piece.WriteString(diff.Path)
			if diff.OldText == "" {
				piece.WriteString("\n(new file, ")
				piece.WriteString(strconv.Itoa(len(diff.NewText)))
				piece.WriteString(" bytes)")
			} else {
				piece.WriteString("\n(edited: ")
				piece.WriteString(strconv.Itoa(len(diff.OldText)))
				piece.WriteString(" → ")
				piece.WriteString(strconv.Itoa(len(diff.NewText)))
				piece.WriteString(" bytes)")
			}
			appendPiece(piece.String())
		default:

		}
	}
	return b.String()
}

func (c *runtimeJClient) handleUsageUpdate(data json.RawMessage) {
	var сообщение struct {
		Usage json.RawMessage `json:"usage"`
	}
	if err := json.Unmarshal(data, &сообщение); err != nil {
		return
	}
	usage := parseACPTokenUsage(сообщение.Usage)

	c.usageMu.Lock()

	if usage.InputTokens > c.usage.InputTokens {
		c.usage.InputTokens = usage.InputTokens
	}
	if usage.OutputTokens > c.usage.OutputTokens {
		c.usage.OutputTokens = usage.OutputTokens
	}
	if usage.CacheReadTokens > c.usage.CacheReadTokens {
		c.usage.CacheReadTokens = usage.CacheReadTokens
	}
	if usage.CacheWriteTokens > c.usage.CacheWriteTokens {
		c.usage.CacheWriteTokens = usage.CacheWriteTokens
	}
	if usage.CostUSDTicks > c.usage.CostUSDTicks {
		c.usage.CostUSDTicks = usage.CostUSDTicks
	}
	c.usageMu.Unlock()
}

func parseACPTokenUsage(data json.RawMessage) TokenUsage {
	if len(data) == 0 || string(data) == "null" {
		return TokenUsage{}
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return TokenUsage{}
	}
	usage := TokenUsage{
		InputTokens:  acpUsageInt64(fields, "inputTokens", "input_tokens"),
		OutputTokens: acpUsageInt64(fields, "outputTokens", "output_tokens"),
		CacheReadTokens: acpUsageInt64(fields,
			"cachedReadTokens",
			"cacheReadTokens",
			"cached_input_tokens",
			"cache_read_tokens",
			"cache_read_input_tokens",
		),
		CacheWriteTokens: acpUsageInt64(fields,
			"cachedWriteTokens",
			"cacheWriteTokens",
			"cache_write_tokens",
			"cache_creation_input_tokens",
		),

		CostUSDTicks: acpUsageInt64(fields, "costUsdTicks", "cost_usd_ticks"),
	}
	return excludeACPCachedInput(usage, acpUsageInt64(fields, "totalTokens", "total_tokens"))
}

func excludeACPCachedInput(usage TokenUsage, totalTokens int64) TokenUsage {
	if totalTokens <= 0 || usage.CacheReadTokens <= 0 || usage.CacheReadTokens > usage.InputTokens {
		return usage
	}
	if totalTokens != usage.InputTokens+usage.OutputTokens {
		return usage
	}
	usage.InputTokens -= usage.CacheReadTokens
	return usage
}

func acpUsageInt64(fields map[string]json.RawMessage, names ...string) int64 {
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		var n json.Number
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if err := dec.Decode(&n); err == nil {
			if v, err := n.Int64(); err == nil {
				return v
			}
			if f, err := n.Float64(); err == nil {
				return int64(f)
			}
		}
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			if v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64); err == nil {
				return v
			}
		}
	}
	return 0
}

func extractACPSessionID(result json.RawMessage) string {
	var r struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(result, &r); err != nil {
		return ""
	}
	return r.SessionID
}

func extractACPAuthMethods(result json.RawMessage) []string {
	var r struct {
		AuthMethods []struct {
			ID string `json:"id"`
		} `json:"authMethods"`
	}
	if err := json.Unmarshal(result, &r); err != nil {
		return nil
	}
	ids := make([]string, 0, len(r.AuthMethods))
	for _, m := range r.AuthMethods {
		if id := strings.TrimSpace(m.ID); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func extractACPCurrentModelID(result json.RawMessage) string {
	var r struct {
		Models struct {
			CurrentModelID      string `json:"currentModelId"`
			CurrentModelIDSnake string `json:"current_model_id"`
		} `json:"models"`
		CurrentModelID      string `json:"currentModelId"`
		CurrentModelIDSnake string `json:"current_model_id"`
	}
	if err := json.Unmarshal(result, &r); err != nil {
		return ""
	}
	for _, candidate := range []string{
		r.Models.CurrentModelID,
		r.Models.CurrentModelIDSnake,
		r.CurrentModelID,
		r.CurrentModelIDSnake,
	} {
		if model := strings.TrimSpace(candidate); model != "" {
			return model
		}
	}
	return ""
}

func resolveResumedSessionID(requested string, response json.RawMessage) (string, bool) {
	got := extractACPSessionID(response)
	if got == "" {
		return requested, false
	}
	return got, got != requested
}

func buildRuntimeJSessionParams(cwd, model string, mcpServers []any) map[string]any {
	if mcpServers == nil {
		mcpServers = []any{}
	}
	params := map[string]any{
		"cwd":        cwd,
		"mcpServers": mcpServers,
	}
	if model != "" {
		params["model"] = model
	}
	return params
}

func buildACPMcpServers(raw json.RawMessage, logger *slog.Logger) ([]any, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return []any{}, nil
	}
	var parsed struct {
		McpServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(trimmed, &parsed); err != nil {
		return nil, fmt.Errorf("parse mcp_config json: %w", err)
	}
	if len(parsed.McpServers) == 0 {
		return []any{}, nil
	}

	names := make([]string, 0, len(parsed.McpServers))
	for name := range parsed.McpServers {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]any, 0, len(names))
	for _, name := range names {

		disabled, malformed := mcpEntryDisabled(parsed.McpServers[name])
		logMalformedMcpFlags(logger, name, malformed)
		if disabled {
			if logger != nil {
				logger.Info("skipping disabled mcp_config entry", "name", name)
			}
			continue
		}
		entry, err := convertACPMcpServer(name, parsed.McpServers[name])
		if err != nil {
			if logger != nil {
				logger.Warn("skipping invalid mcp_config entry", "name", name, "error", err)
			}
			continue
		}
		out = append(out, entry)
	}
	return out, nil
}

func convertACPMcpServer(name string, raw json.RawMessage) (map[string]any, error) {
	var entry struct {
		Type    string            `json:"type"`
		Command string            `json:"command"`
		Args    []string          `json:"args"`
		Env     map[string]string `json:"env"`
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
	}
	if err := json.Unmarshal(raw, &entry); err != nil {
		return nil, fmt.Errorf("parse entry: %w", err)
	}

	command := strings.TrimSpace(entry.Command)
	url := strings.TrimSpace(entry.URL)

	if command != "" {
		args := entry.Args
		if args == nil {
			args = []string{}
		}
		envArr := make([]map[string]any, 0, len(entry.Env))
		for _, k := range sortedStringMapKeys(entry.Env) {
			envArr = append(envArr, map[string]any{
				"name":  k,
				"value": entry.Env[k],
			})
		}
		return map[string]any{
			"name":    name,
			"command": command,
			"args":    args,
			"env":     envArr,
		}, nil
	}

	if url != "" {
		t := strings.ToLower(strings.TrimSpace(entry.Type))
		switch t {
		case "sse":
			t = "sse"
		case "", "http", "streamable-http", "http_streamable":
			t = "http"
		default:

			t = "http"
		}
		headerArr := make([]map[string]any, 0, len(entry.Headers))
		for _, k := range sortedStringMapKeys(entry.Headers) {
			headerArr = append(headerArr, map[string]any{
				"name":  k,
				"value": entry.Headers[k],
			})
		}
		return map[string]any{
			"type":    t,
			"name":    name,
			"url":     url,
			"headers": headerArr,
		}, nil
	}

	return nil, fmt.Errorf("entry has neither command nor url")
}

func sortedStringMapKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

type acpMcpTransportCapabilities struct {
	HTTP bool
	SSE  bool
}

func extractACPMcpCapabilities(result json.RawMessage) acpMcpTransportCapabilities {
	var r struct {
		AgentCapabilities struct {
			McpCapabilities struct {
				HTTP bool `json:"http"`
				SSE  bool `json:"sse"`
			} `json:"mcpCapabilities"`
		} `json:"agentCapabilities"`
	}
	if err := json.Unmarshal(result, &r); err != nil {
		return acpMcpTransportCapabilities{}
	}
	return acpMcpTransportCapabilities{
		HTTP: r.AgentCapabilities.McpCapabilities.HTTP,
		SSE:  r.AgentCapabilities.McpCapabilities.SSE,
	}
}

func filterACPMcpServersByCapability(
	servers []any,
	caps acpMcpTransportCapabilities,
	backend string,
	logger *slog.Logger,
) []any {
	if len(servers) == 0 {
		return servers
	}
	filtered := make([]any, 0, len(servers))
	for _, raw := range servers {
		entry, ok := raw.(map[string]any)
		if !ok {
			filtered = append(filtered, raw)
			continue
		}
		transport, _ := entry["type"].(string)
		switch transport {
		case "http":
			if !caps.HTTP {
				if logger != nil {
					logger.Warn("dropping http MCP server: runtime did not advertise mcpCapabilities.http",
						"backend", backend, "name", entry["name"])
				}
				continue
			}
		case "sse":
			if !caps.SSE {
				if logger != nil {
					logger.Warn("dropping sse MCP server: runtime did not advertise mcpCapabilities.sse",
						"backend", backend, "name", entry["name"])
				}
				continue
			}
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func runtimeJToolNameFromTitle(title string, kind string) string {

	switch title {
	case "execute code":
		return "execute_code"
	}

	if idx := strings.Index(title, ":"); idx > 0 {
		name := strings.TrimSpace(title[:idx])

		switch {
		case name == "terminal":
			return "terminal"
		case name == "read":
			return "read_file"
		case name == "write":
			return "write_file"
		case strings.HasPrefix(name, "patch"):
			return "patch"
		case name == "search":
			return "search_files"
		case name == "web search":
			return "web_search"
		case name == "extract":
			return "web_extract"
		case name == "delegate":
			return "delegate_task"
		case name == "analyze image":
			return "vision_analyze"
		}
		return name
	}

	switch kind {
	case "read":
		return "read_file"
	case "edit":
		return "write_file"
	case "execute":
		return "terminal"
	case "search":
		return "search_files"
	case "fetch":
		return "web_search"
	case "think":
		return "thinking"
	default:

		if title != "" {
			return title
		}
		return kind
	}
}

type acpProviderErrorSniffer struct {
	provider string
	mu       sync.Mutex
	remains  []byte
	lines    []string
	seen     map[string]bool
	terminal bool

	echoJSON     bool
	echoDepth    int
	echoInString bool
	echoEscaped  bool
}

var acpErrorHeaderRe = regexp.MustCompile(`(?:⚠️|❌|\[ERROR\]).*(?:BadRequestError|AuthenticationError|RateLimitError|HTTP [0-9]{3}|Non-retryable|API call failed)`)

var acpErrorDetailRe = regexp.MustCompile(`(?:Error:|detail:|Details:)\s*(.+)`)

var acpTerminalErrorRe = regexp.MustCompile(
	`(?:after \d+ retr|Non-retryable|BadRequestError|AuthenticationError)` +
		`|(?:❌|\[ERROR\]).*(?:API call failed|RateLimitError|BadRequestError|AuthenticationError|Non-retryable|HTTP [0-9]{3})`)

var acpAgentOutputTerminalRe = regexp.MustCompile(`API call failed after \d+ retr(?:y|ies)`)

const acpMaxErrorLines = 8

var acpLogRecordPrefixRe = regexp.MustCompile(
	`^[0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2}(?:,[0-9]+)? \[([A-Z]+)\] ([A-Za-z0-9_.-]+):\s*(.*)$`,
)

var acpEchoLogLevels = map[string]bool{"INFO": true, "DEBUG": true}

const acpMaxErrorLineLen = 4096

func newACPProviderErrorSniffer(provider string) *acpProviderErrorSniffer {
	return &acpProviderErrorSniffer{provider: provider, seen: map[string]bool{}}
}

func (s *acpProviderErrorSniffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data := append(s.remains, p...)

	nl := strings.LastIndexByte(string(data), '\n')
	var complete string
	if nl < 0 {
		s.remains = append(s.remains[:0], data...)
		return len(p), nil
	}
	complete = string(data[:nl])
	s.remains = append(s.remains[:0], data[nl+1:]...)

	for _, rawLine := range strings.Split(complete, "\n") {
		rawLine = strings.TrimSuffix(rawLine, "\r")
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}

		if m := acpLogRecordPrefixRe.FindStringSubmatch(line); m != nil {
			s.resetEchoJSON()
			if acpEchoLogLevels[m[1]] && m[2] == "root" {
				s.startEchoJSON(m[3])
				continue
			}
		} else if s.echoJSON {

			indented := rawLine[0] == ' ' || rawLine[0] == '\t'
			bareError := strings.HasPrefix(line, "⚠️") ||
				strings.HasPrefix(line, "❌") ||
				strings.HasPrefix(line, "[ERROR]")
			if !indented && bareError && acpErrorHeaderRe.MatchString(line) {
				s.resetEchoJSON()
			} else {
				s.consumeEchoJSON(rawLine)
				continue
			}
		}
		if !(acpErrorHeaderRe.MatchString(line) || acpErrorDetailRe.MatchString(line)) {
			continue
		}
		if acpTerminalErrorRe.MatchString(line) {
			s.terminal = true
		}
		if s.seen[line] {
			continue
		}
		s.seen[line] = true
		s.lines = append(s.lines, line)
		if len(s.lines) > acpMaxErrorLines {
			s.lines = s.lines[len(s.lines)-acpMaxErrorLines:]
		}
	}
	return len(p), nil
}

func (s *acpProviderErrorSniffer) startEchoJSON(payload string) {
	payload = strings.TrimSpace(payload)
	if payload == "" || (payload[0] != '{' && payload[0] != '[') {
		return
	}
	s.echoJSON = true
	s.consumeEchoJSON(payload)
}

func (s *acpProviderErrorSniffer) consumeEchoJSON(fragment string) {
	for i := 0; i < len(fragment); i++ {
		switch {
		case s.echoEscaped:
			s.echoEscaped = false
		case s.echoInString:
			switch fragment[i] {
			case '\\':
				s.echoEscaped = true
			case '"':
				s.echoInString = false
			}
		default:
			switch fragment[i] {
			case '"':
				s.echoInString = true
			case '{', '[':
				s.echoDepth++
			case '}', ']':
				if s.echoDepth > 0 {
					s.echoDepth--
				}
			}
		}
	}
	if s.echoDepth == 0 {
		s.resetEchoJSON()
	}
}

func (s *acpProviderErrorSniffer) resetEchoJSON() {
	s.echoJSON = false
	s.echoDepth = 0
	s.echoInString = false
	s.echoEscaped = false
}

func (s *acpProviderErrorSniffer) сообщение() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.messageLocked()
}

func (s *acpProviderErrorSniffer) terminalMessage() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.terminal {
		return ""
	}
	return s.messageLocked()
}

func (s *acpProviderErrorSniffer) messageLocked() string {
	prefix := s.provider + " provider error: "
	for _, line := range s.lines {
		if m := acpErrorDetailRe.FindStringSubmatch(line); m != nil {
			detail := strings.TrimSpace(m[1])
			if detail != "" {
				return acpTruncateError(prefix + detail)
			}
		}
	}
	for _, line := range s.lines {
		if acpErrorHeaderRe.MatchString(line) {
			return acpTruncateError(prefix + line)
		}
	}
	return ""
}

func acpTruncateError(сообщение string) string {
	if len(сообщение) <= acpMaxErrorLineLen {
		return сообщение
	}
	return strings.ToValidUTF8(сообщение[:acpMaxErrorLineLen], "") + "…(truncated)"
}

func promoteACPResultOnProviderError(finalStatus, finalError, finalOutput string, sniffer *acpProviderErrorSniffer) (string, string) {
	if finalStatus != "completed" {
		return finalStatus, finalError
	}
	if сообщение := sniffer.terminalMessage(); сообщение != "" {
		return "failed", сообщение
	}
	if acpAgentOutputTerminalRe.MatchString(finalOutput) {
		сообщение := sniffer.сообщение()
		if сообщение == "" {
			сообщение = sniffer.provider + " provider error: " + acpAgentOutputTerminalRe.FindString(finalOutput)
		}
		return "failed", сообщение
	}
	if finalOutput == "" {
		if сообщение := sniffer.сообщение(); сообщение != "" {
			return "failed", сообщение
		}
	}
	return finalStatus, finalError
}
