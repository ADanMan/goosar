package agent

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var runtimeIBlockedArgs = map[string]blockedArgMode{
	"agent":                    blockedStandalone,
	"stdio":                    blockedStandalone,
	"headless":                 blockedStandalone,
	"serve":                    blockedStandalone,
	"leader":                   blockedStandalone,
	"--always-approve":         blockedStandalone,
	"--yolo":                   blockedStandalone,
	"--no-auto-update":         blockedStandalone,
	"--no-alt-screen":          blockedStandalone,
	"-p":                       blockedStandalone,
	"--print":                  blockedStandalone,
	"--single":                 blockedWithValue,
	"--output-format":          blockedWithValue,
	"--permission-mode":        blockedWithValue,
	"-m":                       blockedWithValue,
	"--model":                  blockedWithValue,
	"--reasoning-effort":       blockedWithValue,
	"--effort":                 blockedWithValue,
	"-r":                       blockedWithValue,
	"--resume":                 blockedWithValue,
	"-c":                       blockedStandalone,
	"--continue":               blockedStandalone,
	"-s":                       blockedWithValue,
	"--session-id":             blockedWithValue,
	"--system-prompt-override": blockedWithValue,
	"--cwd":                    blockedWithValue,
	"-w":                       blockedOptionalValue,
	"--worktree":               blockedOptionalValue,
	"--ref":                    blockedWithValue,
	"--fork-session":           blockedStandalone,
}

type runtimeIBackend struct {
	cfg Config
}

var (
	runtimeIReaderDrainGrace      = 2 * time.Second
	runtimeINotificationQuietTime = 250 * time.Millisecond
)

type runtimeIMessageStream struct {
	ch     chan Message
	mu     sync.Mutex
	closed bool
}

func newRuntimeIMessageStream(size int) *runtimeIMessageStream {
	return &runtimeIMessageStream{ch: make(chan Message, size)}
}

func (s *runtimeIMessageStream) send(сообщение Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	trySend(s.ch, сообщение)
}

func (s *runtimeIMessageStream) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.ch)
}

