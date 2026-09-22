package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mattn/go-shellwords"

	"github.com/adanman/goosar/server/internal/cli"
	"github.com/adanman/goosar/server/internal/deliveryprofile"
)

const (
	DefaultServerURL         = "ws://localhost:8080/ws"
	DefaultPollInterval      = 30 * time.Second
	DefaultHeartbeatInterval = 15 * time.Second

	DefaultAgentTimeout                      = 0
	DefaultRuntimeESemanticInactivityTimeout = 10 * time.Minute
	DefaultRuntimeEHandshakeTimeout          = 30 * time.Second

	DefaultRuntimeMIdleWatchdog = 10 * time.Minute

	DefaultAgentIdleWatchdog = 30 * time.Minute

	DefaultAgentToolWatchdog              = 2 * time.Hour
	DefaultRuntimeName                    = "Local Agent"
	DefaultWorkspaceBootstrapSyncInterval = 30 * time.Second
	DefaultWorkspaceLegacySyncInterval    = 5 * time.Minute
	DefaultWorkspaceSyncInterval          = 30 * time.Minute
	DefaultWorkspaceSyncMaxBackoff        = 30 * time.Minute
	DefaultHealthPort                     = 19514
	DefaultMaxConcurrentTasks             = 20
	DefaultGCInterval                     = 2 * time.Hour
	DefaultGCTTL                          = 24 * time.Hour
	DefaultGCOrphanTTL                    = 72 * time.Hour
	DefaultGCArtifactTTL                  = 12 * time.Hour
	DefaultGCRuntimeESessionTTL           = 14 * 24 * time.Hour
	DefaultAutoUpdateCheckInterval        = 6 * time.Hour
)

var DefaultGCArtifactPatterns = []string{"node_modules", ".next", ".turbo"}

type Config struct {
	ServerBaseURL   string
	DaemonID        string
	LegacyDaemonIDs []string
	DeviceName      string
	RuntimeName     string
	CLIVersion      string
	LaunchedBy      string
	Profile         string

	Agents                            map[string]AgentEntry
	WorkspacesRoot                    string
	KeepEnvAfterTask                  bool
	HealthPort                        int
	MaxConcurrentTasks                int
	GCEnabled                         bool
	GCInterval                        time.Duration
	GCTTL                             time.Duration
	GCOrphanTTL                       time.Duration
	GCArtifactTTL                     time.Duration
	GCArtifactPatterns                []string
	GCCodexSessionTTL                 time.Duration
	AutoUpdateEnabled                 bool
	AutoUpdateCheckInterval           time.Duration
	PollInterval                      time.Duration
	HeartbeatInterval                 time.Duration
	AgentTimeout                      time.Duration
	RuntimeESemanticInactivityTimeout time.Duration
	RuntimeEHandshakeTimeout          time.Duration
	OpenCodeIdleWatchdog              time.Duration
	AgentIdleWatchdog                 time.Duration
	AgentToolWatchdog                 time.Duration

	RuntimeCArgs []string
	RuntimeDArgs []string
	RuntimeEArgs []string
	RuntimeQArgs []string

	ProfileCommandOverrides map[string]string
}

type Overrides struct {
	ServerURL         string
	WorkspacesRoot    string
	PollInterval      time.Duration
	HeartbeatInterval time.Duration

	AgentTimeout                      *time.Duration
	RuntimeESemanticInactivityTimeout time.Duration
	RuntimeEHandshakeTimeout          time.Duration
	MaxConcurrentTasks                int
	DaemonID                          string
	DeviceName                        string
	RuntimeName                       string
	Profile                           string
	HealthPort                        int

	AllowNoAgents bool

	DisableAutoUpdate       bool
	AutoUpdateCheckInterval time.Duration
}

