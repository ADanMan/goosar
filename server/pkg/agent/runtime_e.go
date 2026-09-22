package agent

import (
	"bufio"
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
	"syscall"
	"time"
)

var runtimeEBlockedArgs = map[string]blockedArgMode{
	"--listen": blockedWithValue,
}

const (
	runtimeEFastServiceTier = "priority"
	runtimeEFastModeFeature = "fast_mode"
)

const (
	runtimeEStderrTailBytes                   = 2048
	defaultRuntimeESemanticInactivityTimeout  = 10 * time.Minute
	defaultRuntimeEFirstTurnNoProgressTimeout = 30 * time.Second
	defaultRuntimeEHandshakeTimeout           = 30 * time.Second
	runtimeEVersionDiagnosticTimeout          = 2 * time.Second

	runtimeEGracefulShutdownTimeout = 10 * time.Second
)

var (
	runtimeEGracefulShutdownTimeoutNanos atomic.Int64
	runtimeEProcessWaitDelayNanos        atomic.Int64
	activeRuntimeELaunches               atomic.Int64
	maxActiveRuntimeELaunchesObserved    atomic.Int64
	runtimeECleanupConfirmationOverride  atomic.Int32
)

func sanitizeRuntimeEDiagnostic(value string) string {
	return sanitizeAgentDiagnostic(value)
}

func runtimeEProcessExitStatus(state *os.ProcessState) any {
	if state == nil {
		return nil
	}
	return state.String()
}

func runtimeEGracefulShutdown() time.Duration {
	if n := runtimeEGracefulShutdownTimeoutNanos.Load(); n > 0 {
		return time.Duration(n)
	}
	return runtimeEGracefulShutdownTimeout
}

func runtimeEProcessWaitDelay() time.Duration {
	if n := runtimeEProcessWaitDelayNanos.Load(); n > 0 {
		return time.Duration(n)
	}
	return 10 * time.Second
}

type runtimeEStderrClassification struct {
	modelRefreshTimeout int
	mcpInitTransport    int
	bareTimeout         int
}

func classifyRuntimeEStartupStderr(stderr string, timedOut bool) runtimeEStderrClassification {
	lower := strings.ToLower(sanitizeRuntimeEDiagnostic(stderr))
	classification := runtimeEStderrClassification{
		modelRefreshTimeout: strings.Count(lower, runtimeEModelCatalogRefreshTimeoutSignal),
	}
	for _, line := range strings.Split(lower, "\n") {
		if strings.Contains(line, "mcp") && strings.Contains(line, "transport") &&
			(strings.Contains(line, "error") || strings.Contains(line, "failed") || strings.Contains(line, "closed")) {
			classification.mcpInitTransport++
		}
	}
	if timedOut && classification.modelRefreshTimeout == 0 && classification.mcpInitTransport == 0 {
		classification.bareTimeout = 1
	}
	return classification
}

const RuntimeESemanticInactivityMarker = "codex semantic inactivity timeout"

const RuntimeEFirstTurnNoProgressMarker = "codex app-server no progress timeout"

const RuntimeEHandshakeTimeoutMarker = "codex app-server handshake timeout"

const (
	runtimeEModelCatalogRefreshFailureSignal = "failed to refresh available models"
	runtimeEModelCatalogRefreshTimeoutSignal = "failed to refresh available models: timeout waiting for child process to exit"
)

var errRuntimeEProcessExited = errors.New("codex process exited")

type runtimeETimeoutKind int

const (
	runtimeETimeoutNone runtimeETimeoutKind = iota
	runtimeETimeoutSemanticInactivity
	runtimeETimeoutFirstTurnNoProgress
)

type runtimeETimeoutDiagnostic struct {
	Kind            runtimeETimeoutKind
	Timeout         time.Duration
	LastActivity    string
	ThreadID        string
	TurnID          string
	Model           string
	RuntimeEVersion string
}

type runtimeEBackend struct {
	cfg Config
}

func buildRuntimeEArgs(opts ExecOptions, logger *slog.Logger) []string {
	args := []string{"app-server", "--listen", "stdio://"}
	launchArgs := NormalizeRuntimeELaunchArgs(opts.ExtraArgs, opts.CustomArgs, opts.McpConfig, logger)
	if opts.ServiceTier == runtimeEFastServiceTier {
		launchArgs = enforceRuntimeEFastMode(launchArgs, logger)
	}
	return append(args, launchArgs...)
}

func NormalizeRuntimeELaunchArgs(extraArgs, customArgs []string, mcpConfig json.RawMessage, logger *slog.Logger) []string {
	extra := filterCustomArgs(extraArgs, runtimeEBlockedArgs, logger)
	custom := filterCustomArgs(customArgs, runtimeEBlockedArgs, logger)

	if hasManagedRuntimeEMcpConfig(mcpConfig) {
		extra = filterRuntimeECustomConfigOverrides(extra, logger)
		custom = filterRuntimeECustomConfigOverrides(custom, logger)
	}
	out := make([]string, 0, len(extra)+len(custom))
	out = append(out, extra...)
	out = append(out, custom...)
	return out
}

func enforceRuntimeEFastMode(args []string, logger *slog.Logger) []string {
	args = filterRuntimeEConfigOverrides(
		args,
		runtimeEManagedFastModeConfigKeyRe,
		"features.fast_mode",
		logger,
	)
	filtered := make([]string, 0, len(args)+2)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		flag := arg
		value := ""
		hasInlineValue := false
		if idx := strings.Index(arg, "="); idx > 0 {
			flag = arg[:idx]
			value = arg[idx+1:]
			hasInlineValue = true
		}
		if flag == "--disable" {
			if !hasInlineValue && i+1 < len(args) {
				value = args[i+1]
			}
			if value == runtimeEFastModeFeature {
				if logger != nil {
					logger.Warn("codex: ignored lower-priority feature disable",
						"feature", runtimeEFastModeFeature)
				}
				if !hasInlineValue {
					i++
				}
				continue
			}
		}
		filtered = append(filtered, arg)
	}
	return append(filtered, "--enable", runtimeEFastModeFeature)
}

func hasManagedRuntimeEMcpConfig(raw json.RawMessage) bool {
	return hasManagedMcpConfig(raw)
}

var runtimeEManagedMcpConfigKeyRe = regexp.MustCompile(`^\s*mcp_servers(?:\s*\.|\s*=|\s*$)`)

var runtimeEManagedFastModeConfigKeyRe = regexp.MustCompile(
	`^\s*features\s*\.\s*fast_mode\s*(?:=|$)`)

const (
	runtimeEShellEnvPolicyKeyPattern = `(?:shell_environment_policy|"shell_environment_policy"|'shell_environment_policy')`
	runtimeEProfileNameKeyPattern    = `(?:[A-Za-z0-9_-]+|"[^"]+"|'[^']+')`
)

var runtimeEManagedShellEnvConfigKeyRe = regexp.MustCompile(
	`^\s*(?:` + runtimeEShellEnvPolicyKeyPattern + `|profiles\s*\.\s*` + runtimeEProfileNameKeyPattern + `\s*\.\s*` + runtimeEShellEnvPolicyKeyPattern + `)\s*(?:\.|=|$)`)

func filterRuntimeECustomConfigOverrides(args []string, logger *slog.Logger) []string {
	return filterRuntimeEConfigOverrides(args, runtimeEManagedMcpConfigKeyRe, "mcp_servers", logger)
}

func filterRuntimeEShellEnvConfigOverrides(args []string, logger *slog.Logger) []string {
	return filterRuntimeEConfigOverrides(args, runtimeEManagedShellEnvConfigKeyRe, shellEnvironmentPolicyConfigNamespace, logger)
}

const shellEnvironmentPolicyConfigNamespace = "shell_environment_policy"

func filterRuntimeEConfigOverrides(args []string, managedKeyRe *regexp.Regexp, namespace string, logger *slog.Logger) []string {
	if len(args) == 0 {
		return args
	}
	filtered := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		flag := arg
		inlineValue := ""
		hasInlineValue := false
		if idx := strings.Index(arg, "="); idx > 0 {
			flag = arg[:idx]
			inlineValue = arg[idx+1:]
			hasInlineValue = true
		}
		if flag == "-c" || flag == "--config" {
			value := inlineValue
			if !hasInlineValue && i+1 < len(args) {
				value = args[i+1]
			}
			if managedKeyRe.MatchString(value) {
				if logger != nil {

					key := value
					if eqIdx := strings.Index(value, "="); eqIdx >= 0 {
						key = value[:eqIdx]
					}
					logger.Warn("custom_args: blocked managed Codex config override",
						"namespace", namespace, "flag", flag, "key", strings.TrimSpace(key))
				}
				if !hasInlineValue && i+1 < len(args) {
					i++
				}
				continue
			}
		}
		filtered = append(filtered, arg)
	}
	return filtered
}

const (
	goosarRuntimeEMcpBeginMarker = "# BEGIN goosar-managed mcp_servers (do not edit; regenerated by daemon)"
	goosarRuntimeEMcpEndMarker   = "# END goosar-managed mcp_servers"
)

var runtimeEMcpBlockRe = regexp.MustCompile(
	`(?ms)^` + regexp.QuoteMeta(goosarRuntimeEMcpBeginMarker) +
		`.*?^` + regexp.QuoteMeta(goosarRuntimeEMcpEndMarker) + `\n*`)

var userRuntimeEMcpServersTableHeaderRe = regexp.MustCompile(
	`^\s*\[\s*mcp_servers\s*\.\s*(?:"[^"]*"|[^\]\s]+)\s*\]\s*(?:#.*)?$`)