func (b *runtimeIBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = runtimeCLIName("runtime-i")
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("grok executable not found at %q: %w", execPath, err)
	}

	mcpServers, err := buildACPMcpServers(opts.McpConfig, b.cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("grok: invalid mcp_config: %w", err)
	}

	timeout := opts.Timeout
	runCtx, cancel := runContext(ctx, timeout)

	runtimeIArgs := []string{"--no-auto-update", "agent", "--always-approve"}
	if opts.ThinkingLevel != "" {
		runtimeIArgs = append(runtimeIArgs, "--effort", opts.ThinkingLevel)
	}
	runtimeIArgs = append(runtimeIArgs, filterCustomArgs(opts.CustomArgs, runtimeIBlockedArgs, b.cfg.Logger)...)
	runtimeIArgs = append(runtimeIArgs, "stdio")

	cmd := newRuntimeCmd(exec.CommandContext(runCtx, execPath, runtimeIArgs...))
	hideAgentWindow(cmd)
	b.cfg.logAgentCommand(cmd, newAgentCommandLogArgs(runtimeIArgs,
		trustAgentCommandPositional(1, "agent"),
		trustAgentCommandPositional(len(runtimeIArgs)-1, "stdio"),
	))
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	childEnv := buildEnv(b.cfg.Env)
	cmd.Env = childEnv

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("grok stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("grok stdin pipe: %w", err)
	}

	providerErr := newACPProviderErrorSniffer("grok")
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("grok stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start grok: %w", err)
	}

	stderrSink := io.MultiWriter(newLogWriter(b.cfg.Logger, "[grok:stderr] "), providerErr)
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		_, _ = io.Copy(stderrSink, stderr)
	}()

	b.cfg.Logger.Info("grok acp started", "pid", cmd.Process.Pid, "cwd", opts.Cwd)

	msgStream := newRuntimeIMessageStream(256)
	resCh := make(chan Result, 1)

	var outputMu sync.Mutex
	var output strings.Builder
	var streamingCurrentTurn atomic.Bool

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
			if сообщение.Type == MessageToolUse {

				сообщение.Tool = runtimeKToolNameFromTitle(сообщение.Tool)
			}
			if сообщение.Type == MessageText {
				outputMu.Lock()
				output.WriteString(сообщение.Content)
				outputMu.Unlock()
			}
			msgStream.send(сообщение)
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
		c.closeAllPending(fmt.Errorf("grok process exited"))
	}()

	go func() {
		defer cancel()
		defer msgStream.close()
		defer close(resCh)
		defer func() {
			stdin.Close()
			_ = cmd.Wait()
		}()

		startTime := time.Now()
		finalStatus := "completed"
		var finalError string
		var sessionID string

		var resumeRejected bool
		effectiveModel := strings.TrimSpace(opts.Model)

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
			finalError = fmt.Sprintf("grok initialize failed: %v", err)
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}

		methodID, err := selectRuntimeIAuthMethod(extractACPAuthMethods(initResult), envHasNonEmpty(childEnv, "XAI_API_KEY"))
		if err != nil {
			finalStatus = "failed"
			finalError = fmt.Sprintf("grok authentication setup failed: %v", err)
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}
		if _, err := c.request(runCtx, "authenticate", map[string]any{
			"methodId": methodID,
			"_meta":    map[string]any{"headless": true},
		}); err != nil {
			finalStatus = "failed"
			finalError = fmt.Sprintf("grok authenticate (%s) failed: %v", methodID, err)
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}
		b.cfg.Logger.Info("grok authenticated", "method", methodID)

		mcpServers = filterACPMcpServersByCapability(mcpServers, extractACPMcpCapabilities(initResult), "grok", b.cfg.Logger)

		cwd := opts.Cwd
		if cwd == "" {
			cwd = "."
		}

		if opts.ResumeSessionID != "" {
			result, err := c.request(runCtx, "session/load", map[string]any{
				"cwd":        cwd,
				"sessionId":  opts.ResumeSessionID,
				"mcpServers": mcpServers,
			})
			if err != nil {
				finalStatus = "failed"
				finalError = fmt.Sprintf("grok session/load failed: %v", err)
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			var changed bool
			sessionID, changed = resolveResumedSessionID(opts.ResumeSessionID, result)
			if changed {
				b.cfg.Logger.Warn("agent returned a different session id on resume — original was likely lost; continuing with the new id",
					"backend", "grok",
					"requested", opts.ResumeSessionID,
					"actual", sessionID,
				)
			}
			if effectiveModel == "" {
				effectiveModel = extractACPCurrentModelID(result)
			}
		} else {
			result, err := c.request(runCtx, "session/new", map[string]any{
				"cwd":        cwd,
				"mcpServers": mcpServers,
			})
			if err != nil {
				finalStatus = "failed"
				finalError = fmt.Sprintf("grok session/new failed: %v", err)
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			sessionID = extractACPSessionID(result)
			if sessionID == "" {
				finalStatus = "failed"
				finalError = "grok session/new returned no session ID"
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
			if effectiveModel == "" {
				effectiveModel = extractACPCurrentModelID(result)
			}
		}

		c.sessionID = sessionID

		msgStream.send(Message{Type: MessageStatus, Status: "running", SessionID: sessionID})
		b.cfg.Logger.Info("grok session created", "session_id", sessionID)

		if opts.Model != "" {
			if _, err := c.request(runCtx, "session/set_model", map[string]any{
				"sessionId": sessionID,
				"modelId":   opts.Model,
			}); err != nil {
				b.cfg.Logger.Warn("grok set_session_model failed", "error", err, "requested_model", opts.Model)
				finalStatus = "failed"
				finalError = fmt.Sprintf("grok could not switch to model %q: %v", opts.Model, err)
				if opts.ResumeSessionID != "" && isACPSessionNotFound(err) {
					b.cfg.Logger.Warn("resumed session not found at set_model time; clearing session id so the daemon retries fresh",
						"backend", "grok",
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
			b.cfg.Logger.Info("grok session model set", "model", opts.Model)
		}

		userText := prompt
		if opts.SystemPrompt != "" {

			userText = opts.SystemPrompt + "\n\n---\n\n" + prompt
		}

		streamingCurrentTurn.Store(true)
		_, err = c.request(runCtx, "session/prompt", map[string]any{
			"sessionId": sessionID,
			"prompt": []map[string]any{
				{"type": "text", "text": userText},
			},
		})
		if err != nil {
			if runCtx.Err() == context.DeadlineExceeded {
				finalStatus = "timeout"
				finalError = fmt.Sprintf("grok timed out after %s", timeout)
			} else if runCtx.Err() == context.Canceled {
				finalStatus = "aborted"
				finalError = "execution cancelled"
			} else {
				finalStatus = "failed"
				finalError = fmt.Sprintf("grok session/prompt failed: %v", err)
				if opts.ResumeSessionID != "" && isACPSessionNotFound(err) {
					b.cfg.Logger.Warn("resumed session not found at prompt time; clearing session id so the daemon retries fresh",
						"backend", "grok",
						"session_id", sessionID,
					)
					sessionID = ""
					resumeRejected = true
				}
			}
		} else {
			select {
			case pr := <-promptDone:
				if pr.stopReason == "cancelled" {
					finalStatus = "aborted"
					finalError = "grok cancelled the prompt"
				}

				if effectiveModel == "" {
					effectiveModel = pr.modelID
				}
				c.usageMu.Lock()
				c.usage.InputTokens += pr.usage.InputTokens
				c.usage.OutputTokens += pr.usage.OutputTokens
				c.usage.CacheReadTokens += pr.usage.CacheReadTokens

				c.usage.CostUSDTicks += pr.usage.CostUSDTicks
				c.usageMu.Unlock()
			default:
			}
			waitForRuntimeINotificationQuiescence(runCtx, activity, readerDone)
		}

		duration := time.Since(startTime)
		b.cfg.Logger.Info("grok finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		stdin.Close()
		cancel()

		drainCtx, drainCancel := context.WithTimeout(context.Background(), runtimeIReaderDrainGrace)
		select {
		case <-readerDone:
		case <-drainCtx.Done():
		}
		select {
		case <-stderrDone:
		case <-drainCtx.Done():
		}
		drainCancel()
		streamingCurrentTurn.Store(false)

		outputMu.Lock()
		finalOutput := output.String()
		outputMu.Unlock()

		finalStatus, finalError = promoteACPResultOnProviderError(finalStatus, finalError, finalOutput, providerErr)

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

	return &Session{Messages: msgStream.ch, Result: resCh}, nil
}

const (
	runtimeIAuthMethodAPIKey      = "xai.api_key"
	runtimeIAuthMethodCachedToken = "cached_token"
)

func selectRuntimeIAuthMethod(methods []string, haveAPIKey bool) (string, error) {
	offered := make(map[string]bool, len(methods))
	for _, m := range methods {
		if m = strings.TrimSpace(m); m != "" {
			offered[m] = true
		}
	}
	if haveAPIKey && offered[runtimeIAuthMethodAPIKey] {
		return runtimeIAuthMethodAPIKey, nil
	}
	if offered[runtimeIAuthMethodCachedToken] {
		return runtimeIAuthMethodCachedToken, nil
	}
	if offered[runtimeIAuthMethodAPIKey] {
		return "", fmt.Errorf("Grok advertised only API-key authentication, but XAI_API_KEY is not set")
	}
	advertised := make([]string, 0, len(offered))
	for method := range offered {
		advertised = append(advertised, method)
	}
	sort.Strings(advertised)
	if len(advertised) == 0 {
		return "", fmt.Errorf("Grok advertised no usable authentication methods; set XAI_API_KEY or run `grok login`")
	}
	return "", fmt.Errorf("Grok advertised unsupported authentication methods %q; update Goosar or authenticate with XAI_API_KEY / `grok login`", advertised)
}

func waitForRuntimeINotificationQuiescence(ctx context.Context, activity <-chan struct{}, readerDone <-chan struct{}) {
	quiet := time.NewTimer(runtimeINotificationQuietTime)
	defer quiet.Stop()
	hard := time.NewTimer(runtimeIReaderDrainGrace)
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
			quiet.Reset(runtimeINotificationQuietTime)
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

func envHasNonEmpty(env []string, key string) bool {
	prefix := key + "="
	for i := len(env) - 1; i >= 0; i-- {
		if strings.HasPrefix(env[i], prefix) {
			return strings.TrimSpace(env[i][len(prefix):]) != ""
		}
	}
	return false
}