func LoadConfig(overrides Overrides) (Config, error) {

	rawServerURL := envOrDefault("GOOSAR_SERVER_URL", DefaultServerURL)
	if overrides.ServerURL != "" {
		rawServerURL = overrides.ServerURL
	}
	serverBaseURL, err := NormalizeServerBaseURL(rawServerURL)
	if err != nil {
		return Config{}, err
	}

	var profileCommandOverrides map[string]string
	if cliCfg, err := cli.LoadCLIConfigForProfile(overrides.Profile); err != nil {
		slog.Warn("could not load CLI config for backend overrides; proceeding without",
			"profile", overrides.Profile, "err", err)
	} else {
		if oc := openclawOverrideFrom(cliCfg); oc != nil {
			applyOpenclawOverride(oc)
		}

		if len(cliCfg.ProfileCommandOverrides) > 0 {
			profileCommandOverrides = make(map[string]string, len(cliCfg.ProfileCommandOverrides))
			for id, path := range cliCfg.ProfileCommandOverrides {
				if id == "" || strings.TrimSpace(path) == "" {
					continue
				}
				profileCommandOverrides[id] = path
			}
		}
	}

	var (
		shellResolveOnce sync.Once
		shellResolved    map[string]string
	)
	getShellResolved := func() map[string]string {
		shellResolveOnce.Do(func() {
			shellResolved = resolveAgentsViaLoginShell(defaultAgentCommandNames)
		})
		return shellResolved
	}

	probe := func(runtimeCode, envVar, defaultCmd, modelEnv string) (AgentEntry, bool) {
		cmd := envOrDefault(envVar, defaultCmd)
		if path, err := resolveAgentExecutablePath(cmd); err == nil {
			return AgentEntry{
				Path:    path,
				Command: cmd,
				Model:   strings.TrimSpace(os.Getenv(modelEnv)),
			}, true
		}

		if strings.ContainsAny(cmd, "/\\") {
			return AgentEntry{}, false
		}
		if path, ok := getShellResolved()[cmd]; ok {
			return AgentEntry{
				Path:    path,
				Command: cmd,
				Model:   strings.TrimSpace(os.Getenv(modelEnv)),
			}, true
		}
		if cmd == defaultCmd {

			for _, p := range runtimeBundledExecutablePaths(runtimeCode) {
				if _, err := os.Stat(p); err == nil {
					return AgentEntry{
						Path:    p,
						Command: cmd,
						Model:   strings.TrimSpace(os.Getenv(modelEnv)),
					}, true
				}
			}
		}
		return AgentEntry{}, false
	}

	agents := map[string]AgentEntry{}
	for _, code := range runtimeRegistry.Codes() {
		descriptor, ok := runtimeDescriptor(code)
		if !ok || descriptor.CLIName == "" {
			continue
		}
		prefix := runtimeEnvPrefix(code)
		if e, ok := probe(code, prefix+"_PATH", descriptor.CLIName, prefix+"_MODEL"); ok {
			agents[code] = e
		}
	}
	if len(agents) == 0 && !overrides.AllowNoAgents {
		return Config{}, fmt.Errorf(
			"no agent CLI found: install a CLI for one of the supported runtimes (%s) and ensure it is on PATH, "+
				"or pin its executable with the matching GOOSAR_RUNTIME_<LETTER>_PATH",
			strings.Join(runtimeRegistry.Codes(), ", "))
	}

	runtimeCArgs, err := shellArgsFromEnv(runtimeEnvPrefix("runtime-c") + "_ARGS")
	if err != nil {
		return Config{}, err
	}
	runtimeEArgs, err := shellArgsFromEnv(runtimeEnvPrefix("runtime-e") + "_ARGS")
	if err != nil {
		return Config{}, err
	}
	runtimeDArgs, err := shellArgsFromEnv(runtimeEnvPrefix("runtime-d") + "_ARGS")
	if err != nil {
		return Config{}, err
	}
	runtimeQArgs, err := shellArgsFromEnv(runtimeEnvPrefix("runtime-q") + "_ARGS")
	if err != nil {
		return Config{}, err
	}

	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		host = "local-machine"
	}

	pollInterval, err := durationFromEnv("GOOSAR_DAEMON_POLL_INTERVAL", DefaultPollInterval)
	if err != nil {
		return Config{}, err
	}
	if overrides.PollInterval > 0 {
		pollInterval = overrides.PollInterval
	}

	heartbeatInterval, err := durationFromEnv("GOOSAR_DAEMON_HEARTBEAT_INTERVAL", DefaultHeartbeatInterval)
	if err != nil {
		return Config{}, err
	}
	if overrides.HeartbeatInterval > 0 {
		heartbeatInterval = overrides.HeartbeatInterval
	}

	agentTimeout, err := durationFromEnv("GOOSAR_AGENT_TIMEOUT", DefaultAgentTimeout)
	if err != nil {
		return Config{}, err
	}
	if overrides.AgentTimeout != nil {
		agentTimeout = *overrides.AgentTimeout
	}

	runtimeESemanticInactivityTimeout, err := durationFromEnv("GOOSAR_RUNTIME_E_SEMANTIC_INACTIVITY_TIMEOUT", DefaultRuntimeESemanticInactivityTimeout)
	if err != nil {
		return Config{}, err
	}
	if overrides.RuntimeESemanticInactivityTimeout > 0 {
		runtimeESemanticInactivityTimeout = overrides.RuntimeESemanticInactivityTimeout
	}

	runtimeEHandshakeTimeout, err := durationFromEnv("GOOSAR_RUNTIME_E_HANDSHAKE_TIMEOUT", DefaultRuntimeEHandshakeTimeout)
	if err != nil {
		return Config{}, err
	}
	if runtimeEHandshakeTimeout <= 0 {
		runtimeEHandshakeTimeout = DefaultRuntimeEHandshakeTimeout
	}
	if overrides.RuntimeEHandshakeTimeout > 0 {
		runtimeEHandshakeTimeout = overrides.RuntimeEHandshakeTimeout
	}

	agentIdleWatchdog, err := durationFromEnv("GOOSAR_AGENT_IDLE_WATCHDOG", DefaultAgentIdleWatchdog)
	if err != nil {
		return Config{}, err
	}

	openCodeIdleWatchdog, err := durationFromEnv("GOOSAR_RUNTIME_M_IDLE_WATCHDOG", DefaultRuntimeMIdleWatchdog)
	if err != nil {
		return Config{}, err
	}

	agentToolWatchdog, err := durationFromEnv("GOOSAR_AGENT_TOOL_WATCHDOG", DefaultAgentToolWatchdog)
	if err != nil {
		return Config{}, err
	}

	maxConcurrentTasks, err := intFromEnv("GOOSAR_DAEMON_MAX_CONCURRENT_TASKS", DefaultMaxConcurrentTasks)
	if err != nil {
		return Config{}, err
	}
	if overrides.MaxConcurrentTasks > 0 {
		maxConcurrentTasks = overrides.MaxConcurrentTasks
	}

	profile := overrides.Profile

	daemonID := strings.TrimSpace(os.Getenv("GOOSAR_DAEMON_ID"))
	if overrides.DaemonID != "" {
		daemonID = overrides.DaemonID
	}
	if daemonID == "" {
		persisted, err := EnsureDaemonID(profile)
		if err != nil {
			return Config{}, fmt.Errorf("ensure daemon id: %w", err)
		}
		daemonID = persisted
	}

	legacyDaemonIDs := LegacyDaemonIDs(host, profile)

	if uuids, err := LegacyDaemonUUIDs(); err == nil {
		legacyDaemonIDs = append(legacyDaemonIDs, uuids...)
	}

	legacyDaemonIDs = filterLegacyIDs(legacyDaemonIDs, daemonID)

	deviceName := envOrDefault("GOOSAR_DAEMON_DEVICE_NAME", host)
	if overrides.DeviceName != "" {
		deviceName = overrides.DeviceName
	}

	runtimeName := envOrDefault("GOOSAR_AGENT_RUNTIME_NAME", DefaultRuntimeName)
	if overrides.RuntimeName != "" {
		runtimeName = overrides.RuntimeName
	}

	workspacesRoot, err := ResolveWorkspacesRoot(profile, overrides.WorkspacesRoot)
	if err != nil {
		return Config{}, err
	}

	healthPort := DefaultHealthPort
	if overrides.HealthPort > 0 {
		healthPort = overrides.HealthPort
	}

	keepEnv := os.Getenv("GOOSAR_KEEP_ENV_AFTER_TASK") == "true" || os.Getenv("GOOSAR_KEEP_ENV_AFTER_TASK") == "1"

	gcEnabled := true
	if v := os.Getenv("GOOSAR_GC_ENABLED"); v == "false" || v == "0" {
		gcEnabled = false
	}
	gcInterval, err := durationFromEnv("GOOSAR_GC_INTERVAL", DefaultGCInterval)
	if err != nil {
		return Config{}, err
	}
	gcTTL, err := durationFromEnv("GOOSAR_GC_TTL", DefaultGCTTL)
	if err != nil {
		return Config{}, err
	}
	gcOrphanTTL, err := durationFromEnv("GOOSAR_GC_ORPHAN_TTL", DefaultGCOrphanTTL)
	if err != nil {
		return Config{}, err
	}
	gcArtifactTTL, err := durationFromEnv("GOOSAR_GC_ARTIFACT_TTL", DefaultGCArtifactTTL)
	if err != nil {
		return Config{}, err
	}
	gcCodexSessionTTL, err := durationFromEnv("GOOSAR_GC_RUNTIME_E_SESSION_TTL", DefaultGCRuntimeESessionTTL)
	if err != nil {
		return Config{}, err
	}
	gcArtifactPatterns := patternsFromEnv("GOOSAR_GC_ARTIFACT_PATTERNS", DefaultGCArtifactPatterns)

	autoUpdateEnabled := isOfficialCloudServer(serverBaseURL)
	if v := strings.TrimSpace(os.Getenv("GOOSAR_DAEMON_AUTO_UPDATE")); v != "" {
		switch strings.ToLower(v) {
		case "false", "0", "no", "off":
			autoUpdateEnabled = false
		case "true", "1", "yes", "on":
			autoUpdateEnabled = true
		}
	}
	if overrides.DisableAutoUpdate {
		autoUpdateEnabled = false
	}

	if deliveryprofile.IsPerimeterAdvertised(os.Getenv(deliveryprofile.EnvVar)) {
		autoUpdateEnabled = false
	}
	autoUpdateInterval, err := durationFromEnv("GOOSAR_DAEMON_AUTO_UPDATE_INTERVAL", DefaultAutoUpdateCheckInterval)
	if err != nil {
		return Config{}, err
	}
	if overrides.AutoUpdateCheckInterval > 0 {
		autoUpdateInterval = overrides.AutoUpdateCheckInterval
	}

	return Config{
		ServerBaseURL:                     serverBaseURL,
		DaemonID:                          daemonID,
		LegacyDaemonIDs:                   legacyDaemonIDs,
		DeviceName:                        deviceName,
		RuntimeName:                       runtimeName,
		Profile:                           profile,
		Agents:                            agents,
		WorkspacesRoot:                    workspacesRoot,
		KeepEnvAfterTask:                  keepEnv,
		GCEnabled:                         gcEnabled,
		GCInterval:                        gcInterval,
		GCTTL:                             gcTTL,
		GCOrphanTTL:                       gcOrphanTTL,
		GCArtifactTTL:                     gcArtifactTTL,
		GCArtifactPatterns:                gcArtifactPatterns,
		GCCodexSessionTTL:                 gcCodexSessionTTL,
		AutoUpdateEnabled:                 autoUpdateEnabled,
		AutoUpdateCheckInterval:           autoUpdateInterval,
		HealthPort:                        healthPort,
		MaxConcurrentTasks:                maxConcurrentTasks,
		PollInterval:                      pollInterval,
		HeartbeatInterval:                 heartbeatInterval,
		AgentTimeout:                      agentTimeout,
		RuntimeESemanticInactivityTimeout: runtimeESemanticInactivityTimeout,
		RuntimeEHandshakeTimeout:          runtimeEHandshakeTimeout,
		OpenCodeIdleWatchdog:              openCodeIdleWatchdog,
		AgentIdleWatchdog:                 agentIdleWatchdog,
		AgentToolWatchdog:                 agentToolWatchdog,
		RuntimeCArgs:                      runtimeCArgs,
		RuntimeDArgs:                      runtimeDArgs,
		RuntimeEArgs:                      runtimeEArgs,
		RuntimeQArgs:                      runtimeQArgs,
		ProfileCommandOverrides:           profileCommandOverrides,
	}, nil
}