func ensureRuntimeEMcpConfig(configPath string, mcpConfig json.RawMessage, logger *slog.Logger) error {

	mcpConfig = filterDisabledMcpServers(mcpConfig, logger)

	data, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read config.toml: %w", err)
	}
	existing := string(data)

	stripped := runtimeEMcpBlockRe.ReplaceAllString(existing, "")

	managed := hasManagedRuntimeEMcpConfig(mcpConfig)
	block, _, renderErr := renderRuntimeEMcpServersBlock(mcpConfig)
	if renderErr != nil {
		return renderErr
	}

	var updated string
	if managed {

		stripped = stripRuntimeEUserMcpServerTables(stripped)
		stripped = strings.TrimRight(stripped, "\n")

		if block == "" {
			block = goosarRuntimeEMcpBeginMarker + "\n" + goosarRuntimeEMcpEndMarker + "\n"
		}
		if stripped == "" {
			updated = block
		} else {
			updated = stripped + "\n\n" + block
		}
	} else {

		updated = stripped
	}

	if updated == existing {
		return nil
	}
	if err := os.WriteFile(configPath, []byte(updated), 0o600); err != nil {
		return fmt.Errorf("write config.toml: %w", err)
	}

	if err := os.Chmod(configPath, 0o600); err != nil {
		return fmt.Errorf("chmod config.toml to 0600: %w", err)
	}
	if logger != nil {
		logger.Debug("codex: wrote managed mcp_servers block to config.toml",
			"config_path", configPath, "managed", managed)
	}
	return nil
}

func renderRuntimeEMcpServersBlock(raw json.RawMessage) (string, bool, error) {
	if len(raw) == 0 {
		return "", false, nil
	}
	var parsed struct {
		McpServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", false, fmt.Errorf("parse mcp_config json: %w", err)
	}
	if len(parsed.McpServers) == 0 {
		return "", false, nil
	}

	names := make([]string, 0, len(parsed.McpServers))
	for name := range parsed.McpServers {
		names = append(names, name)
	}
	sort.Strings(names)

	var sb strings.Builder
	sb.WriteString(goosarRuntimeEMcpBeginMarker)
	sb.WriteString("\n")
	for i, name := range names {
		if !isRuntimeEBareTomlKey(name) {
			return "", false, fmt.Errorf("mcp server name %q must be ASCII alphanumeric / _ / - to fit Codex's bare-key requirement", name)
		}
		var serverVal map[string]any
		if err := json.Unmarshal(parsed.McpServers[name], &serverVal); err != nil {
			return "", false, fmt.Errorf("mcp_servers.%s: %w", name, err)
		}
		if serverVal == nil {
			return "", false, fmt.Errorf("mcp_servers.%s must be a JSON object", name)
		}
		serverVal = normalizeRuntimeEMcpServerConfig(serverVal)
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("[mcp_servers.")
		sb.WriteString(name)
		sb.WriteString("]\n")
		keys := make([]string, 0, len(serverVal))
		for k := range serverVal {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			tomlValue, err := jsonValueToRuntimeETOMLInline(serverVal[k])
			if err != nil {
				return "", false, fmt.Errorf("mcp_servers.%s.%s: %w", name, k, err)
			}
			sb.WriteString(runtimeETOMLKey(k))
			sb.WriteString(" = ")
			sb.WriteString(tomlValue)
			sb.WriteString("\n")
		}
	}
	sb.WriteString(goosarRuntimeEMcpEndMarker)
	sb.WriteString("\n")
	return sb.String(), true, nil
}

func normalizeRuntimeEMcpServerConfig(server map[string]any) map[string]any {
	if !isRuntimeERemoteMcpServer(server) {
		normalized := make(map[string]any, len(server))
		for k, v := range server {
			if isGoosarMcpSelectorKey(k) {
				continue
			}
			normalized[k] = v
		}
		return normalized
	}

	normalized := make(map[string]any, len(server)+1)
	for k, v := range server {
		switch {
		case isGoosarMcpSelectorKey(k):
			continue
		case k == "type":
			continue
		case k == "headers":
			if _, ok := server["http_headers"]; !ok {
				normalized["http_headers"] = v
			}
		default:
			normalized[k] = v
		}
	}
	normalized["experimental_use_rmcp_client"] = true
	return normalized
}

func isGoosarMcpSelectorKey(k string) bool {
	switch k {
	case "tools", "prompts", "resources":
		return true
	default:
		return false
	}
}

func isRuntimeERemoteMcpServer(server map[string]any) bool {
	if typ, ok := server["type"].(string); ok && strings.EqualFold(typ, "http") {
		return true
	}
	_, hasURL := server["url"]
	_, hasCommand := server["command"]
	return hasURL && !hasCommand
}

func stripRuntimeEUserMcpServerTables(content string) string {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	skipping := false
	for _, line := range lines {
		if userRuntimeEMcpServersTableHeaderRe.MatchString(line) {
			skipping = true
			continue
		}
		if skipping {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "[") {

				if userRuntimeEMcpServersTableHeaderRe.MatchString(line) ||
					strings.HasPrefix(trimmed, "[mcp_servers.") ||
					strings.HasPrefix(trimmed, "[ mcp_servers.") {
					continue
				}
				skipping = false
				out = append(out, line)
				continue
			}
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func jsonValueToRuntimeETOMLInline(v any) (string, error) {
	switch x := v.(type) {
	case nil:
		return "", fmt.Errorf("null is not a valid TOML value")
	case bool:
		if x {
			return "true", nil
		}
		return "false", nil
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10), nil
		}
		return strconv.FormatFloat(x, 'f', -1, 64), nil
	case string:
		return runtimeETOMLBasicString(x), nil
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			p, err := jsonValueToRuntimeETOMLInline(e)
			if err != nil {
				return "", err
			}
			parts[i] = p
		}
		return "[" + strings.Join(parts, ", ") + "]", nil
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			p, err := jsonValueToRuntimeETOMLInline(x[k])
			if err != nil {
				return "", err
			}
			parts[i] = runtimeETOMLKey(k) + " = " + p
		}
		return "{ " + strings.Join(parts, ", ") + " }", nil
	default:
		return "", fmt.Errorf("unsupported value type %T", v)
	}
}

func runtimeETOMLBasicString(s string) string {
	var sb strings.Builder
	sb.Grow(len(s) + 2)
	sb.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			sb.WriteString(`\\`)
		case '"':
			sb.WriteString(`\"`)
		case '\b':
			sb.WriteString(`\b`)
		case '\t':
			sb.WriteString(`\t`)
		case '\n':
			sb.WriteString(`\n`)
		case '\f':
			sb.WriteString(`\f`)
		case '\r':
			sb.WriteString(`\r`)
		default:
			if r < 0x20 || r == 0x7f {
				sb.WriteString(fmt.Sprintf(`\u%04x`, r))
			} else {
				sb.WriteRune(r)
			}
		}
	}
	sb.WriteByte('"')
	return sb.String()
}

func runtimeETOMLKey(s string) string {
	if isRuntimeEBareTomlKey(s) {
		return s
	}
	return runtimeETOMLBasicString(s)
}

func isRuntimeEBareTomlKey(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '-':
		default:
			return false
		}
	}
	return true
}