var officialCloudHost = os.Getenv("GOOSAR_OFFICIAL_CLOUD_HOST")

func isOfficialCloudServer(baseURL string) bool {
	if officialCloudHost == "" {
		return false
	}
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Hostname(), officialCloudHost)
}

func NormalizeServerBaseURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("invalid GOOSAR_SERVER_URL: %w", err)
	}
	switch u.Scheme {
	case "ws":
		u.Scheme = "http"
	case "wss":
		u.Scheme = "https"
	case "http", "https":
	default:
		return "", fmt.Errorf("GOOSAR_SERVER_URL must use ws, wss, http, or https")
	}
	if u.Path == "/ws" {
		u.Path = ""
	}
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/"), nil
}

func ResolveWorkspacesRoot(profile, override string) (string, error) {
	root := strings.TrimSpace(os.Getenv("GOOSAR_WORKSPACES_ROOT"))
	if override != "" {
		root = override
	}
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w (set GOOSAR_WORKSPACES_ROOT to override)", err)
		}
		if profile != "" {
			root = filepath.Join(home, "goosar_workspaces_"+profile)
		} else {
			root = filepath.Join(home, "goosar_workspaces")
		}
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve absolute workspaces root: %w", err)
	}
	return abs, nil
}

func ArtifactPatternsFromEnv() []string {
	return patternsFromEnv("GOOSAR_GC_ARTIFACT_PATTERNS", DefaultGCArtifactPatterns)
}