func (b *runtimeEBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	firstSession, err := b.executeOnce(ctx, prompt, opts, 1)
	if err != nil {
		return nil, err
	}
	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)

	go func() {
		defer close(msgCh)
		defer close(resCh)
		session := firstSession
		attemptOpts := opts
		for attempt := 1; attempt <= 2; attempt++ {
			if attempt > 1 {
				var err error
				session, err = b.executeOnce(ctx, prompt, attemptOpts, attempt)
				if err != nil {
					resCh <- Result{Status: "failed", Error: err.Error()}
					return
				}
			}

			var heldPins []Message
			holdingPins := true
			flushHeldPins := func() {
				for _, held := range heldPins {
					msgCh <- held
				}
				heldPins = nil
				holdingPins = false
			}
			for сообщение := range session.Messages {
				if holdingPins && сообщение.Type == MessageStatus && сообщение.Status == "running" {
					heldPins = append(heldPins, сообщение)
					continue
				}
				if holdingPins {
					flushHeldPins()
				}
				msgCh <- сообщение
			}
			result, ok := <-session.Result
			if !ok {
				flushHeldPins()
				resCh <- Result{Status: "failed", Error: "codex attempt closed without result"}
				return
			}
			retryReason := ""
			switch {
			case result.runtimeEInitializeRetrySafe:
				retryReason = "initialize"
			case result.runtimeEStartupRefreshRetrySafe:
				retryReason = "model_catalog_refresh"
			}
			if retryReason == "" || attempt == 2 {
				flushHeldPins()
				resCh <- result
				return
			}

			backoff := 75*time.Millisecond + time.Duration(time.Now().UnixNano()%50)*time.Millisecond
			if retryReason == "model_catalog_refresh" {
				backoff = 500*time.Millisecond + time.Duration(time.Now().UnixNano()%1000)*time.Millisecond

				if attemptOpts.ResumeSessionID != "" {
					b.cfg.Logger.Warn("codex retry dropping resume pointer after model catalog refresh failure",
						"prior_thread_id", attemptOpts.ResumeSessionID,
					)
					attemptOpts.ResumeSessionID = ""
					attemptOpts.ResumeExpected = true
				}
			}
			b.cfg.Logger.Warn("codex retry scheduled", "reason", retryReason, "attempt", attempt, "next_attempt", attempt+1, "backoff", backoff.String())
			select {
			case <-ctx.Done():
				flushHeldPins()
				resCh <- result
				return
			case <-time.After(backoff):
			}
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

func (b *runtimeEBackend) executeOnce(ctx context.Context, prompt string, opts ExecOptions, attempt int) (*Session, error) {
	execPath := b.cfg.ExecutablePath
	if execPath == "" {
		execPath = runtimeCLIName("runtime-e")
	}
	if _, err := exec.LookPath(execPath); err != nil {
		return nil, fmt.Errorf("codex executable not found at %q: %w", execPath, err)
	}

	timeout := opts.Timeout
	semanticInactivityTimeout := opts.SemanticInactivityTimeout
	if semanticInactivityTimeout == 0 {
		semanticInactivityTimeout = defaultRuntimeESemanticInactivityTimeout
	}
	handshakeTimeout := opts.HandshakeTimeout
	if handshakeTimeout <= 0 {
		handshakeTimeout = defaultRuntimeEHandshakeTimeout
	}
	runCtx, cancel := runContext(ctx, timeout)

	runtimeEHome := strings.TrimSpace(b.cfg.Env["CODEX_HOME"])
	if runtimeEHome != "" {
		if err := ensureRuntimeEMcpConfig(filepath.Join(runtimeEHome, "config.toml"), opts.McpConfig, b.cfg.Logger); err != nil {

			cancel()
			return nil, fmt.Errorf("apply codex mcp_config: %w", err)
		}
	} else if hasManagedRuntimeEMcpConfig(opts.McpConfig) {

		cancel()
		return nil, fmt.Errorf("codex: mcp_config is set but CODEX_HOME env var is not configured; cannot apply managed MCP")
	}

	if runtimeEHome != "" {

		opts.ExtraArgs = filterRuntimeEShellEnvConfigOverrides(opts.ExtraArgs, b.cfg.Logger)
		opts.CustomArgs = filterRuntimeEShellEnvConfigOverrides(opts.CustomArgs, b.cfg.Logger)
	}
	runtimeEArgs := buildRuntimeEArgs(opts, b.cfg.Logger)
	cmd := newRuntimeCmd(exec.CommandContext(runCtx, execPath, runtimeEArgs...))
	hideAgentWindow(cmd)

	configureProcessGroup(cmd)

	cmd.Cancel = func() error {
		if cmd.Process != nil {
			signalProcessGroup(cmd.Process, syscall.SIGKILL)
		}
		return nil
	}

	cmd.WaitDelay = runtimeEProcessWaitDelay()
	b.cfg.logAgentCommand(cmd, newAgentCommandLogArgs(runtimeEArgs, trustAgentCommandPositional(0, "app-server")))
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = buildEnv(b.cfg.Env)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("codex stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("codex stdin pipe: %w", err)
	}

	stderrBuf := newStderrTail(io.Discard, runtimeEStderrTailBytes)
	cmd.Stderr = stderrBuf

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start codex: %w", err)
	}
	activeLaunches := activeRuntimeELaunches.Add(1)
	for {
		maxSeen := maxActiveRuntimeELaunchesObserved.Load()
		if activeLaunches <= maxSeen || maxActiveRuntimeELaunchesObserved.CompareAndSwap(maxSeen, activeLaunches) {
			break
		}
	}
	launchStarted := time.Now()
	runtimeEVersion := strings.TrimSpace(b.cfg.RuntimeEVersion)
	if runtimeEVersion == "" {
		runtimeEVersion = "unknown"
	}

	b.cfg.Logger.Info("codex lifecycle", "phase", "spawn", "task_id", b.cfg.TaskID, "runtime_id", b.cfg.RuntimeID, "pid", cmd.Process.Pid, "process_group", cmd.Process.Pid, "cwd", opts.Cwd, "attempt", attempt, "active_launches", activeLaunches, "codex_version", runtimeEVersion, "daemon_version", b.cfg.DaemonVersion)

	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)
	semanticActivityCh := make(chan string, 256)

	var outputMu sync.Mutex

	var finalAnswer, lastAgentMessage string
	var semanticObserved atomic.Bool
	turnNotificationGate := &runtimeETurnNotificationGate{}

	turnDone := make(chan bool, 1)

	c := &runtimeEClient{
		cfg:                  b.cfg,
		stdin:                stdin,
		pending:              make(map[int]*pendingRPC),
		processDone:          make(chan struct{}),
		handshakeTimeout:     handshakeTimeout,
		pid:                  cmd.Process.Pid,
		attempt:              attempt,
		activeLaunches:       activeLaunches,
		notificationProtocol: "unknown",
		acceptNotification:   turnNotificationGate.accept,
		onDiscardedNotification: func(string, map[string]any) {

			semanticObserved.Store(true)
		},
		onMessage: func(сообщение Message) {
			logRuntimeEAgentMessage(b.cfg.Logger, сообщение)
			if сообщение.Type == MessageText {
				outputMu.Lock()
				lastAgentMessage = сообщение.Content
				outputMu.Unlock()
			}
			trySend(msgCh, сообщение)
			trySendString(semanticActivityCh, describeRuntimeESemanticActivity(сообщение))
			if describeRuntimeESemanticActivity(сообщение) != "" {
				semanticObserved.Store(true)
			}
		},
		onFinalAnswer: func(text string) {
			outputMu.Lock()
			finalAnswer = text
			outputMu.Unlock()
		},
		onSemanticActivity: func(description string) {
			semanticObserved.Store(true)
			b.cfg.Logger.Debug("codex semantic activity observed", "activity", description)
			trySendString(semanticActivityCh, description)
		},
		onTurnDone: func(aborted bool) {
			select {
			case turnDone <- aborted:
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
		if err := scanner.Err(); err != nil {
			c.markProcessExited(fmt.Errorf("%w: %v", errRuntimeEProcessExited, err))
			return
		}
		c.markProcessExited(errRuntimeEProcessExited)
	}()

	var waitOnce sync.Once
	var cleanupConfirmed bool
	var waitReturned bool
	var cleanupWaitErr error
	drainAndWait := func() {
		waitOnce.Do(func() {
			stdin.Close()

			grace := runtimeEGracefulShutdown()
			waitCh := make(chan struct{})
			var startWait sync.Once
			startProcessWait := func() {
				startWait.Do(func() {
					go func() {
						cleanupWaitErr = cmd.Wait()
						close(waitCh)
					}()
				})
			}

			select {
			case <-readerDone:

			case <-time.After(grace):

				b.cfg.Logger.Warn("codex did not close stdout after stdin EOF; forcing shutdown",
					"pid", cmd.Process.Pid,
					"grace", grace.String(),
				)
				cancel()

				startProcessWait()
				<-waitCh
				select {
				case <-readerDone:
				case <-time.After(grace):
					b.cfg.Logger.Warn("codex stdout reader remained open after bounded process wait",
						"pid", cmd.Process.Pid,
						"grace", grace.String(),
					)
				}
			}

			startProcessWait()
			select {
			case <-waitCh:
				waitReturned = true

			case <-time.After(grace):
				b.cfg.Logger.Warn("codex process still alive after reader exited; forcing shutdown",
					"pid", cmd.Process.Pid,
					"grace", grace.String(),
				)
				cancel()

				<-waitCh
				waitReturned = true
			}

			cleanupConfirmed = waitReturned && cmd.ProcessState != nil && waitProcessGroupGone(cmd.Process, grace)
			if runtimeECleanupConfirmationOverride.Load() < 0 {
				cleanupConfirmed = false
			}
			b.cfg.Logger.Info("codex lifecycle",
				"phase", "cleanup",
				"task_id", b.cfg.TaskID,
				"runtime_id", b.cfg.RuntimeID,
				"pid", cmd.Process.Pid,
				"process_group", cmd.Process.Pid,
				"attempt", attempt,
				"latency", time.Since(launchStarted).Round(time.Millisecond).String(),
				"reaped", cleanupConfirmed,
				"exit_status", runtimeEProcessExitStatus(cmd.ProcessState),
				"wait_error", cleanupWaitErr,
				"stderr_bytes", stderrBuf.TotalBytes(),
				"stderr_truncated", stderrBuf.TotalBytes() > runtimeEStderrTailBytes,
			)
		})
	}

	go func() {
		defer activeRuntimeELaunches.Add(-1)
		defer cancel()
		defer close(msgCh)
		defer close(resCh)
		defer drainAndWait()

		startTime := time.Now()
		finalStatus := "completed"
		var finalError string

		initializeStarted := time.Now()
		b.cfg.Logger.Info("codex lifecycle", "phase", "initialize_sent", "task_id", b.cfg.TaskID, "runtime_id", b.cfg.RuntimeID, "pid", cmd.Process.Pid, "attempt", attempt, "active_launches", activeLaunches)
		_, err := c.request(runCtx, "initialize", map[string]any{
			"clientInfo": map[string]any{
				"name":    "goosar-agent-sdk",
				"title":   "Goosar Agent SDK",
				"version": "0.2.0",
			},
			"capabilities": map[string]any{
				"experimentalApi": true,
			},
		})
		if err != nil {
			initializeLatency := time.Since(initializeStarted)
			var handshakeErr *runtimeEHandshakeTimeoutError
			timedOut := errors.As(err, &handshakeErr) && handshakeErr.Method == "initialize"
			if timedOut {

				signalProcessGroup(cmd.Process, syscall.SIGKILL)
			}
			drainAndWait()
			finalStatus = "failed"
			finalError = fmt.Sprintf("codex initialize failed: %v", err)
			contextEnded := errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
			if !timedOut && !contextEnded {

				finalError = withAgentStderr(finalError, "codex", sanitizeRuntimeEDiagnostic(stderrBuf.Tail()))
			}
			retrySafe := timedOut && !semanticObserved.Load() && cleanupConfirmed && runtimeEInitializeRetrySupported()
			if timedOut && !cleanupConfirmed {
				finalError += "; retry suppressed: process cleanup/reap not confirmed"
			} else if timedOut && cleanupConfirmed && !runtimeEInitializeRetrySupported() {
				finalError += "; retry suppressed: process-tree cleanup cannot be confirmed on this platform"
			}
			b.cfg.Logger.Warn("codex lifecycle", "phase", "initialize_failure", "task_id", b.cfg.TaskID, "runtime_id", b.cfg.RuntimeID, "pid", cmd.Process.Pid, "attempt", attempt, "latency", initializeLatency.Round(time.Millisecond).String(), "semantic_activity", semanticObserved.Load(), "cleanup_confirmed", cleanupConfirmed, "retry_safe", retrySafe)
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds(), runtimeEInitializeRetrySafe: retrySafe}
			return
		}
		b.cfg.Logger.Info("codex lifecycle", "phase", "initialize_response", "task_id", b.cfg.TaskID, "runtime_id", b.cfg.RuntimeID, "pid", cmd.Process.Pid, "attempt", attempt, "latency", time.Since(initializeStarted).Round(time.Millisecond).String())
		c.notify("initialized")

		threadID, resumed, err := c.startOrResumeThread(runCtx, opts, b.cfg.Logger)
		if err != nil {
			var handshakeErr *runtimeEHandshakeTimeoutError
			timedOut := errors.As(err, &handshakeErr) && handshakeErr.Method == "thread/start"
			if timedOut {

				signalProcessGroup(cmd.Process, syscall.SIGKILL)
			}
			drainAndWait()
			finalStatus = "failed"
			stderrTail := sanitizeRuntimeEDiagnostic(stderrBuf.Tail())
			finalError = err.Error()
			if c.threadStartSent {
				classification := classifyRuntimeEStartupStderr(stderrTail, timedOut)
				b.cfg.Logger.Warn("codex lifecycle",
					"phase", "thread_start_failure",
					"task_id", b.cfg.TaskID,
					"runtime_id", b.cfg.RuntimeID,
					"pid", cmd.Process.Pid,
					"attempt", attempt,
					"active_launches", activeLaunches,
					"method", "thread/start",
					"latency", time.Since(c.threadStartStarted).Round(time.Millisecond).String(),
					"latency_ms", time.Since(c.threadStartStarted).Milliseconds(),
					"cleanup_confirmed", cleanupConfirmed,
					"reaped", cleanupConfirmed,
					"retry_safe", false,
					"retry_attempted", false,
					"stderr_model_refresh_timeout_count", classification.modelRefreshTimeout,
					"stderr_mcp_init_transport_count", classification.mcpInitTransport,
					"stderr_bare_timeout_count", classification.bareTimeout,
				)
			}
			resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
			return
		}
		c.threadID = threadID
		if resumed {
			b.cfg.Logger.Info("codex thread resumed", "thread_id", threadID)
		} else {
			b.cfg.Logger.Info("codex thread started", "thread_id", threadID)
		}

		turnParams := map[string]any{
			"threadId": threadID,
			"input":    runtimeETurnInput(prompt, opts.ResumeExpected, resumed),
		}

		applyRuntimeEReasoningEffort(turnParams, opts.ThinkingLevel)
		applyRuntimeEServiceTier(turnParams, opts.ServiceTier)
		waitingForTurn := true
		var timeoutDiagnostic runtimeETimeoutDiagnostic
		var processExitErr error
		finishTurn := func(aborted bool) {
			waitingForTurn = false
			switch {
			case aborted:
				finalStatus = "aborted"
				if errMsg := c.getTurnError(); errMsg != "" {
					finalError = errMsg
				} else {
					finalError = "turn was aborted"
				}
			default:
				if errMsg := c.getTurnError(); errMsg != "" {
					finalStatus = "failed"
					finalError = errMsg
				}
			}
		}
		turnNotificationGate.arm()
		_, err = c.request(runCtx, "turn/start", turnParams)
		if err != nil {
			select {
			case aborted := <-turnDone:
				finishTurn(aborted)
			default:
				drainAndWait()
				finalStatus = "failed"
				finalError = withAgentStderr(fmt.Sprintf("codex turn/start failed: %v", err), "codex", sanitizeRuntimeEDiagnostic(stderrBuf.Tail()))
				resCh <- Result{Status: finalStatus, Error: finalError, DurationMs: time.Since(startTime).Milliseconds()}
				return
			}
		}

		lastSemanticActivity := time.Now()
		lastSemanticActivityDescription := "turn/start"
		semanticTimer := time.NewTimer(semanticInactivityTimeout)
		defer semanticTimer.Stop()

		firstTurnNoProgressTimeout := runtimeEFirstTurnNoProgressTimeout(semanticInactivityTimeout)
		var firstTurnNoProgressTimer *time.Timer
		var firstTurnNoProgressTimerC <-chan time.Time
		firstTurnStarted := false
		firstTurnProgressObserved := false
		stopFirstTurnNoProgressTimer := func() {
			if firstTurnNoProgressTimer == nil {
				return
			}
			stopTimer(firstTurnNoProgressTimer)
			firstTurnNoProgressTimerC = nil
		}
		defer stopFirstTurnNoProgressTimer()

		finishRunContextDone := func() {
			waitingForTurn = false
			if runCtx.Err() == context.DeadlineExceeded {
				finalStatus = "timeout"
				finalError = fmt.Sprintf("codex timed out after %s", timeout)
			} else {
				finalStatus = "aborted"
				finalError = "execution cancelled"
			}
		}
		for waitingForTurn {
			select {
			case aborted := <-turnDone:
				finishTurn(aborted)
			case activity := <-semanticActivityCh:
				lastSemanticActivity = time.Now()
				lastSemanticActivityDescription = activity
				resetTimer(semanticTimer, semanticInactivityTimeout)
				if activity == "status:running" && !firstTurnStarted {
					firstTurnStarted = true
					firstTurnNoProgressTimer = time.NewTimer(firstTurnNoProgressTimeout)
					firstTurnNoProgressTimerC = firstTurnNoProgressTimer.C
				} else if firstTurnStarted && !firstTurnProgressObserved && isRuntimeEFirstTurnProgressActivity(activity) {
					firstTurnProgressObserved = true
					stopFirstTurnNoProgressTimer()
				}
			case <-firstTurnNoProgressTimerC:
				waitingForTurn = false
				finalStatus = "timeout"
				timeoutDiagnostic = runtimeETimeoutDiagnostic{
					Kind:         runtimeETimeoutFirstTurnNoProgress,
					Timeout:      firstTurnNoProgressTimeout,
					LastActivity: lastSemanticActivityDescription,
					ThreadID:     threadID,
					TurnID:       c.turnID,
					Model:        opts.Model,
				}
				b.cfg.Logger.Warn(RuntimeEFirstTurnNoProgressMarker,
					"pid", cmd.Process.Pid,
					"thread_id", threadID,
					"turn_id", c.turnID,
					"timeout", firstTurnNoProgressTimeout.String(),
					"last_activity", lastSemanticActivityDescription,
				)
			case <-semanticTimer.C:
				waitingForTurn = false
				finalStatus = "timeout"
				timeoutDiagnostic = runtimeETimeoutDiagnostic{
					Kind:         runtimeETimeoutSemanticInactivity,
					Timeout:      semanticInactivityTimeout,
					LastActivity: lastSemanticActivityDescription,
					ThreadID:     threadID,
					TurnID:       c.turnID,
					Model:        opts.Model,
				}
				b.cfg.Logger.Warn(RuntimeESemanticInactivityMarker,
					"pid", cmd.Process.Pid,
					"thread_id", threadID,
					"turn_id", c.turnID,
					"timeout", semanticInactivityTimeout.String(),
					"last_activity", lastSemanticActivityDescription,
					"idle_for", time.Since(lastSemanticActivity).Round(time.Millisecond).String(),
				)
			case <-runCtx.Done():
				finishRunContextDone()
			case <-c.processDone:
				select {
				case aborted := <-turnDone:
					finishTurn(aborted)
				default:
					if runCtx.Err() != nil {
						finishRunContextDone()
					} else {
						waitingForTurn = false
						finalStatus = "failed"
						processExitErr = c.getProcessErr()
						if processExitErr == nil {
							processExitErr = errRuntimeEProcessExited
						}
						finalError = processExitErr.Error()
					}
				}
			}
		}

		duration := time.Since(startTime)
		b.cfg.Logger.Info("codex finished", "pid", cmd.Process.Pid, "status", finalStatus, "duration", duration.Round(time.Millisecond).String())

		drainAndWait()

		if processExitErr != nil {
			finalError = withAgentStderr(processExitErr.Error(), "codex", sanitizeRuntimeEDiagnostic(stderrBuf.Tail()))
		}
		stderrTail := sanitizeRuntimeEDiagnostic(stderrBuf.Tail())
		if timeoutDiagnostic.Kind != runtimeETimeoutNone {
			timeoutDiagnostic.RuntimeEVersion = detectRuntimeEVersionForDiagnostics(context.Background(), execPath, cmd.Env, b.cfg.Logger)
			finalError = buildRuntimeETimeoutDiagnosticError(timeoutDiagnostic, stderrTail)
		}

		startupRefreshRetrySafe := timeoutDiagnostic.Kind == runtimeETimeoutFirstTurnNoProgress &&
			!firstTurnProgressObserved &&
			strings.Contains(stderrTail, runtimeEModelCatalogRefreshFailureSignal) &&
			cleanupConfirmed && runtimeEInitializeRetrySupported()
		if startupRefreshRetrySafe {
			b.cfg.Logger.Warn("codex startup model catalog refresh failure is retry safe",
				"pid", cmd.Process.Pid,
				"thread_id", threadID,
				"attempt", attempt,
			)
		}

		outputMu.Lock()
		finalOutput := runtimeEDeliverableOutput(finalAnswer, lastAgentMessage)
		outputMu.Unlock()

		var usageMap map[string]TokenUsage
		c.usageMu.Lock()
		u := c.usage
		c.usageMu.Unlock()

		if u.InputTokens == 0 && u.OutputTokens == 0 {
			taskRuntimeEHome := strings.TrimSpace(b.cfg.Env["CODEX_HOME"])
			if scanned := scanRuntimeESessionUsage(startTime, taskRuntimeEHome, threadID, resumed); scanned != nil {
				u = scanned.usage
				if scanned.model != "" && opts.Model == "" {
					opts.Model = scanned.model
				}
			}
		}

		if u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheReadTokens > 0 || u.CacheWriteTokens > 0 {
			model := opts.Model
			if model == "" {
				model = "unknown"
			}
			usageMap = map[string]TokenUsage{model: u}
		}

		resCh <- Result{
			Status:                          finalStatus,
			Output:                          finalOutput,
			Error:                           finalError,
			SessionID:                       threadID,
			DurationMs:                      duration.Milliseconds(),
			Usage:                           usageMap,
			runtimeEStartupRefreshRetrySafe: startupRefreshRetrySafe,
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

const runtimeEResumeUnavailableNotice = "[System notice] You were expected to continue an earlier conversation, but restoring that session failed and this is a fresh thread with no memory of the previous turns. Rebuild context from the issue/thread, and when you reply, tell the user up front (one short sentence) that the previous conversation context could not be restored and this is a new session.\n\n"

func runtimeETurnInput(prompt string, resumeExpected, resumed bool) []map[string]any {
	text := prompt
	if resumeExpected && !resumed {
		text = runtimeEResumeUnavailableNotice + prompt
	}
	return []map[string]any{{"type": "text", "text": text}}
}

func (c *runtimeEClient) startOrResumeThread(ctx context.Context, opts ExecOptions, logger *slog.Logger) (string, bool, error) {
	if priorThreadID := opts.ResumeSessionID; priorThreadID != "" {

		resumeParams := map[string]any{
			"threadId":              priorThreadID,
			"cwd":                   opts.Cwd,
			"model":                 nilIfEmpty(opts.Model),
			"developerInstructions": nilIfEmpty(opts.SystemPrompt),
		}

		applyRuntimeEReasoningEffort(resumeParams, opts.ThinkingLevel)
		applyRuntimeEServiceTier(resumeParams, opts.ServiceTier)
		resumeResult, err := c.request(ctx, "thread/resume", resumeParams)
		if err == nil {
			if threadID := extractThreadID(resumeResult); threadID != "" {
				return threadID, true, nil
			}
			logger.Warn("codex thread/resume returned no thread ID; falling back to thread/start", "prior_thread_id", priorThreadID)
		} else {
			if isRuntimeETransportError(err) {
				logger.Warn("codex thread/resume failed due to transport error; not falling back to thread/start", "prior_thread_id", priorThreadID, "error", err)
				return "", false, fmt.Errorf("codex thread/resume failed: %w", err)
			}
			logger.Warn("codex thread/resume failed; falling back to thread/start", "prior_thread_id", priorThreadID, "error", err)
		}
	}

	startParams := map[string]any{
		"model":                  nilIfEmpty(opts.Model),
		"modelProvider":          nil,
		"profile":                nil,
		"cwd":                    opts.Cwd,
		"approvalPolicy":         nil,
		"sandbox":                nil,
		"config":                 nil,
		"baseInstructions":       nil,
		"developerInstructions":  nilIfEmpty(opts.SystemPrompt),
		"compactPrompt":          nil,
		"includeApplyPatchTool":  nil,
		"experimentalRawEvents":  false,
		"persistExtendedHistory": true,
	}
	applyRuntimeEReasoningEffort(startParams, opts.ThinkingLevel)
	applyRuntimeEServiceTier(startParams, opts.ServiceTier)
	c.threadStartSent = true
	c.threadStartStarted = time.Now()
	logger.Info("codex lifecycle",
		"phase", "thread_start_sent",
		"task_id", c.cfg.TaskID,
		"runtime_id", c.cfg.RuntimeID,
		"pid", c.pid,
		"attempt", c.attempt,
		"active_launches", c.activeLaunches,
		"method", "thread/start",
	)
	startResult, err := c.request(ctx, "thread/start", startParams)
	if err != nil {
		return "", false, fmt.Errorf("codex thread/start failed: %w", err)
	}
	threadID := extractThreadID(startResult)
	if threadID == "" {
		return "", false, fmt.Errorf("codex thread/start returned no thread ID")
	}
	logger.Info("codex lifecycle",
		"phase", "thread_start_response",
		"task_id", c.cfg.TaskID,
		"runtime_id", c.cfg.RuntimeID,
		"pid", c.pid,
		"attempt", c.attempt,
		"active_launches", c.activeLaunches,
		"method", "thread/start",
		"latency", time.Since(c.threadStartStarted).Round(time.Millisecond).String(),
		"latency_ms", time.Since(c.threadStartStarted).Milliseconds(),
	)
	c.trySetThreadName(ctx, threadID, opts.ThreadName, logger)
	return threadID, false, nil
}

func (c *runtimeEClient) trySetThreadName(ctx context.Context, threadID, name string, logger *slog.Logger) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	if err := c.setThreadName(ctx, threadID, name); err != nil {
		logger.Warn("codex thread/name/set failed; continuing without provider-native thread title",
			"thread_id", threadID, "error", err)
	}
}

func (c *runtimeEClient) setThreadName(ctx context.Context, threadID, name string) error {
	_, err := c.request(ctx, "thread/name/set", map[string]any{
		"threadId": threadID,
		"name":     name,
	})
	return err
}

func applyRuntimeEReasoningEffort(params map[string]any, level string) {
	if params == nil || level == "" {
		return
	}
	if _, isTurnStart := params["input"]; isTurnStart {
		params["effort"] = level
		return
	}
	cfg, _ := params["config"].(map[string]any)
	if cfg == nil {
		cfg = map[string]any{}
	}
	cfg["model_reasoning_effort"] = level
	params["config"] = cfg
}

func applyRuntimeEServiceTier(params map[string]any, tier string) {
	if params == nil || tier == "" {
		return
	}
	params["serviceTier"] = tier
}

func resetTimer(timer *time.Timer, d time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(d)
}

func stopTimer(timer *time.Timer) {
	if timer == nil {
		return
	}
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}

func runtimeEFirstTurnNoProgressTimeout(semanticInactivityTimeout time.Duration) time.Duration {
	if semanticInactivityTimeout <= 0 || semanticInactivityTimeout > defaultRuntimeEFirstTurnNoProgressTimeout {
		return defaultRuntimeEFirstTurnNoProgressTimeout
	}
	scaled := semanticInactivityTimeout * 4 / 5
	if scaled <= 0 {
		return semanticInactivityTimeout
	}
	return scaled
}

func isRuntimeEFirstTurnProgressActivity(activity string) bool {
	return activity != "" && activity != "status:running" && activity != "error:retry"
}

func buildRuntimeETimeoutDiagnosticError(diag runtimeETimeoutDiagnostic, stderrTail string) string {
	stderrTail = sanitizeRuntimeEDiagnostic(stderrTail)
	var сообщение string
	switch diag.Kind {
	case runtimeETimeoutFirstTurnNoProgress:
		сообщение = fmt.Sprintf("%s after %s: received turn start but no item, message, tool, turn/completed, or error event (%s)",
			RuntimeEFirstTurnNoProgressMarker,
			diag.Timeout,
			formatRuntimeEDiagnosticFields(diag),
		)
	case runtimeETimeoutSemanticInactivity:
		сообщение = fmt.Sprintf("%s after %s without agent progress (last activity: %s; %s)",
			RuntimeESemanticInactivityMarker,
			diag.Timeout,
			nonEmptyRuntimeEDiagnosticValue(diag.LastActivity),
			formatRuntimeEDiagnosticFields(diag),
		)
	default:
		сообщение = "codex timed out"
	}
	сообщение = appendRuntimeEKnownStderrHint(сообщение, stderrTail)
	return withAgentStderr(сообщение, "codex", stderrTail)
}

func formatRuntimeEDiagnosticFields(diag runtimeETimeoutDiagnostic) string {
	return fmt.Sprintf("codex_version=%q thread_id=%q turn_id=%q model=%q",
		nonEmptyRuntimeEDiagnosticValue(diag.RuntimeEVersion),
		nonEmptyRuntimeEDiagnosticValue(diag.ThreadID),
		nonEmptyRuntimeEDiagnosticValue(diag.TurnID),
		formatRuntimeEDiagnosticModel(diag.Model),
	)
}

func nonEmptyRuntimeEDiagnosticValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return "unknown"
	}
	return value
}

func formatRuntimeEDiagnosticModel(model string) string {
	if strings.TrimSpace(model) == "" {
		return "default(empty)"
	}
	return model
}

func appendRuntimeEKnownStderrHint(сообщение, stderrTail string) string {
	if strings.Contains(stderrTail, runtimeEModelCatalogRefreshFailureSignal) {
		return сообщение + "; diagnosis: Codex could not load its model catalog, which blocks the first turn. This is usually a transient network failure reaching the Codex service. Check network/proxy connectivity and retry the task, or switch to another runtime while the Codex service is unreachable"
	}
	return сообщение
}

func detectRuntimeEVersionForDiagnostics(ctx context.Context, execPath string, env []string, logger *slog.Logger) string {
	versionCtx, cancel := context.WithTimeout(ctx, runtimeEVersionDiagnosticTimeout)
	defer cancel()

	cmd := newRuntimeCmd(exec.CommandContext(versionCtx, execPath, "--version"))
	cmd.Env = env
	data, err := cmd.Output()
	if err != nil {
		if logger != nil {
			logger.Debug("codex version diagnostic failed", "error", err)
		}
		return "unknown"
	}
	version := extractVersionLine(string(data))
	if strings.TrimSpace(version) == "" {
		return "unknown"
	}
	return version
}

func trySendString(ch chan<- string, value string) {
	select {
	case ch <- value:
	default:
	}
}

func runtimeEDeliverableOutput(finalAnswer, lastAgentMessage string) string {
	if finalAnswer != "" {
		return finalAnswer
	}
	return lastAgentMessage
}

func logRuntimeEAgentMessage(logger *slog.Logger, сообщение Message) {
	if logger == nil {
		return
	}
	attrs := []any{
		"type", string(сообщение.Type),
		"tool", сообщение.Tool,
		"call_id", сообщение.CallID,
		"status", сообщение.Status,
		"content_len", len(сообщение.Content),
		"output_len", len(сообщение.Output),
	}
	logger.Info("codex agent message received", attrs...)
	if сообщение.Type == MessageToolResult {
		logger.Info("codex tool_result observed", "tool", сообщение.Tool, "call_id", сообщение.CallID, "output_len", len(сообщение.Output))
	}
}

func describeRuntimeESemanticActivity(сообщение Message) string {
	switch сообщение.Type {
	case MessageToolUse, MessageToolResult:
		if сообщение.Tool != "" {
			return fmt.Sprintf("%s:%s", сообщение.Type, сообщение.Tool)
		}
	case MessageStatus:
		if сообщение.Status != "" {
			return fmt.Sprintf("%s:%s", сообщение.Type, сообщение.Status)
		}
	}
	return string(сообщение.Type)
}

type runtimeEClient struct {
	cfg                Config
	stdin              interface{ Write([]byte) (int, error) }
	mu                 sync.Mutex
	nextID             int
	pending            map[int]*pendingRPC
	processDone        chan struct{}
	processErr         error
	handshakeTimeout   time.Duration
	pid                int
	attempt            int
	activeLaunches     int64
	threadStartSent    bool
	threadStartStarted time.Time
	threadID           string
	turnID             string
	onMessage          func(Message)
	onSemanticActivity func(description string)
	onTurnDone         func(aborted bool)

	onFinalAnswer func(text string)

	acceptNotification func(method string, params map[string]any) bool

	onDiscardedNotification func(method string, params map[string]any)

	notificationProtocol string
	turnStarted          bool
	completedTurnIDs     map[string]bool

	usageMu sync.Mutex
	usage   TokenUsage

	turnErrorMu sync.Mutex
	turnError   string
}

type runtimeETurnNotificationGate struct {
	armed   atomic.Bool
	started bool
	turnID  string
}

func (g *runtimeETurnNotificationGate) arm() {
	g.armed.Store(true)
}

func (g *runtimeETurnNotificationGate) accept(method string, params map[string]any) bool {
	if !g.armed.Load() {
		return false
	}

	if method == "codex/event" || strings.HasPrefix(method, "codex/event/") {
		сообщение, _ := params["msg"].(map[string]any)
		msgType, _ := сообщение["type"].(string)
		if msgType == "task_started" {
			g.started = true
			return true
		}

		return true
	}

	switch {
	case method == "turn/started":
		g.started = true
		g.turnID = extractNestedString(params, "turn", "id")
		return true
	case method == "turn/completed":
		if !g.started {

			return true
		}
		turnID := extractNestedString(params, "turn", "id")
		return g.turnID == "" || turnID == "" || turnID == g.turnID
	case method == "thread/status/changed" || strings.HasPrefix(method, "item/"):
		if !g.started {
			return true
		}
		turnID, _ := params["turnId"].(string)
		return g.turnID == "" || turnID == "" || turnID == g.turnID
	default:

		return true
	}
}

func (c *runtimeEClient) setTurnError(сообщение string) {
	if сообщение == "" {
		return
	}
	c.turnErrorMu.Lock()
	defer c.turnErrorMu.Unlock()
	if c.turnError == "" {
		c.turnError = сообщение
	}
}

func (c *runtimeEClient) getTurnError() string {
	c.turnErrorMu.Lock()
	defer c.turnErrorMu.Unlock()
	return c.turnError
}

type pendingRPC struct {
	ch     chan rpcResult
	method string
}

type rpcResult struct {
	result json.RawMessage
	err    error
}

type runtimeEHandshakeTimeoutError struct {
	Method  string
	Timeout time.Duration
}

func (e *runtimeEHandshakeTimeoutError) Error() string {
	return fmt.Sprintf("%s: %s did not respond after %s", RuntimeEHandshakeTimeoutMarker, e.Method, e.Timeout)
}

func (e *runtimeEHandshakeTimeoutError) Unwrap() error {
	return context.DeadlineExceeded
}

func isRuntimeEHandshakeRPC(method string) bool {
	switch method {
	case "initialize", "thread/start", "thread/resume", "thread/name/set", "turn/start":
		return true
	default:
		return false
	}
}

func runtimeERequestContextError(ctx context.Context) error {
	var handshakeErr *runtimeEHandshakeTimeoutError
	if errors.As(context.Cause(ctx), &handshakeErr) {
		return handshakeErr
	}
	return ctx.Err()
}

func (c *runtimeEClient) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	requestCtx := ctx
	cancelRequest := func() {}
	if c.handshakeTimeout > 0 && isRuntimeEHandshakeRPC(method) {
		timeoutErr := &runtimeEHandshakeTimeoutError{Method: method, Timeout: c.handshakeTimeout}
		requestCtx, cancelRequest = context.WithTimeoutCause(ctx, c.handshakeTimeout, timeoutErr)
	}
	defer cancelRequest()

	c.mu.Lock()
	if c.processErr != nil {
		err := c.processErr
		c.mu.Unlock()
		return nil, err
	}
	if c.processDone == nil {
		c.processDone = make(chan struct{})
	}
	processDone := c.processDone
	c.nextID++
	id := c.nextID
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
	if _, err := c.stdin.Write(data); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("write %s: %w", method, err)
	}
	if method == "turn/start" {
		threadID := ""
		if paramMap, ok := params.(map[string]any); ok {
			threadID, _ = paramMap["threadId"].(string)
		}
		c.cfg.Logger.Info("codex turn/start sent", "request_id", id, "thread_id", threadID)
	}

	select {
	case res := <-pr.ch:
		return res.result, res.err
	case <-processDone:
		select {
		case res := <-pr.ch:
			return res.result, res.err
		default:
		}
		c.mu.Lock()
		delete(c.pending, id)
		err := c.processErr
		c.mu.Unlock()
		if requestCtx.Err() != nil {
			return nil, runtimeERequestContextError(requestCtx)
		}
		if err == nil {
			err = errRuntimeEProcessExited
		}
		return nil, err
	case <-requestCtx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, runtimeERequestContextError(requestCtx)
	}
}

func (c *runtimeEClient) notify(method string) {
	сообщение := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
	}
	data, _ := json.Marshal(сообщение)
	data = append(data, '\n')
	_, _ = c.stdin.Write(data)
}

func (c *runtimeEClient) respond(id int, result any) {
	сообщение := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	}
	data, _ := json.Marshal(сообщение)
	data = append(data, '\n')
	_, _ = c.stdin.Write(data)
}

func (c *runtimeEClient) respondError(id int, code int, текст string) {
	сообщение := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]any{
			"code":    code,
			"message": текст,
		},
	}
	data, _ := json.Marshal(сообщение)
	data = append(data, '\n')
	_, _ = c.stdin.Write(data)
}

func (c *runtimeEClient) closeAllPending(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, pr := range c.pending {
		pr.ch <- rpcResult{err: err}
		delete(c.pending, id)
	}
}

func (c *runtimeEClient) markProcessExited(err error) {
	if err == nil {
		err = errRuntimeEProcessExited
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.processErr == nil {
		c.processErr = err
		if c.processDone != nil {
			close(c.processDone)
		}
	}
	for id, pr := range c.pending {
		pr.ch <- rpcResult{err: err}
		delete(c.pending, id)
	}
}

func (c *runtimeEClient) getProcessErr() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.processErr
}