func patternsFromEnv(name string, defaults []string) []string {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		out := make([]string, len(defaults))
		copy(out, defaults)
		return out
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || strings.ContainsAny(p, "/\\") {
			continue
		}
		out = append(out, p)
	}
	return out
}

func shellArgsFromEnv(name string) ([]string, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return nil, nil
	}
	args, err := shellwords.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid %s: %w", name, err)
	}
	return args, nil
}

func resolveAgentExecutablePath(cmd string) (string, error) {
	resolved, err := exec.LookPath(cmd)
	if err != nil {
		return "", err
	}
	if strings.ContainsAny(cmd, "/\\") {
		return resolved, nil
	}
	if isInGoosarHooksDir(resolved) {
		if unshadowed, err := lookPathExcludingGoosarHooks(cmd); err == nil {
			return unshadowed, nil
		}
	}
	return canonicalExecutablePath(resolved), nil
}

func agentExecutablePresent(path string) bool {
	if path == "" {
		return false
	}
	_, err := exec.LookPath(path)
	return err == nil
}

func reresolveAgentCommand(cmd string) (string, bool) {
	if cmd == "" {
		return "", false
	}
	if path, err := resolveAgentExecutablePath(cmd); err == nil {
		return path, true
	}

	if !strings.ContainsAny(cmd, "/\\") {
		if path, ok := resolveAgentsViaLoginShell([]string{cmd})[cmd]; ok {
			return path, true
		}
	}
	return "", false
}

func lookPathExcludingGoosarHooks(cmd string) (string, error) {
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			dir = "."
		}
		if isGoosarHooksDir(dir) {
			continue
		}
		candidate := filepath.Join(dir, cmd)
		if isExecutableFile(candidate) {
			return canonicalExecutablePath(candidate), nil
		}
	}
	return "", exec.ErrNotFound
}

func isInGoosarHooksDir(path string) bool {
	if path == "" {
		return false
	}
	return isGoosarHooksDir(filepath.Dir(path))
}

func isGoosarHooksDir(dir string) bool {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return false
	}
	return samePathDir(dir, filepath.Join(home, ".goosar", "hooks"))
}

func samePathDir(a, b string) bool {
	absA, err := filepath.Abs(a)
	if err != nil {
		return false
	}
	absB, err := filepath.Abs(b)
	if err != nil {
		return false
	}
	absA = filepath.Clean(absA)
	absB = filepath.Clean(absB)
	if realA, err := filepath.EvalSymlinks(absA); err == nil {
		absA = realA
	}
	if realB, err := filepath.EvalSymlinks(absB); err == nil {
		absB = realB
	}
	return absA == absB
}

func canonicalExecutablePath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real
	}
	return abs
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode()&0o111 != 0
}

var defaultAgentCommandNames = buildDefaultAgentCommandNames()

func buildDefaultAgentCommandNames() []string {
	codes := runtimeRegistry.Codes()
	names := make([]string, 0, len(codes))
	seen := make(map[string]bool, len(codes))
	for _, code := range codes {
		d, ok := runtimeRegistry.ByCode(code)
		if !ok || d.CLIName == "" || seen[d.CLIName] {
			continue
		}
		seen[d.CLIName] = true
		names = append(names, d.CLIName)
	}
	return names
}