func isRuntimeETransportError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errRuntimeEProcessExited) {
		return true
	}
	var handshakeErr *runtimeEHandshakeTimeoutError
	if errors.As(err, &handshakeErr) {
		return true
	}
	return strings.HasPrefix(err.Error(), "write ")
}

func (c *runtimeEClient) handleLine(line string) {
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
			c.handleServerRequest(raw)
			return
		}
	}

	if _, hasMethod := raw["method"]; hasMethod {
		c.handleNotification(raw)
	}
}

func (c *runtimeEClient) handleResponse(raw map[string]json.RawMessage) {
	var id int
	if err := json.Unmarshal(raw["id"], &id); err != nil {
		return
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
			Code    int    `json:"code"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(errData, &rpcErr)
		pr.ch <- rpcResult{err: fmt.Errorf("%s: %s (code=%d)", pr.method, rpcErr.Message, rpcErr.Code)}
	} else {
		pr.ch <- rpcResult{result: raw["result"]}
	}
}

func (c *runtimeEClient) handleServerRequest(raw map[string]json.RawMessage) {
	var id int
	_ = json.Unmarshal(raw["id"], &id)

	var method string
	_ = json.Unmarshal(raw["method"], &method)

	switch method {
	case "item/commandExecution/requestApproval", "execCommandApproval":
		c.respond(id, map[string]any{"decision": "accept"})
	case "item/fileChange/requestApproval", "applyPatchApproval":
		c.respond(id, map[string]any{"decision": "accept"})
	case "item/permissions/requestApproval":
		c.respond(id, runtimeEPermissionsApprovalResponse(raw["params"], c.cfg.Logger))
	case "mcpServer/elicitation/request":
		c.respond(id, map[string]any{"action": "accept", "content": nil, "_meta": nil})
	default:
		сообщение := fmt.Sprintf("unsupported codex app-server request: %s", method)
		c.cfg.Logger.Warn("codex: unhandled server request", "method", method, "id", id)
		c.setTurnError(сообщение)
		c.respondError(id, -32601, сообщение)
	}
}

func runtimeEPermissionsApprovalResponse(params json.RawMessage, logger *slog.Logger) map[string]any {
	var payload struct {
		Permissions map[string]any `json:"permissions"`
	}
	if err := json.Unmarshal(params, &payload); err != nil && logger != nil {
		logger.Warn("codex: failed to parse permission approval request; granting empty turn-scoped profile", "error", err)
	}

	granted := map[string]any{}
	var dropped []string
	for key, value := range payload.Permissions {
		switch key {
		case "network", "fileSystem":
			if value != nil {
				granted[key] = value
			}
		default:
			dropped = append(dropped, key)
		}
	}
	if len(dropped) > 0 && logger != nil {
		sort.Strings(dropped)
		logger.Warn("codex: dropping unrecognized permission keys from approval request; add explicit handling if the app-server protocol expanded", "keys", dropped)
	}

	return map[string]any{
		"permissions": granted,
		"scope":       "turn",
	}
}

func (c *runtimeEClient) handleNotification(raw map[string]json.RawMessage) {
	var method string
	_ = json.Unmarshal(raw["method"], &method)

	var params map[string]any
	if p, ok := raw["params"]; ok {
		_ = json.Unmarshal(p, &params)
	}

	if c.isNotificationFromOtherThread(params) {
		return
	}
	if c.acceptNotification != nil && !c.acceptNotification(method, params) {
		if c.onDiscardedNotification != nil {
			c.onDiscardedNotification(method, params)
		}
		return
	}

	if method == "codex/event" || strings.HasPrefix(method, "codex/event/") {
		c.notificationProtocol = "legacy"
		msgData, ok := params["msg"]
		if !ok {
			return
		}
		msgMap, ok := msgData.(map[string]any)
		if !ok {
			return
		}
		c.handleEvent(msgMap)
		return
	}

	if c.notificationProtocol != "legacy" {
		if c.notificationProtocol == "unknown" &&
			(method == "turn/started" || method == "turn/completed" ||
				method == "thread/started" || strings.HasPrefix(method, "item/")) {
			c.notificationProtocol = "raw"
		}

		if c.notificationProtocol == "raw" {
			c.handleRawNotification(method, params)
		}
	}
}

func (c *runtimeEClient) handleEvent(сообщение map[string]any) {
	msgType, _ := сообщение["type"].(string)

	switch msgType {
	case "task_started":
		c.turnStarted = true
		if c.onMessage != nil {
			c.onMessage(Message{Type: MessageStatus, Status: "running", SessionID: c.threadID})
		}
	case "agent_message":
		text, _ := сообщение["message"].(string)
		if text != "" && c.onMessage != nil {
			c.onMessage(Message{Type: MessageText, Content: text})
		}
	case "exec_command_begin":
		callID, _ := сообщение["call_id"].(string)
		command, _ := сообщение["command"].(string)
		if c.onMessage != nil {
			c.onMessage(Message{
				Type:   MessageToolUse,
				Tool:   "exec_command",
				CallID: callID,
				Input:  map[string]any{"command": command},
			})
		}
	case "exec_command_end":
		callID, _ := сообщение["call_id"].(string)
		output, _ := сообщение["output"].(string)
		if c.onMessage != nil {
			c.onMessage(Message{
				Type:   MessageToolResult,
				Tool:   "exec_command",
				CallID: callID,
				Output: output,
			})
		}
	case "patch_apply_begin":
		callID, _ := сообщение["call_id"].(string)
		if c.onMessage != nil {
			c.onMessage(Message{
				Type:   MessageToolUse,
				Tool:   "patch_apply",
				CallID: callID,
			})
		}
	case "patch_apply_end":
		callID, _ := сообщение["call_id"].(string)
		if c.onMessage != nil {
			c.onMessage(Message{
				Type:   MessageToolResult,
				Tool:   "patch_apply",
				CallID: callID,
			})
		}
	case "task_complete":

		c.extractUsageFromMap(сообщение)
		if c.onTurnDone != nil {
			c.onTurnDone(false)
		}
	case "turn_aborted":
		if c.onTurnDone != nil {
			c.onTurnDone(true)
		}
	}
}

func (c *runtimeEClient) handleRawNotification(method string, params map[string]any) {

	if c.isNotificationFromOtherThread(params) {
		return
	}

	switch method {
	case "turn/started":
		c.turnStarted = true
		if turnID := extractNestedString(params, "turn", "id"); turnID != "" {
			c.turnID = turnID
		}
		if c.onMessage != nil {
			c.onMessage(Message{Type: MessageStatus, Status: "running", SessionID: c.threadID})
		}

	case "turn/completed":
		turnID := extractNestedString(params, "turn", "id")
		status := extractNestedString(params, "turn", "status")
		threadID, _ := params["threadId"].(string)
		c.cfg.Logger.Info("codex turn/completed received", "thread_id", threadID, "turn_id", turnID, "status", status)
		aborted := status == "cancelled" || status == "canceled" ||
			status == "aborted" || status == "interrupted"

		if status == "failed" {
			errMsg := extractNestedString(params, "turn", "error", "message")
			if errMsg == "" {
				errMsg = "codex turn failed"
			}
			c.setTurnError(errMsg)
		}

		if c.completedTurnIDs == nil {
			c.completedTurnIDs = map[string]bool{}
		}
		if turnID != "" {
			if c.completedTurnIDs[turnID] {
				return
			}
			c.completedTurnIDs[turnID] = true
		}

		if turn, ok := params["turn"].(map[string]any); ok {
			c.extractUsageFromMap(turn)
		}

		if c.onTurnDone != nil {
			c.onTurnDone(aborted)
		}

	case "error":

		willRetry, _ := params["willRetry"].(bool)
		errMsg := extractNestedString(params, "error", "message")
		if errMsg == "" {
			errMsg = extractNestedString(params, "message")
		}
		if errMsg != "" {
			c.cfg.Logger.Warn("codex error notification", "message", errMsg, "will_retry", willRetry)
			if c.onSemanticActivity != nil {
				if willRetry {
					c.onSemanticActivity("error:retry")
				} else {
					c.onSemanticActivity("error:terminal")
				}
			}
			if !willRetry {
				c.setTurnError(errMsg)
				if c.onTurnDone != nil {
					c.onTurnDone(false)
				}
			}
		}

	case "thread/status/changed":
		statusType := extractNestedString(params, "status", "type")
		if statusType == "idle" && c.turnStarted {
			if c.onTurnDone != nil {
				c.onTurnDone(false)
			}
		}

	default:
		if strings.HasPrefix(method, "item/") {
			c.handleItemNotification(method, params)
		}
	}
}

func (c *runtimeEClient) isNotificationFromOtherThread(params map[string]any) bool {
	threadID, ok := params["threadId"].(string)
	return ok && c.threadID != "" && threadID != c.threadID
}

func (c *runtimeEClient) handleItemNotification(method string, params map[string]any) {
	item, _ := params["item"].(map[string]any)
	itemType, _ := item["type"].(string)
	itemID, _ := item["id"].(string)
	if isRuntimeEItemProgressActivity(method) && c.onSemanticActivity != nil {
		c.onSemanticActivity(describeRuntimeEItemProgressActivity(method, itemType, itemID))
	}
	if item == nil {
		return
	}

	switch {
	case method == "item/started" && itemType == "commandExecution":
		command, _ := item["command"].(string)
		if c.onMessage != nil {
			c.onMessage(Message{
				Type:   MessageToolUse,
				Tool:   "exec_command",
				CallID: itemID,
				Input:  map[string]any{"command": command},
			})
		}

	case method == "item/completed" && itemType == "commandExecution":
		output, _ := item["aggregatedOutput"].(string)
		if c.onMessage != nil {
			c.onMessage(Message{
				Type:   MessageToolResult,
				Tool:   "exec_command",
				CallID: itemID,
				Output: output,
			})
		}

	case method == "item/started" && itemType == "fileChange":
		if c.onMessage != nil {
			c.onMessage(Message{
				Type:   MessageToolUse,
				Tool:   "patch_apply",
				CallID: itemID,
			})
		}

	case method == "item/completed" && itemType == "fileChange":
		if c.onMessage != nil {
			c.onMessage(Message{
				Type:   MessageToolResult,
				Tool:   "patch_apply",
				CallID: itemID,
			})
		}

	case method == "item/completed" && itemType == "agentMessage":
		text, _ := item["text"].(string)
		if text != "" && c.onMessage != nil {
			c.onMessage(Message{Type: MessageText, Content: text})
		}
		phase, _ := item["phase"].(string)
		if phase == "final_answer" {

			if text != "" && c.onFinalAnswer != nil {
				c.onFinalAnswer(text)
			}
			if c.turnStarted && c.onTurnDone != nil {
				c.onTurnDone(false)
			}
		}
	}
}

func isRuntimeEItemProgressActivity(method string) bool {
	return strings.HasPrefix(method, "item/")
}

func describeRuntimeEItemProgressActivity(method, itemType, itemID string) string {
	if itemType == "" {
		itemType = "unknown"
	}
	if itemID == "" {
		return fmt.Sprintf("%s:%s", method, itemType)
	}
	return fmt.Sprintf("%s:%s:%s", method, itemType, itemID)
}

func (c *runtimeEClient) extractUsageFromMap(data map[string]any) {

	var usageMap map[string]any
	for _, key := range []string{"usage", "token_usage", "tokens"} {
		if v, ok := data[key].(map[string]any); ok {
			usageMap = v
			break
		}
	}
	if usageMap == nil {
		return
	}

	c.usageMu.Lock()
	defer c.usageMu.Unlock()

	inputTokens := runtimeEInt64(usageMap, "input_tokens", "input", "prompt_tokens")
	cacheReadTokens := runtimeEInt64(usageMap, "cached_input_tokens", "cache_read_tokens", "cache_read_input_tokens")
	c.usage.InputTokens += runtimeEUncachedInputTokens(inputTokens, cacheReadTokens)
	c.usage.OutputTokens += runtimeEInt64(usageMap, "output_tokens", "output", "completion_tokens")
	c.usage.CacheReadTokens += cacheReadTokens
	c.usage.CacheWriteTokens += runtimeEInt64(usageMap, "cache_write_tokens", "cache_creation_input_tokens")
}

func runtimeEUncachedInputTokens(inputTokens, cachedInputTokens int64) int64 {
	uncached := inputTokens - cachedInputTokens
	if uncached < 0 {
		return 0
	}
	return uncached
}

func runtimeEInt64(m map[string]any, keys ...string) int64 {
	for _, key := range keys {
		switch v := m[key].(type) {
		case float64:
			if v != 0 {
				return int64(v)
			}
		case int64:
			if v != 0 {
				return v
			}
		}
	}
	return 0
}

type runtimeESessionUsage struct {
	usage TokenUsage
	model string
}

func scanRuntimeESessionUsage(startTime time.Time, runtimeEHome, threadID string, resumed bool) *runtimeESessionUsage {
	root := runtimeESessionRoot(runtimeEHome)
	if root == "" || strings.TrimSpace(threadID) == "" {
		return nil
	}

	type candidate struct {
		path    string
		modTime time.Time
	}
	var files []candidate
	for _, path := range findRuntimeESessionRollouts(root, threadID) {
		info, err := os.Stat(path)
		if err != nil || info.ModTime().Before(startTime) {
			continue
		}
		files = append(files, candidate{path: path, modTime: info.ModTime()})
	}
	if len(files) == 0 {
		return nil
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].modTime.Equal(files[j].modTime) {
			return files[i].path < files[j].path
		}
		return files[i].modTime.Before(files[j].modTime)
	})

	result := parseRuntimeESessionFileSince(files[len(files)-1].path, startTime, resumed)
	if result == nil || (result.usage.InputTokens == 0 && result.usage.OutputTokens == 0 &&
		result.usage.CacheReadTokens == 0 && result.usage.CacheWriteTokens == 0) {
		return nil
	}
	return result
}

func findRuntimeESessionRollouts(root, threadID string) []string {
	threadID = strings.TrimSpace(threadID)
	if root == "" || threadID == "" {
		return nil
	}

	patterns := []string{
		filepath.Join(root, "rollout-*.jsonl"),
		filepath.Join(root, "*", "*", "*", "rollout-*.jsonl"),
	}
	seen := make(map[string]bool)
	var candidates []string
	for _, pattern := range patterns {
		paths, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, path := range paths {
			if seen[path] {
				continue
			}
			seen[path] = true
			candidates = append(candidates, path)
		}
	}

	suffix := "-" + threadID + ".jsonl"
	var matches []string
	for _, path := range candidates {
		if !strings.HasSuffix(filepath.Base(path), suffix) {
			continue
		}
		if metadataID, ok := readRuntimeERolloutThreadID(path); ok && metadataID != threadID {
			continue
		}
		matches = append(matches, path)
	}
	if len(matches) > 0 {
		return matches
	}

	for _, path := range candidates {
		if metadataID, ok := readRuntimeERolloutThreadID(path); ok && metadataID == threadID {
			matches = append(matches, path)
		}
	}
	return matches
}

func readRuntimeERolloutThreadID(path string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()

	var evt struct {
		Type    string `json:"type"`
		Payload *struct {
			ID string `json:"id"`
		} `json:"payload"`
	}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for lineCount := 0; lineCount < 64 && scanner.Scan(); lineCount++ {
		line := scanner.Bytes()
		if !bytesContainsStr(line, "session_meta") {
			continue
		}
		if err := json.Unmarshal(line, &evt); err == nil && evt.Type == "session_meta" && evt.Payload != nil && evt.Payload.ID != "" {
			return evt.Payload.ID, true
		}
	}
	return "", false
}

func runtimeESessionRoot(runtimeEHome string) string {
	if runtimeEHome = strings.TrimSpace(runtimeEHome); runtimeEHome == "" {
		runtimeEHome = os.Getenv("CODEX_HOME")
	}
	if runtimeEHome != "" {
		dir := filepath.Join(runtimeEHome, "sessions")
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	dir := filepath.Join(home, ".codex", "sessions")
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir
	}
	return ""
}

type runtimeERawTokenUsage struct {
	InputTokens           int64 `json:"input_tokens"`
	OutputTokens          int64 `json:"output_tokens"`
	CachedInputTokens     int64 `json:"cached_input_tokens"`
	CacheReadInputTokens  int64 `json:"cache_read_input_tokens"`
	ReasoningOutputTokens int64 `json:"reasoning_output_tokens"`
}

type runtimeESessionTokenCount struct {
	Timestamp time.Time `json:"timestamp"`
	Type      string    `json:"type"`
	Payload   *struct {
		Type string `json:"type"`
		Info *struct {
			TotalTokenUsage *runtimeERawTokenUsage `json:"total_token_usage"`
			LastTokenUsage  *runtimeERawTokenUsage `json:"last_token_usage"`
			Model           string                 `json:"model"`
		} `json:"info"`
		Model string `json:"model"`
	} `json:"payload"`
}

func parseRuntimeESessionFile(path string) *runtimeESessionUsage {
	return parseRuntimeESessionFileSince(path, time.Time{}, false)
}

func parseRuntimeESessionFileSince(path string, startTime time.Time, resumed bool) *runtimeESessionUsage {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var result runtimeESessionUsage
	var previousTotal, accumulated, finalUsage runtimeERawTokenUsage
	previousTotalFound := false
	finalUsageFound := false
	afterStartBoundary := false

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()

		if !bytesContainsStr(line, "token_count") && !bytesContainsStr(line, "turn_context") {
			continue
		}

		var evt runtimeESessionTokenCount
		if err := json.Unmarshal(line, &evt); err != nil || evt.Payload == nil {
			continue
		}
		timestampAfterStart := !startTime.IsZero() && !evt.Timestamp.IsZero() && evt.Timestamp.After(startTime)
		if timestampAfterStart {
			afterStartBoundary = true
		}

		if evt.Type == "turn_context" && evt.Payload.Model != "" {
			result.model = evt.Payload.Model
			continue
		}

		if evt.Payload.Type == "token_count" && evt.Payload.Info != nil {
			afterStart := startTime.IsZero() || timestampAfterStart ||
				(evt.Timestamp.IsZero() && (!resumed || afterStartBoundary))
			if usage := evt.Payload.Info.TotalTokenUsage; usage != nil {
				current := normalizeRuntimeERawTokenUsage(*usage)
				if afterStart {
					delta := current
					if previousTotalFound {
						delta = subtractRuntimeERawTokenUsage(current, previousTotal)
					}
					accumulated = addRuntimeERawTokenUsage(accumulated, delta)
					finalUsage = accumulated
					finalUsageFound = true
				}
				previousTotal = current
				previousTotalFound = true
			} else if usage := evt.Payload.Info.LastTokenUsage; usage != nil && afterStart {

				finalUsage = normalizeRuntimeERawTokenUsage(*usage)
				finalUsageFound = true
			}
			if evt.Payload.Info.Model != "" {
				result.model = evt.Payload.Info.Model
			}
		}
	}

	if !finalUsageFound {
		return nil
	}
	cachedTokens := finalUsage.CachedInputTokens
	result.usage = TokenUsage{
		InputTokens:     runtimeEUncachedInputTokens(finalUsage.InputTokens, cachedTokens),
		OutputTokens:    finalUsage.OutputTokens + finalUsage.ReasoningOutputTokens,
		CacheReadTokens: cachedTokens,
	}
	return &result
}

func subtractRuntimeERawTokenUsage(total, baseline runtimeERawTokenUsage) runtimeERawTokenUsage {
	total = normalizeRuntimeERawTokenUsage(total)
	baseline = normalizeRuntimeERawTokenUsage(baseline)

	return runtimeERawTokenUsage{
		InputTokens:           nonNegativeTokenDelta(total.InputTokens, baseline.InputTokens),
		OutputTokens:          nonNegativeTokenDelta(total.OutputTokens, baseline.OutputTokens),
		CachedInputTokens:     nonNegativeTokenDelta(total.CachedInputTokens, baseline.CachedInputTokens),
		ReasoningOutputTokens: nonNegativeTokenDelta(total.ReasoningOutputTokens, baseline.ReasoningOutputTokens),
	}
}

func normalizeRuntimeERawTokenUsage(usage runtimeERawTokenUsage) runtimeERawTokenUsage {
	if usage.CachedInputTokens == 0 {
		usage.CachedInputTokens = usage.CacheReadInputTokens
	}
	usage.CacheReadInputTokens = 0
	return usage
}

func addRuntimeERawTokenUsage(a, b runtimeERawTokenUsage) runtimeERawTokenUsage {
	return runtimeERawTokenUsage{
		InputTokens:           a.InputTokens + b.InputTokens,
		OutputTokens:          a.OutputTokens + b.OutputTokens,
		CachedInputTokens:     a.CachedInputTokens + b.CachedInputTokens,
		ReasoningOutputTokens: a.ReasoningOutputTokens + b.ReasoningOutputTokens,
	}
}

func nonNegativeTokenDelta(total, baseline int64) int64 {
	if total < baseline {

		return total
	}
	return total - baseline
}

func bytesContainsStr(b []byte, s string) bool {
	return strings.Contains(string(b), s)
}

func extractThreadID(result json.RawMessage) string {
	var r struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(result, &r); err != nil {
		return ""
	}
	return r.Thread.ID
}

func extractNestedString(m map[string]any, keys ...string) string {
	current := any(m)
	for _, key := range keys {
		obj, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = obj[key]
	}
	s, _ := current.(string)
	return s
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