var runtimeBundledExecutablePaths = func(runtimeCode string) []string {
	d, ok := runtimeDescriptor(runtimeCode)
	if !ok || len(d.BundledExecutablePaths) == 0 {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	resolved := d.ResolvedBundledExecutablePaths(home)
	if home == "" {

		absolute := make([]string, 0, len(resolved))
		for _, p := range resolved {
			if filepath.IsAbs(p) {
				absolute = append(absolute, p)
			}
		}
		return absolute
	}
	return resolved
}

const loginShellResolveTimeout = 3 * time.Second

const loginShellResolveWaitDelay = 2 * time.Second

var supportedLoginShells = map[string]struct{}{
	"bash": {},
	"zsh":  {},
	"sh":   {},
	"dash": {},
	"ksh":  {},
}

func resolveAgentsViaLoginShell(names []string) map[string]string {
	out := map[string]string{}
	if len(names) == 0 {
		return out
	}
	shell := strings.TrimSpace(os.Getenv("SHELL"))
	if shell == "" {
		return out
	}
	if _, ok := supportedLoginShells[filepath.Base(shell)]; !ok {
		return out
	}

	safe := make([]string, 0, len(names))
	for _, n := range names {
		if isSafeAgentName(n) {
			safe = append(safe, n)
		}
	}
	if len(safe) == 0 {
		return out
	}

	ctx, cancel := context.WithTimeout(context.Background(), loginShellResolveTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, shell, "-ilc", buildLoginShellResolveScript(safe))
	cmd.WaitDelay = loginShellResolveWaitDelay
	raw, err := cmd.Output()
	if err != nil {
		return out
	}

	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		name, path := parts[0], strings.TrimSpace(parts[1])
		if !filepath.IsAbs(path) {
			continue
		}

		if _, err := exec.LookPath(path); err != nil {
			continue
		}
		out[name] = path
	}
	return out
}

func buildLoginShellResolveScript(names []string) string {
	var b strings.Builder
	b.WriteString("for n in")
	for _, n := range names {
		b.WriteByte(' ')
		b.WriteString(n)
	}
	b.WriteString("; do\n")
	b.WriteString("  unalias \"$n\" 2>/dev/null\n")
	b.WriteString("  unset -f \"$n\" 2>/dev/null\n")
	b.WriteString("  p=$(command -v \"$n\" 2>/dev/null) || continue\n")
	b.WriteString("  [ -n \"$p\" ] || continue\n")
	b.WriteString("  case \"$p\" in /*) ;; *) continue ;; esac\n")
	b.WriteString("  d=$(dirname \"$p\") && f=$(basename \"$p\") && c=$(cd \"$d\" 2>/dev/null && pwd -P) || continue\n")
	b.WriteString("  hc=\"\"\n")
	b.WriteString("  if [ -n \"${HOME:-}\" ]; then hd=\"$HOME/.goosar/hooks\"; hc=$(cd \"$hd\" 2>/dev/null && pwd -P) || hc=\"\"; fi\n")
	b.WriteString("  if [ -n \"$hc\" ] && [ \"$c\" = \"$hc\" ]; then\n")
	b.WriteString("    oldIFS=$IFS; IFS=:\n")
	b.WriteString("    for d2 in $PATH; do\n")
	b.WriteString("      [ -n \"$d2\" ] || d2=.\n")
	b.WriteString("      c2=$(cd \"$d2\" 2>/dev/null && pwd -P) || continue\n")
	b.WriteString("      [ \"$c2\" = \"$hc\" ] && continue\n")
	b.WriteString("      if [ -f \"$c2/$n\" ] && [ -x \"$c2/$n\" ]; then c=\"$c2\"; f=\"$n\"; break; fi\n")
	b.WriteString("    done\n")
	b.WriteString("    IFS=$oldIFS\n")
	b.WriteString("  fi\n")
	b.WriteString("  printf '%s\\t%s\\n' \"$n\" \"$c/$f\"\n")
	b.WriteString("done\n")
	return b.String()
}

func isSafeAgentName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.':
		default:
			return false
		}
	}
	return true
}

func openclawOverrideFrom(cfg cli.CLIConfig) *cli.OpenClawOverride {
	if cfg.Backends == nil {
		return nil
	}
	return cfg.Backends.OpenClaw
}

func applyOpenclawOverride(oc *cli.OpenClawOverride) {
	if oc == nil {
		return
	}
	if oc.BinaryPath != "" {
		pathEnv := runtimeEnvPrefix("runtime-n") + "_PATH"
		if _, set := os.LookupEnv(pathEnv); !set {
			_ = os.Setenv(pathEnv, oc.BinaryPath)
		}
	}
	if oc.StateDir != "" {
		if _, set := os.LookupEnv("OPENCLAW_STATE_DIR"); !set {
			_ = os.Setenv("OPENCLAW_STATE_DIR", oc.StateDir)
		}
	}
}
