package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"

	"github.com/adanman/goosar/server/internal/cli"
	"github.com/adanman/goosar/server/internal/daemon/execenv"
	"github.com/adanman/goosar/server/internal/daemon/repocache"
	"github.com/adanman/goosar/server/internal/perimeterpolicy"
	"github.com/adanman/goosar/server/internal/selfexec"
	"github.com/adanman/goosar/server/pkg/agent"
	"github.com/adanman/goosar/server/pkg/skillbundle"
	"github.com/adanman/goosar/server/pkg/taskfailure"
)

var ErrRepoNotConfigured = errors.New("repo is not configured for this workspace")

var ErrNoRuntimesToRegister = errors.New("no agent runtimes could be registered")

var errTaskPrepareTimeout = errors.New("task preparation timed out")

var errSkillBundleUnavailable = errors.New("skill bundle unavailable")

const (
	taskSlotWaitTimeout      = 2 * time.Second
	taskSlotCapacityBackoff  = 5 * time.Second
	repoCheckoutModeEnv      = "GOOSAR_REPO_CHECKOUT_MODE"
	repoCheckoutModeIsolated = "isolated"

	defaultTaskPrepareTimeout = 5 * time.Minute
)

func repoCheckoutModeFor(provider, goos string) string {
	if provider == runtimeCodeE && goos == "linux" {
		return repoCheckoutModeIsolated
	}
	return ""
}

var (
	taskPrepareLeaseRefresh = 15 * time.Second
	taskPrepareLeaseTimeout = 10 * time.Second
)

func taskScopedAuthToken(task Task) (string, error) {
	token := strings.TrimSpace(task.AuthToken)
	if token == "" {
		return "", errors.New("server did not provide task-scoped auth token")
	}
	if !strings.HasPrefix(token, "mat_") {
		return "", errors.New("server provided non-task-scoped auth token")
	}
	return token, nil
}

type taskRunner interface {
	run(ctx context.Context, task Task, provider string, slot int, log *slog.Logger) (TaskResult, error)
}

type taskRunnerFunc func(context.Context, Task, string, int, *slog.Logger) (TaskResult, error)

func (f taskRunnerFunc) run(ctx context.Context, task Task, provider string, slot int, log *slog.Logger) (TaskResult, error) {
	return f(ctx, task, provider, slot, log)
}

type terminalTaskReportKind uint8

const (
	terminalTaskReportComplete terminalTaskReportKind = iota + 1
	terminalTaskReportFail

	terminalTaskReportTimeout = 6 * time.Minute
)

type terminalTaskReport struct {
	kind          terminalTaskReportKind
	taskID        string
	output        string
	branchName    string
	errorMessage  string
	sessionID     string
	workDir       string
	failureReason string

	sessionRolloutMissing bool
}

type executionEnvironmentCommand func() ([]string, error)

func defaultExecutionEnvironmentCommand() ([]string, error) {
	executable, err := resolveSelfExecutable()
	if err != nil {
		return nil, fmt.Errorf("resolve execution-environment helper: %w", err)
	}
	return []string{executable, execenv.PreparationHelperArg}, nil
}

var (
	isBrewInstall         = cli.IsBrewInstall
	getBrewPrefix         = cli.GetBrewPrefix
	matchKnownBrewPrefix  = cli.MatchKnownBrewPrefix
	resolveSelfExecutable = selfexec.Resolve

	detectAgentVersion   = agent.DetectVersion
	checkAgentMinVersion = agent.CheckMinVersion

	lookPath = exec.LookPath

	profilePathExecutable = func(path string) bool {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			return false
		}
		return info.Mode().Perm()&0o111 != 0
	}
)

type workspaceState struct {
	workspaceID     string
	runtimeIDs      []string
	reposVersion    string
	allowedRepoURLs map[string]struct{}
	taskRepoURLs    map[string]struct{}
	taskRepoRefs    map[string]map[string]string
	settings        json.RawMessage
	lastRepoSyncErr string
	repoRefreshMu   sync.Mutex

	profileSetSig string
}

type repoCacheBackend interface {
	Lookup(workspaceID, url string) string
	Sync(workspaceID string, repos []repocache.RepoInfo) error
	WithRepoLock(barePath string, fn func() error) error
	CreateWorktree(params repocache.WorktreeParams) (*repocache.WorktreeResult, error)
}

type Daemon struct {
	cfg        Config
	client     *Client
	repoCache  repoCacheBackend
	skillCache *SkillBundleCache
	logger     *slog.Logger

	repoCheckoutTasksMu sync.RWMutex
	repoCheckoutTasks   map[string]activeRepoCheckoutTask

	mu           sync.Mutex
	workspaces   map[string]*workspaceState
	runtimeIndex map[string]Runtime

	profileLaunchSpecs map[string]profileLaunchSpec
	reloading          sync.Mutex
	runtimeSet         *runtimeSetWatcher

	versionsMu    sync.RWMutex
	agentVersions map[string]string

	resolvedPathsMu sync.RWMutex
	resolvedPaths   map[string]healedAgent

	healGroup singleflight.Group

	wsHBMu      sync.RWMutex
	wsHBLastAck map[string]time.Time

	mcpPolicy atomic.Pointer[perimeterpolicy.MCPPolicy]

	reconcile *reconcileBroadcaster

	workspaceChanges *workspaceChangeSignal

	wsRPC *wsRPCClient

	batchClaimUnsupported atomic.Bool

	wsClaimHTTPFallbackAfter atomic.Int64

	runtimeGoneMu       sync.Mutex
	runtimeGoneInflight map[string]struct{}

	accessRevokedMu       sync.Mutex
	accessRevokedDisarmed map[string]bool

	accessRevokedStrikes map[string]accessDenialStrike

	accessDeniedMinWindow     time.Duration
	reregisterNextAttempt     map[string]time.Time
	reregisterLastCompletedAt map[string]time.Time

	cancelFunc    context.CancelFunc
	rootCtx       context.Context
	restartBinary string
	updating      atomic.Bool
	activeTasks   atomic.Int64

	forceShutdown atomic.Bool

	draining atomic.Bool

	lastTooOldHint atomic.Int64
	ready          atomic.Bool

	claimMu        sync.Mutex
	pauseClaims    bool
	claimsInFlight int

	activeEnvRootsMu   sync.Mutex
	activeEnvRootsCond *sync.Cond
	activeEnvRoots     map[string]int
	deletingEnvRoots   map[string]bool

	activeCodexStoresMu   sync.Mutex
	activeCodexStoresCond *sync.Cond
	activeCodexStores     map[string]int
	deletingCodexStores   map[string]bool

	localPathLocks *LocalPathLocker

	bgSyncs sync.WaitGroup

	runner             taskRunner
	cancelPollInterval time.Duration

	executionEnvironmentCommand executionEnvironmentCommand

	taskPrepareTimeout time.Duration

	runUpdateFn func(targetVersion string) (string, error)
}

type profileLaunchSpec struct {
	path      string
	version   string
	fixedArgs []string
}

func New(cfg Config, logger *slog.Logger) *Daemon {
	cacheRoot := filepath.Join(cfg.WorkspacesRoot, ".repos")
	skillCacheRoot := filepath.Join(cfg.WorkspacesRoot, ".skill-cache", "v1")
	client := NewClient(cfg.ServerBaseURL)

	client.SetVersion(cfg.CLIVersion)
	d := &Daemon{
		cfg:                       cfg,
		client:                    client,
		repoCache:                 repocache.New(cacheRoot, logger),
		skillCache:                NewSkillBundleCache(skillCacheRoot),
		logger:                    logger,
		workspaces:                make(map[string]*workspaceState),
		runtimeIndex:              make(map[string]Runtime),
		profileLaunchSpecs:        make(map[string]profileLaunchSpec),
		runtimeSet:                newRuntimeSetWatcher(),
		agentVersions:             make(map[string]string),
		resolvedPaths:             make(map[string]healedAgent),
		wsHBLastAck:               make(map[string]time.Time),
		activeEnvRoots:            make(map[string]int),
		deletingEnvRoots:          make(map[string]bool),
		activeCodexStores:         make(map[string]int),
		deletingCodexStores:       make(map[string]bool),
		localPathLocks:            NewLocalPathLocker(),
		runtimeGoneInflight:       make(map[string]struct{}),
		reregisterNextAttempt:     make(map[string]time.Time),
		reregisterLastCompletedAt: make(map[string]time.Time),
		cancelPollInterval:        5 * time.Second,
		accessDeniedMinWindow:     accessDeniedDefaultMinWindow,
		taskPrepareTimeout:        defaultTaskPrepareTimeout,
		reconcile:                 newReconcileBroadcaster(),
		workspaceChanges:          newWorkspaceChangeSignal(),
		wsRPC:                     newWSRPCClient(wsRPCResponseGrace),
	}
	d.activeEnvRootsCond = sync.NewCond(&d.activeEnvRootsMu)
	d.activeCodexStoresCond = sync.NewCond(&d.activeCodexStoresMu)
	d.executionEnvironmentCommand = defaultExecutionEnvironmentCommand
	d.runner = taskRunnerFunc(d.runTask)
	d.runUpdateFn = d.runUpdate
	return d
}

func (d *Daemon) setAgentVersion(provider, version string) {
	d.versionsMu.Lock()
	defer d.versionsMu.Unlock()
	d.agentVersions[provider] = version
}

func (d *Daemon) agentVersion(provider string) string {
	d.versionsMu.RLock()
	defer d.versionsMu.RUnlock()
	return d.agentVersions[provider]
}

type healedAgent struct {
	path    string
	version string
}

func (d *Daemon) resolveAgentEntry(ctx context.Context, provider string, entry AgentEntry) (AgentEntry, string) {

	d.resolvedPathsMu.RLock()
	healed, ok := d.resolvedPaths[provider]
	d.resolvedPathsMu.RUnlock()
	if ok && agentExecutablePresent(healed.path) {
		entry.Path = healed.path
		return entry, healed.version
	}

	if agentExecutablePresent(entry.Path) {
		return entry, d.agentVersion(provider)
	}

	if entry.Command == "" {
		return entry, d.agentVersion(provider)
	}

	command := entry.Command
	v, _, _ := d.healGroup.Do(provider, func() (any, error) {
		return d.healAgentPath(ctx, provider, command), nil
	})
	healed, _ = v.(healedAgent)
	if healed.path == "" {
		return entry, d.agentVersion(provider)
	}
	entry.Path = healed.path
	return entry, healed.version
}

func (d *Daemon) healAgentPath(ctx context.Context, provider, command string) healedAgent {

	d.resolvedPathsMu.RLock()
	cached, ok := d.resolvedPaths[provider]
	d.resolvedPathsMu.RUnlock()
	if ok && agentExecutablePresent(cached.path) {
		return cached
	}

	newPath, found := reresolveAgentCommand(command)
	if !found {
		return healedAgent{}
	}

	version, err := detectAgentVersion(ctx, newPath)
	if err != nil {
		d.logger.Warn("re-resolved agent executable failed version detection; keeping pinned path",
			"provider", provider, "command", command, "new_path", newPath, "error", err)
		return healedAgent{}
	}
	if err := checkAgentMinVersion(provider, version); err != nil {
		d.logger.Warn("re-resolved agent executable is below the minimum supported version; not adopting it",
			"provider", provider, "command", command, "new_path", newPath, "version", version, "error", err)
		return healedAgent{}
	}

	adopted := healedAgent{path: newPath, version: version}

	d.resolvedPathsMu.Lock()
	if d.resolvedPaths == nil {
		d.resolvedPaths = make(map[string]healedAgent)
	}
	d.resolvedPaths[provider] = adopted
	d.resolvedPathsMu.Unlock()

	d.setAgentVersion(provider, version)

	d.logger.Info("re-resolved agent executable after pinned path vanished (in-place upgrade)",
		"provider", provider, "command", command, "new_path", newPath, "version", version)
	return adopted
}

func (d *Daemon) notifyRuntimeSetChanged() {
	d.runtimeSet.notify()
}

const reregisterCoalesceWindow = 30 * time.Second

const reregisterFailureBackoff = 60 * time.Second

func (d *Daemon) handleRuntimeGone(runtimeID string) {
	if runtimeID == "" {
		return
	}

	entryAt := time.Now()

	d.runtimeGoneMu.Lock()
	if _, inflight := d.runtimeGoneInflight[runtimeID]; inflight {
		d.runtimeGoneMu.Unlock()
		return
	}
	d.runtimeGoneInflight[runtimeID] = struct{}{}
	d.runtimeGoneMu.Unlock()
	defer func() {
		d.runtimeGoneMu.Lock()
		delete(d.runtimeGoneInflight, runtimeID)
		d.runtimeGoneMu.Unlock()
	}()

	workspaceID, removed := d.removeStaleRuntime(runtimeID)
	if !removed {

		return
	}

	d.logger.Info("runtime deleted server-side; pruned from local state",
		"runtime_id", runtimeID, "workspace_id", workspaceID)
	d.notifyRuntimeSetChanged()

	if !d.tryClaimRegisterSlot(workspaceID, entryAt, time.Now()) {
		d.logger.Debug("skip re-register: coalescing with recent attempt",
			"workspace_id", workspaceID)
		return
	}

	err := d.reregisterWorkspaceAfterRuntimeGone(d.recoveryContext(), workspaceID)
	d.recordRegisterCompletion(workspaceID, time.Now(), err)
	if err != nil {

		d.logger.Warn("re-register after runtime gone failed",
			"workspace_id", workspaceID, "error", err)
	}
}

func (d *Daemon) tryClaimRegisterSlot(workspaceID string, entryAt, now time.Time) bool {
	d.runtimeGoneMu.Lock()
	defer d.runtimeGoneMu.Unlock()
	if next, ok := d.reregisterNextAttempt[workspaceID]; ok && now.Before(next) {
		return false
	}
	if last, ok := d.reregisterLastCompletedAt[workspaceID]; ok && !last.Before(entryAt) {
		return false
	}
	d.reregisterNextAttempt[workspaceID] = now.Add(reregisterCoalesceWindow)
	return true
}

func (d *Daemon) recordRegisterCompletion(workspaceID string, completedAt time.Time, err error) {
	d.runtimeGoneMu.Lock()
	defer d.runtimeGoneMu.Unlock()
	if err != nil {
		d.reregisterNextAttempt[workspaceID] = completedAt.Add(reregisterFailureBackoff)
		return
	}
	d.reregisterLastCompletedAt[workspaceID] = completedAt
	delete(d.reregisterNextAttempt, workspaceID)
}

func (d *Daemon) recoveryContext() context.Context {
	if d.rootCtx != nil {
		return d.rootCtx
	}
	return context.Background()
}

func (d *Daemon) removeStaleRuntime(runtimeID string) (string, bool) {
	d.mu.Lock()
	var workspaceID string
	for wsID, ws := range d.workspaces {
		found := false
		filtered := ws.runtimeIDs[:0:0]
		for _, rid := range ws.runtimeIDs {
			if rid == runtimeID {
				found = true
				continue
			}
			filtered = append(filtered, rid)
		}
		if found {
			ws.runtimeIDs = filtered
			workspaceID = wsID
			break
		}
	}
	if workspaceID == "" {
		d.mu.Unlock()
		return "", false
	}
	delete(d.runtimeIndex, runtimeID)
	d.mu.Unlock()

	d.wsHBMu.Lock()
	delete(d.wsHBLastAck, runtimeID)
	d.wsHBMu.Unlock()

	return workspaceID, true
}

func (d *Daemon) workspaceNeedsRuntimeRecovery(workspaceID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	ws, ok := d.workspaces[workspaceID]
	if !ok {
		return false
	}
	return len(ws.runtimeIDs) == 0
}

func (d *Daemon) applyRegisterResponseInPlace(workspaceID string, resp *RegisterResponse, profileSig string) (newIDs, droppedIDs []string, ok bool) {
	newIDs = make([]string, 0, len(resp.Runtimes))
	newIDSet := make(map[string]struct{}, len(resp.Runtimes))
	for _, rt := range resp.Runtimes {
		newIDs = append(newIDs, rt.ID)
		newIDSet[rt.ID] = struct{}{}
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	ws, exists := d.workspaces[workspaceID]
	if !exists {
		return nil, nil, false
	}

	for _, oldID := range ws.runtimeIDs {
		if _, kept := newIDSet[oldID]; !kept {
			delete(d.runtimeIndex, oldID)
			droppedIDs = append(droppedIDs, oldID)
		}
	}
	for _, rt := range resp.Runtimes {
		d.runtimeIndex[rt.ID] = rt
	}

	ws.runtimeIDs = newIDs
	if resp.ReposVersion != "" {
		ws.reposVersion = resp.ReposVersion
		ws.allowedRepoURLs = repoAllowlist(resp.Repos)
	}
	if len(resp.Settings) > 0 {
		ws.settings = resp.Settings
	}

	if profileSig != "" {
		ws.profileSetSig = profileSig
	}
	return newIDs, droppedIDs, true
}

func (d *Daemon) reregisterWorkspaceAfterRuntimeGone(ctx context.Context, workspaceID string) error {
	resp, profileSig, err := d.registerRuntimesForWorkspace(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("register runtimes: %w", err)
	}

	newIDs, _, ok := d.applyRegisterResponseInPlace(workspaceID, resp, profileSig)
	if !ok {
		return fmt.Errorf("workspace %s no longer tracked", workspaceID)
	}

	for _, rid := range newIDs {
		d.logger.Info("re-registered runtime after server-side deletion",
			"workspace_id", workspaceID, "runtime_id", rid)
	}
	d.notifyRuntimeSetChanged()

	for _, rid := range newIDs {
		if err := d.client.RecoverOrphans(ctx, rid); err != nil {
			d.logger.Warn("recover-orphans after re-register failed",
				"runtime_id", rid, "error", err)
		}
	}
	return nil
}

type runtimeSetWatcher struct {
	mu          sync.Mutex
	subscribers map[chan struct{}]struct{}
}

func newRuntimeSetWatcher() *runtimeSetWatcher {
	return &runtimeSetWatcher{subscribers: make(map[chan struct{}]struct{})}
}

func (w *runtimeSetWatcher) Subscribe() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	w.mu.Lock()
	w.subscribers[ch] = struct{}{}
	w.mu.Unlock()
	return ch, func() {
		w.mu.Lock()
		delete(w.subscribers, ch)
		w.mu.Unlock()
	}
}

func (w *runtimeSetWatcher) notify() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for ch := range w.subscribers {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (d *Daemon) wsHeartbeatFreshness() time.Duration {
	if d.cfg.HeartbeatInterval <= 0 {
		return 30 * time.Second
	}
	return 2 * d.cfg.HeartbeatInterval
}

func (d *Daemon) recordWSHeartbeatAck(runtimeID string) {
	if runtimeID == "" {
		return
	}
	d.wsHBMu.Lock()
	d.wsHBLastAck[runtimeID] = time.Now()
	d.wsHBMu.Unlock()
}

func (d *Daemon) wsHeartbeatRecentlyAcked(runtimeID string) bool {
	d.wsHBMu.RLock()
	last, ok := d.wsHBLastAck[runtimeID]
	d.wsHBMu.RUnlock()
	if !ok {
		return false
	}
	return time.Since(last) < d.wsHeartbeatFreshness()
}

func (d *Daemon) clearWSHeartbeatAcks() {
	d.wsHBMu.Lock()
	for k := range d.wsHBLastAck {
		delete(d.wsHBLastAck, k)
	}
	d.wsHBMu.Unlock()
}

func (d *Daemon) Run(ctx context.Context) error {

	ctx, cancel := context.WithCancel(ctx)
	d.cancelFunc = cancel
	d.rootCtx = ctx

	healthLn, err := d.listenHealth()
	if err != nil {
		return err
	}

	agentNames := make([]string, 0, len(d.cfg.Agents))
	for name := range d.cfg.Agents {
		agentNames = append(agentNames, name)
	}
	logFields := []any{"version", d.cfg.CLIVersion, "agents", agentNames, "server", d.cfg.ServerBaseURL}
	if d.cfg.Profile != "" {
		logFields = append(logFields, "profile", d.cfg.Profile)
	}
	d.logger.Info("starting daemon", logFields...)
	d.logger.Debug("daemon config resolved",
		"daemon_id", d.cfg.DaemonID,
		"device_name", d.cfg.DeviceName,
		"workspaces_root", d.cfg.WorkspacesRoot,
		"health_port", d.cfg.HealthPort,
		"poll_interval", d.cfg.PollInterval,
		"heartbeat_interval", d.cfg.HeartbeatInterval,
		"agent_timeout", d.cfg.AgentTimeout,
		"idle_watchdog", d.cfg.AgentIdleWatchdog,
		"opencode_idle_watchdog", d.cfg.OpenCodeIdleWatchdog,
		"max_concurrent_tasks", d.cfg.MaxConcurrentTasks,
		"gc_enabled", d.cfg.GCEnabled,
		"auto_update", d.cfg.AutoUpdateEnabled,
		"launched_by", d.cfg.LaunchedBy,
	)

	if err := execenv.EnsureWorkspacesRootMarker(d.cfg.WorkspacesRoot); err != nil {
		d.logger.Warn("workspaces root marker not written; CLI fail-closed guard limited to task workdirs", "error", err)
	}

	if err := d.resolveAuth(); err != nil {
		return err
	}

	healthCtx, healthCancel := context.WithCancel(context.WithoutCancel(ctx))
	defer healthCancel()
	go d.serveHealth(healthCtx, healthLn, time.Now())

	go func() {
		<-ctx.Done()
		d.draining.Store(true)
	}()

	if err := d.preflightAuth(ctx); err != nil {
		return err
	}

	defer d.deregisterRuntimes()

	go d.workspaceSyncLoop(ctx)

	taskWakeups := make(chan taskWakeup, 256)
	go d.taskWakeupLoop(ctx, taskWakeups)

	drainCtx, drainCancel := context.WithCancel(context.WithoutCancel(ctx))
	defer drainCancel()
	go d.heartbeatLoop(drainCtx)
	go d.gcLoop(ctx)
	go d.autoUpdateLoop(ctx)
	go d.tokenRenewalLoop(ctx)

	d.ready.Store(true)
	d.logger.Debug("background loops launched (workspace-sync, task-wakeup, heartbeat, gc, auto-update, token-renewal); health now reporting ready")
	err = d.pollLoop(ctx, taskWakeups)
	d.logger.Debug("daemon main loop returning", "error", err)
	return err
}

func (d *Daemon) RestartBinary() string {
	return d.restartBinary
}

func (d *Daemon) deregisterRuntimes() {
	runtimeIDs := d.allRuntimeIDs()
	if len(runtimeIDs) == 0 {
		d.logger.Debug("deregister: no runtimes to deregister")
		return
	}

	d.logger.Debug("deregistering runtimes on shutdown", "count", len(runtimeIDs), "runtime_ids", runtimeIDs)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := d.client.Deregister(ctx, runtimeIDs); err != nil {
		d.logger.Warn("failed to deregister runtimes on shutdown", "error", err)
	} else {
		d.logger.Info("deregistered runtimes", "count", len(runtimeIDs))
	}
}

func (d *Daemon) resolveAuth() error {
	cfg, err := cli.LoadCLIConfigForProfile(d.cfg.Profile)
	if err != nil {
		return fmt.Errorf("load CLI config: %w", err)
	}
	if cfg.Token == "" {
		loginHint := "'goosar login'"
		if d.cfg.Profile != "" {
			loginHint = fmt.Sprintf("'goosar login --profile %s'", d.cfg.Profile)
		}
		d.logger.Warn("not authenticated — run " + loginHint + " to authenticate, then restart the daemon")
		return fmt.Errorf("not authenticated: run %s first", loginHint)
	}
	d.client.SetToken(cfg.Token)
	d.logger.Info("authenticated")
	d.logger.Debug("auth token loaded", "profile", d.cfg.Profile, "token_len", len(cfg.Token))
	return nil
}

func (d *Daemon) allRuntimeIDs() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	var ids []string
	for _, ws := range d.workspaces {
		ids = append(ids, ws.runtimeIDs...)
	}
	return ids
}

func (d *Daemon) findRuntime(id string) *Runtime {
	d.mu.Lock()
	defer d.mu.Unlock()
	if rt, ok := d.runtimeIndex[id]; ok {
		return &rt
	}
	return nil
}

func (d *Daemon) recordProfileLaunch(profileID, path, version string, fixedArgs []string) {
	if profileID == "" || path == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.profileLaunchSpecs == nil {
		d.profileLaunchSpecs = make(map[string]profileLaunchSpec)
	}
	d.profileLaunchSpecs[profileID] = profileLaunchSpec{
		path:      path,
		version:   version,
		fixedArgs: append([]string(nil), fixedArgs...),
	}
}

func (d *Daemon) customProfileLaunchForRuntime(runtimeID string) (profileLaunchSpec, bool) {
	if runtimeID == "" {
		return profileLaunchSpec{}, false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	rt, ok := d.runtimeIndex[runtimeID]
	if !ok || rt.ProfileID == "" {
		return profileLaunchSpec{}, false
	}
	spec, ok := d.profileLaunchSpecs[rt.ProfileID]
	if !ok || spec.path == "" {
		return profileLaunchSpec{}, false
	}
	spec.fixedArgs = append([]string(nil), spec.fixedArgs...)
	return spec, true
}

const runtimeVersionProbeConcurrency = 8

const runtimeVersionProbeAttempts = 2

var runtimeVersionProbeRetryDelay = 500 * time.Millisecond

var runtimeVersionProbeRetryWindow = time.Second

func (d *Daemon) probeBuiltinRuntime(ctx context.Context, name string, entry AgentEntry) (string, bool) {
	var (
		lastErr  error
		attempts int
	)
	for attempts < runtimeVersionProbeAttempts {
		if attempts > 0 {
			select {
			case <-ctx.Done():
			case <-time.After(runtimeVersionProbeRetryDelay):
			}

			if ctx.Err() != nil {
				break
			}
		}
		attempts++

		startedAt := time.Now()

		resolved, _ := d.resolveAgentEntry(ctx, name, entry)
		version, err := detectAgentVersion(ctx, resolved.Path)
		if err != nil {
			lastErr = err
			if time.Since(startedAt) >= runtimeVersionProbeRetryWindow {
				break
			}
			if attempts < runtimeVersionProbeAttempts {
				d.logger.Debug("agent version probe failed; retrying", "name", name, "attempt", attempts, "error", err)
			}
			continue
		}

		if err := checkAgentMinVersion(name, version); err != nil {
			d.logger.Warn("skip registering runtime: version too old", "name", name, "version", version, "error", err)
			return "", false
		}
		d.setAgentVersion(name, version)
		d.logger.Debug("agent version detected", "name", name, "version", version, "path", resolved.Path)
		return version, true
	}
	d.logger.Warn("skip registering runtime", "name", name, "attempts", attempts, "error", lastErr)
	return "", false
}

func (d *Daemon) detectBuiltinRuntimes(ctx context.Context) []map[string]string {
	type detected struct {
		name    string
		version string
	}
	var (
		mu      sync.Mutex
		results []detected
		g       errgroup.Group
	)
	g.SetLimit(runtimeVersionProbeConcurrency)
	for name, entry := range d.cfg.Agents {
		name, entry := name, entry
		g.Go(func() error {
			version, ok := d.probeBuiltinRuntime(ctx, name, entry)
			if !ok {
				return nil
			}
			mu.Lock()
			results = append(results, detected{name: name, version: version})
			mu.Unlock()
			return nil
		})
	}

	_ = g.Wait()

	sort.Slice(results, func(i, j int) bool { return results[i].name < results[j].name })

	runtimes := make([]map[string]string, 0, len(results))
	for _, r := range results {
		displayName := runtimeDisplayName(r.name)
		if d.cfg.DeviceName != "" {
			displayName = fmt.Sprintf("%s (%s)", displayName, d.cfg.DeviceName)
		}
		runtimes = append(runtimes, map[string]string{
			"name":    displayName,
			"type":    r.name,
			"version": r.version,
			"status":  "online",
		})
	}
	return runtimes
}

func cloneRuntimeEntries(in []map[string]string) []map[string]string {
	out := make([]map[string]string, 0, len(in))
	for _, entry := range in {
		cp := make(map[string]string, len(entry))
		for k, v := range entry {
			cp[k] = v
		}
		out = append(out, cp)
	}
	return out
}

func (d *Daemon) registerRuntimesForWorkspace(ctx context.Context, workspaceID string) (*RegisterResponse, string, error) {
	return d.registerRuntimesForWorkspaceBatch(ctx, workspaceID, d.detectBuiltinRuntimes(ctx))
}

func (d *Daemon) registerRuntimesForWorkspaceBatch(ctx context.Context, workspaceID string, builtins []map[string]string) (*RegisterResponse, string, error) {
	d.logger.Debug("registering runtimes for workspace", "workspace_id", workspaceID, "agent_count", len(d.cfg.Agents))
	runtimes := cloneRuntimeEntries(builtins)
	var failedProfiles []map[string]string

	profileSig := d.appendProfileRuntimes(ctx, workspaceID, &runtimes, &failedProfiles)

	if len(runtimes) == 0 && len(failedProfiles) == 0 {

		return nil, profileSig, ErrNoRuntimesToRegister
	}

	req := map[string]any{
		"workspace_id":      workspaceID,
		"daemon_id":         d.cfg.DaemonID,
		"legacy_daemon_ids": d.cfg.LegacyDaemonIDs,
		"device_name":       d.cfg.DeviceName,
		"cli_version":       d.cfg.CLIVersion,
		"launched_by":       d.cfg.LaunchedBy,
		"runtimes":          runtimes,
		"failed_profiles":   failedProfiles,
	}

	resp, err := d.client.Register(ctx, req)
	if err != nil {
		return nil, "", fmt.Errorf("register runtimes: %w", err)
	}
	if len(resp.Runtimes) == 0 && len(failedProfiles) == 0 {
		return nil, "", fmt.Errorf("register runtimes: empty response")
	}
	d.logger.Debug("register response", "workspace_id", workspaceID, "runtimes", len(resp.Runtimes), "repos", len(resp.Repos), "repos_version", resp.ReposVersion)
	return resp, profileSig, nil
}

func (d *Daemon) appendProfileRuntimes(ctx context.Context, workspaceID string, runtimes *[]map[string]string, failedProfiles *[]map[string]string) string {
	resp, err := d.client.GetRuntimeProfiles(ctx, workspaceID)
	if err != nil {

		d.logger.Info("skip custom runtime profiles: fetch failed (continuing with built-in runtimes)",
			"workspace_id", workspaceID, "error", err)
		return ""
	}
	if resp == nil {

		return profileSetSignature(nil)
	}
	for _, profile := range resp.RuntimeProfiles {
		if profile.CommandName == "" || profile.ProtocolFamily == "" {
			d.logger.Warn("skip custom runtime profile: missing command_name or protocol_family",
				"workspace_id", workspaceID, "profile_id", profile.ID, "display_name", profile.DisplayName)
			continue
		}
		if !agent.IsSupportedType(profile.ProtocolFamily) {
			reason := "unsupported protocol_family: " + profile.ProtocolFamily
			d.logger.Warn("skip custom runtime profile: unsupported protocol_family",
				"workspace_id", workspaceID, "profile_id", profile.ID,
				"display_name", profile.DisplayName, "protocol_family", profile.ProtocolFamily)
			*failedProfiles = append(*failedProfiles, map[string]string{
				"profile_id":   profile.ID,
				"command_name": profile.CommandName,
				"reason":       reason,
			})
			continue
		}

		var resolved string
		var failureReason string
		if override := strings.TrimSpace(d.cfg.ProfileCommandOverrides[profile.ID]); override != "" {
			if profilePathExecutable(override) {
				resolved = override
				d.logger.Info("custom runtime profile: using per-machine command path override",
					"workspace_id", workspaceID, "profile_id", profile.ID, "command_path", resolved)
			} else {
				failureReason = "Configured path override is not executable: " + override
				d.logger.Warn("custom runtime profile: command path override not executable; falling back to PATH",
					"workspace_id", workspaceID, "profile_id", profile.ID,
					"override_path", override, "command_name", profile.CommandName)
			}
		}
		if resolved == "" {
			r, err := lookPath(profile.CommandName)
			if err != nil {

				d.logger.Info("skip custom runtime profile: command not found on PATH",
					"workspace_id", workspaceID, "profile_id", profile.ID,
					"command_name", profile.CommandName, "error", err)
				if failureReason != "" {
					failureReason += "; "
				}
				failureReason += "command not found on PATH: " + profile.CommandName
				*failedProfiles = append(*failedProfiles, map[string]string{
					"profile_id":   profile.ID,
					"command_name": profile.CommandName,
					"reason":       failureReason,
				})
				continue
			}
			resolved = r
		}

		version, verErr := detectAgentVersion(ctx, resolved)
		if verErr != nil {
			d.logger.Debug("custom runtime profile: version probe failed (registering with empty version)",
				"workspace_id", workspaceID, "profile_id", profile.ID, "path", resolved, "error", verErr)
			version = ""
		}
		displayName := profile.DisplayName
		if d.cfg.DeviceName != "" {
			displayName = fmt.Sprintf("%s (%s)", displayName, d.cfg.DeviceName)
		}
		d.recordProfileLaunch(profile.ID, resolved, version, profile.FixedArgs)
		d.logger.Info("registering custom runtime profile",
			"workspace_id", workspaceID, "profile_id", profile.ID,
			"protocol_family", profile.ProtocolFamily, "command_path", resolved)
		*runtimes = append(*runtimes, map[string]string{
			"name":       displayName,
			"type":       profile.ProtocolFamily,
			"version":    version,
			"status":     "online",
			"profile_id": profile.ID,
		})
	}
	return profileSetSignature(resp.RuntimeProfiles)
}

func profileSetSignature(profiles []RuntimeProfile) string {
	if len(profiles) == 0 {
		return "0"
	}
	sorted := append([]RuntimeProfile(nil), profiles...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	h := fnv.New64a()

	const sep = "\x1f"
	for _, p := range sorted {
		fmt.Fprintf(h, "%s%s%t%s%s%s%s%s%s%s",
			p.ID, sep,
			p.Enabled, sep,
			p.ProtocolFamily, sep,
			p.CommandName, sep,
			p.Visibility, sep,
		)
		for _, a := range p.FixedArgs {
			fmt.Fprintf(h, "%s%s", a, sep)
		}

		h.Write([]byte("\x1e"))
	}
	return strconv.FormatUint(h.Sum64(), 16)
}

func newWorkspaceState(workspaceID string, runtimeIDs []string, reposVersion string, repos []RepoData, settings json.RawMessage) *workspaceState {
	return &workspaceState{
		workspaceID:     workspaceID,
		runtimeIDs:      runtimeIDs,
		reposVersion:    reposVersion,
		allowedRepoURLs: repoAllowlist(repos),
		settings:        settings,
	}
}

func repoAllowlist(repos []RepoData) map[string]struct{} {
	allowed := make(map[string]struct{}, len(repos))
	for _, repo := range repos {
		if repo.URL == "" {
			continue
		}
		allowed[repo.URL] = struct{}{}
	}
	return allowed
}

func (d *Daemon) setWorkspaceRepoSyncError(workspaceID, syncErr string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if ws, ok := d.workspaces[workspaceID]; ok {
		ws.lastRepoSyncErr = syncErr
	}
}

func (d *Daemon) workspaceRepoAllowed(workspaceID, repoURL string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	ws, ok := d.workspaces[workspaceID]
	if !ok {
		return false
	}
	if _, allowed := ws.allowedRepoURLs[repoURL]; allowed {
		return true
	}
	if _, allowed := ws.taskRepoURLs[repoURL]; allowed {
		return true
	}
	return false
}

func (d *Daemon) workspaceLastRepoSyncErr(workspaceID string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	ws, ok := d.workspaces[workspaceID]
	if !ok {
		return ""
	}
	return ws.lastRepoSyncErr
}

func (d *Daemon) workspaceCoAuthoredByEnabled(workspaceID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	ws, ok := d.workspaces[workspaceID]
	if !ok || len(ws.settings) == 0 {
		return true
	}
	var s struct {
		GitHubEnabled       *bool `json:"github_enabled"`
		CoAuthoredByEnabled *bool `json:"co_authored_by_enabled"`
	}
	if err := json.Unmarshal(ws.settings, &s); err != nil {
		return true
	}
	if s.GitHubEnabled != nil && !*s.GitHubEnabled {
		return false
	}
	if s.CoAuthoredByEnabled == nil {
		return true
	}
	return *s.CoAuthoredByEnabled
}

func (d *Daemon) registerTaskRepos(workspaceID, taskID string, repos []RepoData) {
	if len(repos) == 0 {
		return
	}

	type repoCandidate struct {
		url     string
		tracked bool
	}

	d.mu.Lock()
	ws, ok := d.workspaces[workspaceID]
	if !ok {
		d.mu.Unlock()
		return
	}
	if ws.taskRepoURLs == nil {
		ws.taskRepoURLs = make(map[string]struct{}, len(repos))
	}
	if taskID != "" && ws.taskRepoRefs == nil {
		ws.taskRepoRefs = make(map[string]map[string]string)
	}
	candidates := make([]repoCandidate, 0, len(repos))
	for _, repo := range repos {
		url := strings.TrimSpace(repo.URL)
		if url == "" {
			continue
		}

		_, inWorkspace := ws.allowedRepoURLs[url]
		_, inTask := ws.taskRepoURLs[url]
		ws.taskRepoURLs[url] = struct{}{}
		if taskID != "" {
			if ws.taskRepoRefs[taskID] == nil {
				ws.taskRepoRefs[taskID] = make(map[string]string, len(repos))
			}
			if _, exists := ws.taskRepoRefs[taskID][url]; !exists {
				ws.taskRepoRefs[taskID][url] = strings.TrimSpace(repo.Ref)
			}
		}
		candidates = append(candidates, repoCandidate{
			url:     url,
			tracked: inWorkspace || inTask,
		})
	}
	d.mu.Unlock()

	toSync := make([]RepoData, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.tracked && d.repoCache != nil && d.repoCache.Lookup(workspaceID, candidate.url) != "" {
			continue
		}
		toSync = append(toSync, RepoData{URL: candidate.url})
	}

	if d.repoCache != nil && len(toSync) > 0 {

		d.bgSyncs.Add(1)
		go func() {
			defer d.bgSyncs.Done()
			d.syncWorkspaceRepos(workspaceID, toSync)
		}()
	}
}

func (d *Daemon) taskRepoDefaultRef(workspaceID, taskID, repoURL string) string {
	taskID = strings.TrimSpace(taskID)
	repoURL = strings.TrimSpace(repoURL)
	if taskID == "" || repoURL == "" {
		return ""
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	ws, ok := d.workspaces[workspaceID]
	if !ok || ws.taskRepoRefs == nil {
		return ""
	}
	return strings.TrimSpace(ws.taskRepoRefs[taskID][repoURL])
}

func (d *Daemon) clearTaskRepoRefs(workspaceID, taskID string) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if ws, ok := d.workspaces[workspaceID]; ok && ws.taskRepoRefs != nil {
		delete(ws.taskRepoRefs, taskID)
	}
}

func (d *Daemon) waitBackgroundSyncs() {
	d.bgSyncs.Wait()
}

func (d *Daemon) syncWorkspaceRepos(workspaceID string, repos []RepoData) {
	if d.repoCache == nil {
		return
	}
	if err := d.repoCache.Sync(workspaceID, repoDataToInfo(repos)); err != nil {
		d.setWorkspaceRepoSyncError(workspaceID, err.Error())
		d.logger.Warn("repo cache sync failed", "workspace_id", workspaceID, "error", err)
		return
	}
	d.setWorkspaceRepoSyncError(workspaceID, "")
}

func (d *Daemon) refreshWorkspaceRepos(ctx context.Context, workspaceID string) (*WorkspaceReposResponse, error) {
	refreshCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resp, err := d.client.GetWorkspaceRepos(refreshCtx, workspaceID)
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	if ws, ok := d.workspaces[workspaceID]; ok {
		ws.reposVersion = resp.ReposVersion
		ws.allowedRepoURLs = repoAllowlist(resp.Repos)

		ws.settings = resp.Settings
	}
	d.mu.Unlock()

	return resp, nil
}

func (d *Daemon) refreshWorkspaceRuntimeProfiles(ctx context.Context, workspaceID string) error {
	refreshCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resp, err := d.client.GetRuntimeProfiles(refreshCtx, workspaceID)
	if err != nil {

		return err
	}
	var profiles []RuntimeProfile
	if resp != nil {
		profiles = resp.RuntimeProfiles
	}
	live := profileSetSignature(profiles)

	d.mu.Lock()
	ws, ok := d.workspaces[workspaceID]
	if !ok {
		d.mu.Unlock()

		return nil
	}
	cached := ws.profileSetSig
	d.mu.Unlock()

	if cached == live {
		return nil
	}

	d.logger.Info("custom runtime profile set changed; refreshing workspace runtimes",
		"workspace_id", workspaceID, "previous_sig", cached, "current_sig", live,
		"profile_count", len(profiles))

	regResp, profileSig, err := d.registerRuntimesForWorkspace(ctx, workspaceID)
	if err != nil {
		if errors.Is(err, ErrNoRuntimesToRegister) {

			return d.convergeWorkspaceRuntimesToZero(ctx, workspaceID, profileSig)
		}
		return err
	}

	newIDs, droppedIDs, ok := d.applyRegisterResponseInPlace(workspaceID, regResp, profileSig)
	if !ok {
		return fmt.Errorf("workspace %s no longer tracked", workspaceID)
	}

	for _, rid := range newIDs {
		d.logger.Info("re-registered runtime after profile drift",
			"workspace_id", workspaceID, "runtime_id", rid)
	}
	d.notifyRuntimeSetChanged()

	if len(droppedIDs) > 0 {
		if err := d.client.Deregister(ctx, droppedIDs); err != nil {
			d.logger.Warn("deregister of dropped runtimes after profile drift failed",
				"workspace_id", workspaceID, "runtime_ids", droppedIDs, "error", err)
		}
	}

	return nil
}

func (d *Daemon) convergeWorkspaceRuntimesToZero(ctx context.Context, workspaceID, profileSig string) error {
	d.mu.Lock()
	ws, ok := d.workspaces[workspaceID]
	if !ok {
		d.mu.Unlock()
		return nil
	}
	oldRuntimeIDs := append([]string(nil), ws.runtimeIDs...)
	for _, rid := range oldRuntimeIDs {
		delete(d.runtimeIndex, rid)
	}
	ws.runtimeIDs = nil
	if profileSig != "" {

		ws.profileSetSig = profileSig
	}
	d.mu.Unlock()

	d.logger.Info("custom runtime profile drift converged to zero; clearing local tracking",
		"workspace_id", workspaceID, "deregistered_runtime_ids", oldRuntimeIDs)

	if len(oldRuntimeIDs) > 0 {
		if err := d.client.Deregister(ctx, oldRuntimeIDs); err != nil {

			d.logger.Warn("deregister after zero-runtime convergence failed",
				"workspace_id", workspaceID, "runtime_ids", oldRuntimeIDs, "error", err)
		}
	}
	d.notifyRuntimeSetChanged()
	return nil
}

func (d *Daemon) ensureRepoReady(ctx context.Context, workspaceID, repoURL string) error {
	if d.repoCache == nil {
		return fmt.Errorf("repo cache not initialized")
	}

	repoURL = strings.TrimSpace(repoURL)

	d.mu.Lock()
	ws, ok := d.workspaces[workspaceID]
	d.mu.Unlock()
	if !ok {
		return fmt.Errorf("workspace is not watched by this daemon: %s", workspaceID)
	}

	cacheHitOnEntry := d.workspaceRepoAllowed(workspaceID, repoURL) && d.repoCache.Lookup(workspaceID, repoURL) != ""

	ws.repoRefreshMu.Lock()
	defer ws.repoRefreshMu.Unlock()

	if !cacheHitOnEntry && d.workspaceRepoAllowed(workspaceID, repoURL) && d.repoCache.Lookup(workspaceID, repoURL) != "" {
		return nil
	}

	resp, err := d.refreshWorkspaceRepos(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("refresh workspace repos: %w", err)
	}

	if !d.workspaceRepoAllowed(workspaceID, repoURL) {
		return ErrRepoNotConfigured
	}

	if d.repoCache.Lookup(workspaceID, repoURL) != "" {
		return nil
	}

	d.syncWorkspaceRepos(workspaceID, resp.Repos)

	if d.repoCache.Lookup(workspaceID, repoURL) != "" {
		return nil
	}

	if syncErr := d.workspaceLastRepoSyncErr(workspaceID); syncErr != "" {
		return fmt.Errorf("repo is configured but not synced: %s", syncErr)
	}

	return fmt.Errorf("repo is configured but not synced")
}

const DefaultTokenRenewalInterval = 3 * 24 * time.Hour

func (d *Daemon) preflightAuth(ctx context.Context) error {
	d.tryRenewToken(ctx)
	return d.syncWorkspacesFromAPI(ctx, false)
}

func (d *Daemon) tokenRenewalLoop(ctx context.Context) {
	ticker := time.NewTicker(DefaultTokenRenewalInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.tryRenewToken(ctx)
		}
	}
}

func (d *Daemon) tryRenewToken(ctx context.Context) {
	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	resp, err := d.client.RenewToken(reqCtx)
	if err != nil {
		if isUnauthorizedError(err) {
			loginHint := "'goosar login'"
			if d.cfg.Profile != "" {
				loginHint = fmt.Sprintf("'goosar login --profile %s'", d.cfg.Profile)
			}
			d.logger.Warn("auth token rejected by server — run "+loginHint+" to re-authenticate, then restart the daemon", "error", err)
			return
		}
		d.logger.Debug("token renewal failed; will retry on next cycle", "error", err)
		return
	}
	if resp.Renewed {
		d.logger.Info("auth token renewed", "expires_at", resp.ExpiresAt)
	} else {
		d.logger.Debug("auth token not yet eligible for renewal", "expires_at", resp.ExpiresAt)
	}
}

func (d *Daemon) workspaceSyncLoop(ctx context.Context) {
	timer := time.NewTimer(jitterDuration(d.workspaceSyncBaseInterval()))
	defer timer.Stop()

	var reconcileCh <-chan struct{}
	if d.reconcile != nil {
		reconcileCh = d.reconcile.notify()
	}
	var workspaceChangesCh <-chan struct{}
	if d.workspaceChanges != nil {
		workspaceChangesCh = d.workspaceChanges.notify()
	}

	var consecutiveFailures int
	resetTimer := func() {
		interval := workspaceSyncBackoff(d.workspaceSyncBaseInterval(), consecutiveFailures)
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(jitterDuration(interval))
	}

	syncNow := func(reconcileProfiles bool) {
		if err := d.syncWorkspacesFromAPI(ctx, reconcileProfiles); err != nil {
			consecutiveFailures++
			d.logger.Debug("workspace sync failed", "error", err)
		} else {
			consecutiveFailures = 0
		}
		resetTimer()
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-reconcileCh:
			if d.reconcile != nil {
				reconcileCh = d.reconcile.notify()
			}
			syncNow(true)
		case <-workspaceChangesCh:
			syncNow(false)
		case <-timer.C:
			syncNow(false)
		}
	}
}

func (d *Daemon) workspaceSyncBaseInterval() time.Duration {
	if len(d.allRuntimeIDs()) == 0 {
		return DefaultWorkspaceBootstrapSyncInterval
	}
	if d.client.usesLegacyWorkspaceEndpoint() {
		return DefaultWorkspaceLegacySyncInterval
	}
	return DefaultWorkspaceSyncInterval
}

func workspaceSyncBackoff(base time.Duration, consecutiveFailures int) time.Duration {
	maxInterval := DefaultWorkspaceSyncMaxBackoff
	if base == DefaultWorkspaceBootstrapSyncInterval {
		maxInterval = DefaultWorkspaceLegacySyncInterval
	}
	interval := base
	for i := 0; i < consecutiveFailures; i++ {
		if interval >= maxInterval/2 {
			return maxInterval
		}
		interval *= 2
	}
	if interval > maxInterval {
		return maxInterval
	}
	return interval
}

func (d *Daemon) syncWorkspacesFromAPI(ctx context.Context, reconcileProfiles bool) error {
	d.reloading.Lock()
	defer d.reloading.Unlock()

	apiCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	workspaces, err := d.client.ListWorkspaces(apiCtx)
	if err != nil {
		return fmt.Errorf("list workspaces: %w", err)
	}
	d.logger.Debug("workspace sync: fetched workspaces", "count", len(workspaces))

	apiIDs := make(map[string]string, len(workspaces))
	for _, ws := range workspaces {
		apiIDs[ws.ID] = ws.Name
	}

	d.mu.Lock()
	currentIDs := make(map[string]bool, len(d.workspaces))
	for id := range d.workspaces {
		currentIDs[id] = true
	}
	d.mu.Unlock()

	var (
		builtins       []map[string]string
		builtinsProbed bool
	)
	probeBuiltins := func() []map[string]string {
		if !builtinsProbed {
			builtins = d.detectBuiltinRuntimes(ctx)
			builtinsProbed = true
		}
		return builtins
	}

	var registered int
	var removed int
	for id, name := range apiIDs {
		if currentIDs[id] {
			if reconcileProfiles {
				if err := d.refreshWorkspaceRuntimeProfiles(ctx, id); err != nil {
					d.logger.Debug("workspace reconcile: profile refresh failed", "workspace_id", id, "error", err)
				}
			}

			if !d.workspaceNeedsRuntimeRecovery(id) {
				continue
			}
			d.logger.Info("workspace has no runtimes; retrying registration", "workspace_id", id, "name", name)
			if err := d.reregisterWorkspaceAfterRuntimeGone(ctx, id); err != nil {
				d.logger.Warn("retry register failed", "workspace_id", id, "error", err)
				continue
			}
			registered++
			continue
		}
		resp, profileSig, err := d.registerRuntimesForWorkspaceBatch(ctx, id, probeBuiltins())
		if err != nil {
			d.logger.Error("failed to register runtimes", "workspace_id", id, "name", name, "error", err)
			continue
		}
		runtimeIDs := make([]string, len(resp.Runtimes))
		for i, rt := range resp.Runtimes {
			runtimeIDs[i] = rt.ID
			d.logger.Info("registered runtime", "workspace_id", id, "runtime_id", rt.ID, "provider", rt.Provider)
		}
		d.mu.Lock()
		ws := newWorkspaceState(id, runtimeIDs, resp.ReposVersion, resp.Repos, resp.Settings)

		ws.profileSetSig = profileSig
		d.workspaces[id] = ws
		for _, rt := range resp.Runtimes {
			d.runtimeIndex[rt.ID] = rt
		}
		d.mu.Unlock()

		if d.repoCache != nil && len(resp.Repos) > 0 {
			go d.syncWorkspaceRepos(id, resp.Repos)
		}

		for _, rid := range runtimeIDs {
			if err := d.client.RecoverOrphans(ctx, rid); err != nil {
				d.logger.Warn("recover-orphans failed", "runtime_id", rid, "error", err)
			}
		}

		d.logger.Info("watching workspace", "workspace_id", id, "name", name, "runtimes", len(resp.Runtimes), "repos", len(resp.Repos))
		registered++
	}

	for id := range currentIDs {
		if _, ok := apiIDs[id]; !ok {
			d.mu.Lock()
			if ws, exists := d.workspaces[id]; exists {
				for _, rid := range ws.runtimeIDs {
					delete(d.runtimeIndex, rid)
				}
			}
			delete(d.workspaces, id)
			d.mu.Unlock()
			d.logger.Info("stopped watching workspace", "workspace_id", id)
			removed++
		}
	}
	if registered > 0 || removed > 0 {
		d.notifyRuntimeSetChanged()
	}

	if len(d.allRuntimeIDs()) == 0 && registered == 0 && len(workspaces) > 0 {
		return fmt.Errorf("failed to register runtimes for any of the %d workspace(s)", len(workspaces))
	}
	if registered > 0 || removed > 0 {
		d.logger.Debug("workspace sync done", "registered", registered, "removed", removed, "tracked", len(apiIDs))
	}
	return nil
}

func (d *Daemon) heartbeatLoop(ctx context.Context) {
	runtimeSetCh, unsub := d.runtimeSet.Subscribe()
	defer unsub()

	cancels := make(map[string]context.CancelFunc)
	defer func() {
		for _, cancel := range cancels {
			cancel()
		}
	}()

	sync := func() {
		want := make(map[string]struct{})
		for _, rid := range d.allRuntimeIDs() {
			want[rid] = struct{}{}
		}
		for rid, cancel := range cancels {
			if _, ok := want[rid]; !ok {
				cancel()
				delete(cancels, rid)
			}
		}
		for rid := range want {
			if _, ok := cancels[rid]; ok {
				continue
			}
			rctx, rcancel := context.WithCancel(ctx)
			cancels[rid] = rcancel
			go d.runRuntimeHeartbeat(rctx, rid)
		}
	}

	sync()
	for {
		select {
		case <-ctx.Done():
			return
		case <-runtimeSetCh:
			sync()
		}
	}
}

func (d *Daemon) runRuntimeHeartbeat(ctx context.Context, rid string) {
	interval := d.cfg.HeartbeatInterval
	if interval <= 0 {
		interval = 15 * time.Second
	}

	if jitter := time.Duration(rand.Int63n(int64(interval))); jitter > 0 {
		select {
		case <-ctx.Done():
			return
		case <-time.After(jitter):
		}
	}

	consecutiveTransientFailures := 0
	tick := func() {
		if d.runHeartbeatTick(ctx, rid) {
			consecutiveTransientFailures++
			if consecutiveTransientFailures == 2 {
				d.client.CloseIdleConnections()
			}
			return
		}
		consecutiveTransientFailures = 0
	}

	tick()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tick()
		}
	}
}

func (d *Daemon) runHeartbeatTick(ctx context.Context, rid string) bool {

	if d.wsHeartbeatRecentlyAcked(rid) {
		d.logger.Debug("heartbeat: skipping HTTP tick, WS recently acked", "runtime_id", rid)
		return false
	}
	d.logger.Debug("heartbeat: HTTP tick", "runtime_id", rid)
	resp, err := d.client.SendHeartbeat(ctx, rid)
	if err != nil {
		if ctx.Err() == nil {
			if isRuntimeNotFoundError(err) {

				d.resetWorkspaceAccessDenialStrikes(rid)
				go d.handleRuntimeGone(rid)
				return false
			}
			if isWorkspaceAccessDeniedError(err) {

				if d.recordWorkspaceAccessDenied(rid) {
					go d.handleWorkspaceAccessRevoked(rid)
				}
				return false
			}

			d.resetWorkspaceAccessDenialStrikes(rid)
			d.logger.Warn("heartbeat failed", "runtime_id", rid, "error", err)
		}
		return ctx.Err() == nil && isTransientError(err)
	}
	d.markRuntimeAccessRestored(rid)
	if resp != nil && resp.RuntimeGone {

		go d.handleRuntimeGone(rid)
		return false
	}
	d.handleHeartbeatActions(ctx, rid, resp)
	return false
}

func (d *Daemon) handleHeartbeatActions(ctx context.Context, runtimeID string, resp *HeartbeatResponse) {
	if resp == nil {
		return
	}
	if resp.PendingUpdate != nil || resp.PendingModelList != nil || resp.PendingLocalSkills != nil || resp.PendingLocalSkillImport != nil {
		d.logger.Debug("heartbeat: pending actions",
			"runtime_id", runtimeID,
			"update", resp.PendingUpdate != nil,
			"model_list", resp.PendingModelList != nil,
			"local_skills", resp.PendingLocalSkills != nil,
			"local_skill_import", resp.PendingLocalSkillImport != nil,
		)
	}
	if resp.PendingUpdate != nil {
		go d.handleUpdate(ctx, runtimeID, resp.PendingUpdate)
	}
	if resp.PendingModelList != nil {
		if rt := d.findRuntime(runtimeID); rt != nil {
			go d.handleModelList(ctx, *rt, resp.PendingModelList.ID)
		}
	}
	if resp.PendingLocalSkills != nil {
		if rt := d.findRuntime(runtimeID); rt != nil {
			go d.handleLocalSkillList(ctx, *rt, resp.PendingLocalSkills.ID)
		}
	}

	if len(resp.PendingLocalSkillImports) > 0 {
		if rt := d.findRuntime(runtimeID); rt != nil {
			for _, imp := range resp.PendingLocalSkillImports {
				go d.handleLocalSkillImport(ctx, *rt, imp)
			}
		}
	} else if resp.PendingLocalSkillImport != nil {
		if rt := d.findRuntime(runtimeID); rt != nil {
			go d.handleLocalSkillImport(ctx, *rt, *resp.PendingLocalSkillImport)
		}
	}
}

func (d *Daemon) handleModelList(ctx context.Context, rt Runtime, requestID string) {
	d.logger.Info("model list requested", "runtime_id", rt.ID, "request_id", requestID, "provider", rt.Provider)

	entry, ok := d.cfg.Agents[rt.Provider]
	if !ok {
		d.reportModelListResult(ctx, rt, requestID, map[string]any{
			"status": "failed",
			"error":  fmt.Sprintf("no agent configured for provider %q", rt.Provider),
		})
		return
	}

	entry, _ = d.resolveAgentEntry(ctx, rt.Provider, entry)

	models, err := agent.ListModels(ctx, rt.Provider, entry.Path)
	if err != nil {
		d.reportModelListResult(ctx, rt, requestID, map[string]any{
			"status": "failed",
			"error":  err.Error(),
		})
		return
	}

	type thinkingLevelWire struct {
		Value       string `json:"value"`
		Label       string `json:"label"`
		Description string `json:"description,omitempty"`
	}
	type modelThinkingWire struct {
		SupportedLevels []thinkingLevelWire `json:"supported_levels"`
		DefaultLevel    string              `json:"default_level,omitempty"`
	}
	type modelServiceTierWire struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
	}
	type modelWire struct {
		ID           string                 `json:"id"`
		Label        string                 `json:"label"`
		Default      bool                   `json:"default,omitempty"`
		Thinking     *modelThinkingWire     `json:"thinking,omitempty"`
		ServiceTiers []modelServiceTierWire `json:"service_tiers,omitempty"`
	}
	wire := make([]modelWire, 0, len(models))
	for _, m := range models {
		entry := modelWire{
			ID:      m.ID,
			Label:   m.Label,
			Default: m.Default,
		}
		if m.Thinking != nil {
			levels := make([]thinkingLevelWire, 0, len(m.Thinking.SupportedLevels))
			for _, lvl := range m.Thinking.SupportedLevels {
				levels = append(levels, thinkingLevelWire{
					Value:       lvl.Value,
					Label:       lvl.Label,
					Description: lvl.Description,
				})
			}
			entry.Thinking = &modelThinkingWire{
				SupportedLevels: levels,
				DefaultLevel:    m.Thinking.DefaultLevel,
			}
		}
		for _, tier := range m.ServiceTiers {
			entry.ServiceTiers = append(entry.ServiceTiers, modelServiceTierWire{
				ID:          tier.ID,
				Name:        tier.Name,
				Description: tier.Description,
			})
		}
		wire = append(wire, entry)
	}
	d.reportModelListResult(ctx, rt, requestID, map[string]any{
		"status":    "completed",
		"models":    wire,
		"supported": agent.ModelSelectionSupported(rt.Provider),
	})
}

func (d *Daemon) handleLocalSkillList(ctx context.Context, rt Runtime, requestID string) {
	d.logger.Info("runtime local skills requested", "runtime_id", rt.ID, "request_id", requestID, "provider", rt.Provider)

	skills, supported, err := listRuntimeLocalSkills(rt.Provider)
	if err != nil {
		d.reportLocalSkillListResult(ctx, rt, requestID, map[string]any{
			"status": "failed",
			"error":  err.Error(),
		})
		return
	}
	mcpServers, mcpSupported, err := listRuntimeLocalMcpServers(rt.Provider, d.mcpPolicy.Load())
	if err != nil {
		d.logger.Warn("runtime local MCP discovery failed", "runtime_id", rt.ID, "provider", rt.Provider, "error", err)
		mcpServers = []runtimeLocalMcpServerSummary{}
		mcpSupported = false
	}

	d.reportLocalSkillListResult(ctx, rt, requestID, map[string]any{
		"status":        "completed",
		"skills":        skills,
		"supported":     supported,
		"mcp_servers":   mcpServers,
		"mcp_supported": mcpSupported,
	})
}

func (d *Daemon) handleLocalSkillImport(ctx context.Context, rt Runtime, pending PendingLocalSkillImport) {
	d.logger.Info("runtime local skill import requested", "runtime_id", rt.ID, "request_id", pending.ID, "provider", rt.Provider, "skill_key", pending.SkillKey)

	skill, supported, err := loadRuntimeLocalSkillBundle(rt.Provider, pending.SkillKey)
	if err != nil {
		d.reportLocalSkillImportResult(ctx, rt, pending.ID, map[string]any{
			"status": "failed",
			"error":  err.Error(),
		})
		return
	}
	if !supported {
		d.reportLocalSkillImportResult(ctx, rt, pending.ID, map[string]any{
			"status": "failed",
			"error":  fmt.Sprintf("provider %q does not expose runtime local skills", rt.Provider),
		})
		return
	}

	d.reportLocalSkillImportResult(ctx, rt, pending.ID, map[string]any{
		"status": "completed",
		"skill":  skill,
	})
}

var runtimeReportBackoffs = []time.Duration{0, 500 * time.Millisecond, 2 * time.Second, 4 * time.Second}

func (d *Daemon) reportLocalSkillListResult(ctx context.Context, rt Runtime, requestID string, payload map[string]any) {
	d.reportRuntimeResultWithRetry(ctx, "local_skill_list", rt.ID, requestID, func(ctx context.Context) error {
		return d.client.ReportLocalSkillListResult(ctx, rt.ID, requestID, payload)
	})
}

func (d *Daemon) reportLocalSkillImportResult(ctx context.Context, rt Runtime, requestID string, payload map[string]any) {
	d.reportRuntimeResultWithRetry(ctx, "local_skill_import", rt.ID, requestID, func(ctx context.Context) error {
		return d.client.ReportLocalSkillImportResult(ctx, rt.ID, requestID, payload)
	})
}

func (d *Daemon) reportModelListResult(ctx context.Context, rt Runtime, requestID string, payload map[string]any) {
	d.reportRuntimeResultWithRetry(ctx, "model_list", rt.ID, requestID, func(ctx context.Context) error {
		return d.client.ReportModelListResult(ctx, rt.ID, requestID, payload)
	})
}

func (d *Daemon) reportRuntimeResultWithRetry(ctx context.Context, kind, runtimeID, requestID string, fn func(context.Context) error) {
	var lastErr error
	for attempt, wait := range runtimeReportBackoffs {
		if wait > 0 {
			select {
			case <-ctx.Done():
				d.logger.Error("runtime async report cancelled",
					"kind", kind, "runtime_id", runtimeID, "request_id", requestID,
					"attempt", attempt, "error", ctx.Err())
				return
			case <-time.After(wait):
			}
		}
		err := fn(ctx)
		if err == nil {
			if attempt > 0 {
				d.logger.Info("runtime async report succeeded after retry",
					"kind", kind, "runtime_id", runtimeID, "request_id", requestID,
					"attempt", attempt+1)
			}
			return
		}
		lastErr = err

		var reqErr *requestError
		if errors.As(err, &reqErr) && reqErr.StatusCode >= 400 && reqErr.StatusCode < 500 {
			d.logger.Error("runtime async report rejected — not retrying",
				"kind", kind, "runtime_id", runtimeID, "request_id", requestID,
				"status", reqErr.StatusCode, "error", err)
			return
		}

		d.logger.Warn("runtime async report failed — will retry",
			"kind", kind, "runtime_id", runtimeID, "request_id", requestID,
			"attempt", attempt+1, "error", err)
	}
	d.logger.Error("runtime async report exhausted retries",
		"kind", kind, "runtime_id", runtimeID, "request_id", requestID, "error", lastErr)
}

func (d *Daemon) handleUpdate(ctx context.Context, runtimeID string, update *PendingUpdate) {

	if d.cfg.LaunchedBy == "desktop" {
		d.logger.Info("refusing CLI self-update: daemon is managed by Desktop", "runtime_id", runtimeID, "update_id", update.ID)
		d.reportUpdateResult(ctx, runtimeID, update.ID, map[string]any{
			"status": "failed",
			"error":  "CLI is managed by Goosar Desktop — update the Desktop app to upgrade the CLI",
		})
		return
	}

	if !d.updating.CompareAndSwap(false, true) {
		d.logger.Warn("update already in progress, ignoring", "runtime_id", runtimeID, "update_id", update.ID)
		return
	}
	defer d.updating.Store(false)

	d.logger.Info("CLI update requested", "runtime_id", runtimeID, "update_id", update.ID, "target_version", update.TargetVersion)

	d.reportUpdateResult(ctx, runtimeID, update.ID, map[string]any{
		"status": "running",
	})

	output, err := d.runUpdateFn(update.TargetVersion)
	if err != nil {
		d.logger.Error("CLI update failed", "error", err, "output", output)
		d.reportUpdateResult(ctx, runtimeID, update.ID, map[string]any{
			"status": "failed",
			"error":  err.Error(),
		})
		return
	}

	d.logger.Info("CLI update completed successfully", "output", output)
	d.reportUpdateResult(ctx, runtimeID, update.ID, map[string]any{
		"status": "completed",
		"output": fmt.Sprintf("Updated to %s", update.TargetVersion),
	})

	d.triggerRestart()
}

func (d *Daemon) runUpdate(targetVersion string) (string, error) {
	if cli.IsBrewInstall() {
		d.logger.Info("updating CLI via Homebrew...")
		out, err := cli.UpdateViaBrew()
		if err != nil {
			return out, fmt.Errorf("brew upgrade failed: %w", err)
		}
		return out, nil
	}
	d.logger.Info("updating CLI via direct download...", "target_version", targetVersion)
	out, err := cli.UpdateViaDownload(targetVersion)
	if err != nil {
		return out, fmt.Errorf("download update failed: %w", err)
	}
	return out, nil
}

var updateReportBackoffs = []time.Duration{0, 500 * time.Millisecond, 2 * time.Second, 4 * time.Second}

func (d *Daemon) reportUpdateResult(ctx context.Context, runtimeID, updateID string, payload map[string]any) {
	d.reportUpdateResultWithRetry(ctx, runtimeID, updateID, func(ctx context.Context) error {
		return d.client.ReportUpdateResult(ctx, runtimeID, updateID, payload)
	})
}

func (d *Daemon) reportUpdateResultWithRetry(ctx context.Context, runtimeID, updateID string, fn func(context.Context) error) {
	var lastErr error
	for attempt, wait := range updateReportBackoffs {
		if wait > 0 {
			select {
			case <-ctx.Done():
				d.logger.Error("CLI update report cancelled",
					"runtime_id", runtimeID, "update_id", updateID,
					"attempt", attempt, "error", ctx.Err())
				return
			case <-time.After(wait):
			}
		}

		err := fn(ctx)
		if err == nil {
			if attempt > 0 {
				d.logger.Info("CLI update report succeeded after retry",
					"runtime_id", runtimeID, "update_id", updateID,
					"attempt", attempt+1)
			}
			return
		}
		lastErr = err

		var reqErr *requestError
		if errors.As(err, &reqErr) && reqErr.StatusCode >= 400 && reqErr.StatusCode < 500 {
			d.logger.Error("CLI update report rejected — not retrying",
				"runtime_id", runtimeID, "update_id", updateID,
				"status", reqErr.StatusCode, "error", err)
			return
		}

		d.logger.Warn("CLI update report failed — will retry",
			"runtime_id", runtimeID, "update_id", updateID,
			"attempt", attempt+1, "error", err)
	}
	d.logger.Error("CLI update report exhausted retries",
		"runtime_id", runtimeID, "update_id", updateID, "error", lastErr)
}

func (d *Daemon) tryEnterClaim() bool {
	d.claimMu.Lock()
	defer d.claimMu.Unlock()
	if d.pauseClaims {
		return false
	}
	d.claimsInFlight++
	return true
}

func (d *Daemon) exitClaim() {
	d.claimMu.Lock()
	defer d.claimMu.Unlock()
	d.claimsInFlight--
}

func (d *Daemon) trySetClaimBarrier() bool {
	d.claimMu.Lock()
	defer d.claimMu.Unlock()
	if d.claimsInFlight > 0 || d.activeTasks.Load() > 0 {
		return false
	}
	d.pauseClaims = true
	return true
}

func (d *Daemon) releaseClaimBarrier() {
	d.claimMu.Lock()
	defer d.claimMu.Unlock()
	d.pauseClaims = false
}

func (d *Daemon) triggerRestart() {
	newBin, err := resolveSelfExecutable()
	if err != nil {
		d.logger.Error("could not resolve executable path for restart", "error", err)
		return
	}

	if isBrewInstall() {
		if brewPrefix := getBrewPrefix(); brewPrefix != "" {
			newBin = filepath.Join(brewPrefix, "bin", "goosar")
		} else if prefix := matchKnownBrewPrefix(newBin); prefix != "" {
			newBin = filepath.Join(prefix, "bin", "goosar")
		} else {
			d.logger.Warn("brew install detected but prefix could not be resolved; restart may fail",
				"executable", newBin)
		}
	} else {
		if resolved, err := filepath.EvalSymlinks(newBin); err == nil {
			newBin = resolved
		}
	}

	d.logger.Info("scheduling daemon restart", "new_binary", newBin)
	d.restartBinary = newBin

	if d.cancelFunc != nil {
		d.cancelFunc()
	}
}

func (d *Daemon) pollLoop(ctx context.Context, taskWakeups <-chan taskWakeup) error {
	sem := newTaskSlotSemaphore(d.cfg.MaxConcurrentTasks)
	var taskWG sync.WaitGroup

	taskCtx, taskCancel := taskParentContext(ctx)

	leakTaskCtx := false
	defer func() {
		if !leakTaskCtx {
			taskCancel()
		}
	}()

	runtimeSetCh, unsub := d.runtimeSet.Subscribe()
	defer unsub()

	wakeup := make(chan struct{}, 1)
	nudge := func() {
		signalPollerWakeup(wakeup)
	}

	pollerCtx, pollerCancel := context.WithCancel(ctx)
	pollerDone := make(chan struct{})
	go func() {
		defer close(pollerDone)
		d.runBatchPoller(pollerCtx, taskCtx, sem, wakeup, &taskWG)
	}()

	for {
		select {
		case <-ctx.Done():
			pollerCancel()

			<-pollerDone
			if !drainInFlightTasks(&taskWG, drainTimeoutFromEnv(), d.forceShutdown.Load(), taskCancel, d.logger) {
				leakTaskCtx = true
			}
			return ctx.Err()
		case <-runtimeSetCh:

			nudge()
		case <-taskWakeups:

			nudge()
		}
	}
}

func (d *Daemon) runBatchPoller(pollerCtx, parentCtx context.Context, sem chan int, wakeup chan struct{}, taskWG *sync.WaitGroup) {
	releaseSlots := func(slots []int) {
		for _, sl := range slots {
			sem <- sl
		}
	}

	for {
		if pollerCtx.Err() != nil {
			return
		}

		runtimeIDs := d.allRuntimeIDs()
		if len(runtimeIDs) == 0 {
			if err := sleepWithContextOrWakeup(pollerCtx, d.cfg.PollInterval, wakeup); err != nil {
				return
			}
			continue
		}

		slot, acquired, woke, err := waitForTaskSlot(pollerCtx, sem, wakeup, taskSlotWaitTimeout)
		if err != nil {
			return
		}
		if !acquired {
			if woke {
				continue
			}
			if err := sleepWithContextOrWakeup(pollerCtx, capacityBackoff(d.cfg.PollInterval), wakeup); err != nil {
				return
			}
			continue
		}
		slots := append([]int{slot}, drainAvailableSlots(sem, d.cfg.MaxConcurrentTasks-1)...)

		if !d.tryEnterClaim() {
			releaseSlots(slots)
			if err := sleepWithContextOrWakeup(pollerCtx, d.cfg.PollInterval, wakeup); err != nil {
				return
			}
			continue
		}

		tasks, err := d.ClaimTasksWSFirst(pollerCtx, d.cfg.DaemonID, runtimeIDs, len(slots))
		if err != nil {
			d.exitClaim()
			releaseSlots(slots)
			if pollerCtx.Err() == nil && !d.logTooOldHint(err) {
				d.logger.Warn("batch claim failed", "error", err)
			}
			if err := sleepWithContextOrWakeup(pollerCtx, d.cfg.PollInterval, wakeup); err != nil {
				return
			}
			continue
		}

		dispatched := 0
		for i := range tasks {
			if i >= len(slots) || tasks[i] == nil {
				break
			}
			t := *tasks[i]
			slot := slots[i]
			taskTarget := t.IssueID
			if taskTarget == "" && t.ChatSessionID != "" {
				taskTarget = "chat:" + shortID(t.ChatSessionID)
			}
			d.logger.Info("task received", "task", shortID(t.ID), "target", taskTarget)
			taskWG.Add(1)
			d.activeTasks.Add(1)
			go func(t Task, slot int) {
				defer taskWG.Done()
				defer d.activeTasks.Add(-1)
				defer func() {

					sem <- slot
					signalPollerWakeup(wakeup)
				}()
				d.handleTask(parentCtx, t, slot)
			}(t, slot)
			dispatched++
		}
		d.exitClaim()
		if dispatched < len(slots) {
			releaseSlots(slots[dispatched:])
		}

		if dispatched > 0 && dispatched == len(slots) {
			continue
		}
		if err := sleepWithContextOrWakeup(pollerCtx, d.cfg.PollInterval, wakeup); err != nil {
			return
		}
	}
}

func signalPollerWakeup(wakeup chan<- struct{}) {
	select {
	case wakeup <- struct{}{}:
	default:
	}
}

func drainAvailableSlots(sem chan int, max int) []int {
	if max <= 0 {
		return nil
	}
	var slots []int
	for len(slots) < max {
		select {
		case s := <-sem:
			slots = append(slots, s)
		default:
			return slots
		}
	}
	return slots
}

func capacityBackoff(pollInterval time.Duration) time.Duration {
	if pollInterval <= 0 || pollInterval > taskSlotCapacityBackoff {
		return taskSlotCapacityBackoff
	}
	return pollInterval
}

func waitForTaskSlot(ctx context.Context, sem chan int, wakeup <-chan struct{}, wait time.Duration) (slot int, acquired, woke bool, err error) {
	select {
	case slot = <-sem:
		return slot, true, false, nil
	case <-ctx.Done():
		return 0, false, false, ctx.Err()
	default:
	}

	if wait <= 0 {
		return 0, false, false, nil
	}

	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case slot = <-sem:
		return slot, true, false, nil
	case <-wakeup:
		return 0, false, true, nil
	case <-ctx.Done():
		return 0, false, false, ctx.Err()
	case <-timer.C:
		return 0, false, false, nil
	}
}

func newTaskSlotSemaphore(maxConcurrentTasks int) chan int {
	sem := make(chan int, maxConcurrentTasks)
	for i := 0; i < maxConcurrentTasks; i++ {
		sem <- i
	}
	return sem
}

func shouldInterruptAgent(status string, err error) bool {
	if err != nil {
		return isTaskNotFoundError(err)
	}
	return isAgentTaskTerminal(status)
}

func (d *Daemon) watchTaskCancellation(ctx context.Context, taskID string, pollInterval time.Duration, taskLog *slog.Logger) <-chan struct{} {
	cancelled := make(chan struct{})

	var reconcileCh <-chan struct{}
	if d.reconcile != nil {
		reconcileCh = d.reconcile.notify()
	}
	go func() {
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		check := func() bool {
			status, err := d.client.GetTaskStatus(ctx, taskID)
			if !shouldInterruptAgent(status, err) {
				return false
			}
			if err != nil {
				taskLog.Info("task gone server-side, interrupting agent", "error", err)
			} else {
				taskLog.Info("task reached terminal state server-side, interrupting agent", "status", status)
			}
			close(cancelled)
			return true
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-reconcileCh:

				if d.reconcile != nil {
					reconcileCh = d.reconcile.notify()
				}
				if check() {
					return
				}
			case <-ticker.C:
				if check() {
					return
				}
			}
		}
	}()
	return cancelled
}

func (d *Daemon) handleTask(ctx context.Context, task Task, slot int) {
	d.mu.Lock()
	rt := d.runtimeIndex[task.RuntimeID]
	d.mu.Unlock()
	provider := rt.Provider

	d.mcpPolicy.Store(task.McpPolicy.Policy())

	taskLog := d.logger.With("task", shortID(task.ID))
	agentName := "agent"
	if task.Agent != nil {
		agentName = task.Agent.Name
	}
	if task.ChatSessionID != "" {
		taskLog.Info("picked chat task", "chat_session", shortID(task.ChatSessionID), "agent", agentName, "provider", provider)
	} else {
		taskLog.Info("picked task", "issue", task.IssueID, "agent", agentName, "provider", provider)
	}
	taskLog.Debug("task context",
		"workspace_id", task.WorkspaceID,
		"runtime_id", task.RuntimeID,
		"agent_id", task.AgentID,
		"repos", len(task.Repos),
		"project_id", task.ProjectID,
		"autopilot_run_id", task.AutopilotRunID,
		"trigger_comment_id", task.TriggerCommentID,
		"resume_session", task.PriorSessionID != "",
		"reuse_workdir", task.PriorWorkDir != "",
	)

	localRelease, abort := d.acquireLocalDirectoryLockIfNeeded(ctx, task, taskLog)
	if abort {
		return
	}
	if localRelease != nil {
		defer localRelease()
	}

	predictedEnvRoot := execenv.PredictRootDir(d.cfg.WorkspacesRoot, task.WorkspaceID, task.ID)
	if predictedEnvRoot != "" {
		d.markActiveEnvRoot(predictedEnvRoot)
		defer d.unmarkActiveEnvRoot(predictedEnvRoot)
	}
	if task.PriorWorkDir != "" {
		if priorRoot := filepath.Dir(task.PriorWorkDir); priorRoot != "" && priorRoot != predictedEnvRoot {
			d.markActiveEnvRoot(priorRoot)
			defer d.unmarkActiveEnvRoot(priorRoot)
		}
	}

	runCtx, runCancel := context.WithCancel(ctx)
	defer runCancel()

	pollInterval := d.cancelPollInterval
	if pollInterval == 0 {
		pollInterval = 5 * time.Second
	}
	cancelledByPoll := d.watchTaskCancellation(runCtx, task.ID, pollInterval, taskLog)
	go func() {
		select {
		case <-cancelledByPoll:
			runCancel()
		case <-runCtx.Done():
		}
	}()

	result, err := d.runner.run(runCtx, task, provider, slot, taskLog)

	if len(result.Usage) > 0 {
		if usageErr := d.client.ReportTaskUsage(ctx, task.ID, result.Usage); usageErr != nil {
			taskLog.Warn("report task usage failed", "error", usageErr)
		}
	}

	select {
	case <-cancelledByPoll:
		taskLog.Info("task cancelled during execution, discarding result")

		if ackErr := d.client.AckTaskCancelled(ctx, task.ID); ackErr != nil {
			taskLog.Warn("cancel ack failed; server sweeper will finalize", "error", ackErr)
		}
		return
	default:
	}

	if err != nil {
		taskLog.Error("task failed", "error", err)

		if failErr := d.reportTerminalTask(ctx, terminalTaskReport{
			kind:          terminalTaskReportFail,
			taskID:        task.ID,
			errorMessage:  err.Error(),
			failureReason: taskRunFailureReason(err),
		}); failErr != nil {
			taskLog.Error("fail task callback failed", "error", failErr)
		}
		return
	}

	_ = d.client.ReportProgress(ctx, task.ID, "Finishing task", 2, 2)

	if status, err := d.client.GetTaskStatus(ctx, task.ID); shouldInterruptAgent(status, err) {
		taskLog.Info("task cancelled during execution, discarding result", "status", status, "error", err)

		if ackErr := d.client.AckTaskCancelled(ctx, task.ID); ackErr != nil {
			taskLog.Warn("cancel ack failed; server sweeper will finalize", "error", ackErr)
		}
		return
	}

	d.reportTaskResult(ctx, task.ID, result, taskLog)

	if result.EnvRoot != "" {
		if meta, ok := gcMetaForTask(task); ok {

			if assignment, _ := localDirectoryAssignmentForTask(task, d.cfg.DaemonID); assignment != nil {
				meta.LocalDirectory = true
			}
			if err := execenv.WriteGCMeta(result.EnvRoot, meta, taskLog); err != nil {
				taskLog.Warn("write gc meta failed (non-fatal)", "error", err)
			}
		}
	}
}

func taskRunFailureReason(err error) string {
	if errors.Is(err, errTaskPrepareTimeout) {
		return taskfailure.ReasonTimeout.String()
	}
	if errors.Is(err, execenv.ErrEnvRootBusy) {
		return taskfailure.ReasonEnvRootBusy.String()
	}

	if errors.Is(err, errSkillBundleUnavailable) {
		return taskfailure.ReasonSkillBundleUnavailable.String()
	}
	return taskfailure.Classify(err.Error()).String()
}

func (d *Daemon) acquireLocalDirectoryLockIfNeeded(ctx context.Context, task Task, taskLog *slog.Logger) (release func(), abort bool) {
	if len(task.ProjectResources) == 0 || d.cfg.DaemonID == "" {
		return nil, false
	}
	assignment, err := localDirectoryAssignmentForTask(task, d.cfg.DaemonID)
	if err != nil {
		taskLog.Error("local_directory: resolve resource failed", "error", err)
		if failErr := d.reportTerminalTask(ctx, terminalTaskReport{
			kind:          terminalTaskReportFail,
			taskID:        task.ID,
			errorMessage:  err.Error(),
			failureReason: "local_directory_error",
		}); failErr != nil {
			taskLog.Error("fail task after local_directory resolve error", "error", failErr)
		}
		return nil, true
	}
	if assignment == nil {
		return nil, false
	}
	taskLog = taskLog.With("local_directory", assignment.AbsPath)
	if err := validateLocalPath(assignment.AbsPath); err != nil {
		taskLog.Error("local_directory: path validation failed", "error", err)
		if failErr := d.reportTerminalTask(ctx, terminalTaskReport{
			kind:          terminalTaskReportFail,
			taskID:        task.ID,
			errorMessage:  err.Error(),
			failureReason: "local_directory_error",
		}); failErr != nil {
			taskLog.Error("fail task after local_directory validation error", "error", failErr)
		}
		return nil, true
	}

	waitCtx, waitCancel := context.WithCancel(ctx)
	defer waitCancel()
	pollInterval := d.cancelPollInterval
	if pollInterval == 0 {
		pollInterval = 5 * time.Second
	}
	var (
		watcherOnce      sync.Once
		prepareLeaseOnce sync.Once
		cancelledByPoll  <-chan struct{}
		stopPrepareLease func()
	)
	defer func() {
		if stopPrepareLease != nil {
			stopPrepareLease()
		}
	}()

	onWait := func(holder string) {
		reason := fmt.Sprintf("local_directory %s", assignment.AbsPath)
		if holder != "" {
			reason = fmt.Sprintf("%s (held by task %s)", reason, shortID(holder))
		}
		taskLog.Info("local_directory: waiting on path mutex", "holder", shortID(holder))
		if waitErr := d.client.MarkTaskWaitingLocalDirectory(ctx, task.ID, reason); waitErr != nil {

			taskLog.Warn("local_directory: mark waiting status failed", "error", waitErr)
		}
		prepareLeaseOnce.Do(func() {
			stopPrepareLease = d.startTaskPrepareLeaseExtender(waitCtx, task, taskLog)
		})

		watcherOnce.Do(func() {
			cancelledByPoll = d.watchTaskCancellation(waitCtx, task.ID, pollInterval, taskLog)
			go func() {
				select {
				case <-cancelledByPoll:
					waitCancel()
				case <-waitCtx.Done():
				}
			}()
		})
	}
	release, err = d.localPathLocks.Acquire(waitCtx, assignment.RealPath, task.ID, onWait)
	if err != nil {

		if cancelledByPoll != nil {
			select {
			case <-cancelledByPoll:
				taskLog.Info("local_directory: wait aborted by server-side terminal state")
				return nil, true
			default:
			}
		}
		taskLog.Error("local_directory: lock acquire failed", "error", err)
		failureReason := "local_directory_error"
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			failureReason = "cancelled"
		}
		if failErr := d.reportTerminalTask(ctx, terminalTaskReport{
			kind:          terminalTaskReportFail,
			taskID:        task.ID,
			errorMessage:  fmt.Sprintf("local_directory wait cancelled: %s", err.Error()),
			failureReason: failureReason,
		}); failErr != nil {
			taskLog.Error("fail task after local_directory lock cancel", "error", failErr)
		}
		return nil, true
	}
	taskLog.Info("local_directory: lock acquired")
	return release, false
}

func (d *Daemon) reportTaskResult(ctx context.Context, taskID string, result TaskResult, taskLog *slog.Logger) {
	switch result.Status {
	case "completed":
		taskLog.Info("task completed", "status", result.Status)
		err := d.reportTerminalTask(ctx, terminalTaskReport{
			kind:                  terminalTaskReportComplete,
			taskID:                taskID,
			output:                result.Comment,
			branchName:            result.BranchName,
			sessionID:             result.SessionID,
			workDir:               result.WorkDir,
			sessionRolloutMissing: result.SessionRolloutMissing,
		})
		if err == nil {
			return
		}

		if isTransientError(err) {
			taskLog.Error("complete task failed after retries; leaving task in running rather than falling back to fail", "error", err)
			return
		}
		taskLog.Error("complete task rejected by server, falling back to fail", "error", err)

		fallbackErrMsg := fmt.Sprintf("complete task failed: %s", err.Error())
		if failErr := d.reportTerminalTask(ctx, terminalTaskReport{
			kind:                  terminalTaskReportFail,
			taskID:                taskID,
			errorMessage:          fallbackErrMsg,
			sessionID:             result.SessionID,
			workDir:               result.WorkDir,
			failureReason:         taskfailure.Classify(fallbackErrMsg).String(),
			sessionRolloutMissing: result.SessionRolloutMissing,
		}); failErr != nil {
			taskLog.Error("fail task fallback also failed", "error", failErr)
		}
	default:
		failureReason := result.FailureReason
		if failureReason == "" {
			if result.Status == "cancelled" {

				failureReason = "cancelled"
			} else {

				failureReason = taskfailure.Classify(result.Comment).String()
			}
		}
		taskLog.Info("task did not complete, reporting failure", "status", result.Status, "failure_reason", failureReason)
		if err := d.reportTerminalTask(ctx, terminalTaskReport{
			kind:                  terminalTaskReportFail,
			taskID:                taskID,
			errorMessage:          result.Comment,
			sessionID:             result.SessionID,
			workDir:               result.WorkDir,
			failureReason:         failureReason,
			sessionRolloutMissing: result.SessionRolloutMissing,
		}); err != nil {
			taskLog.Error("report failed task failed", "error", err)
		}
	}
}

func (d *Daemon) reportTerminalTask(parentCtx context.Context, report terminalTaskReport) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parentCtx), terminalTaskReportTimeout)
	defer cancel()

	switch report.kind {
	case terminalTaskReportComplete:
		return d.client.CompleteTask(ctx, report.taskID, report.output, report.branchName, report.sessionID, report.workDir, report.sessionRolloutMissing)
	case terminalTaskReportFail:
		return d.client.FailTask(ctx, report.taskID, report.errorMessage, report.sessionID, report.workDir, report.failureReason, report.sessionRolloutMissing)
	default:
		return fmt.Errorf("unsupported terminal task report kind %d", report.kind)
	}
}

func gcMetaForTask(task Task) (execenv.GCMeta, bool) {
	meta := execenv.GCMeta{WorkspaceID: task.WorkspaceID}
	switch {
	case task.ChatSessionID != "":
		meta.Kind = execenv.GCKindChat
		meta.ChatSessionID = task.ChatSessionID
	case task.AutopilotRunID != "":
		meta.Kind = execenv.GCKindAutopilotRun
		meta.AutopilotRunID = task.AutopilotRunID
	case task.IssueID != "":
		meta.Kind = execenv.GCKindIssue
		meta.IssueID = task.IssueID
	case task.QuickCreatePrompt != "":

		meta.Kind = execenv.GCKindQuickCreate
		meta.TaskID = task.ID
	default:
		return execenv.GCMeta{}, false
	}
	return meta, true
}

func providerNeedsInlineSystemPrompt(provider string) bool {
	switch provider {
	case runtimeCodeN, runtimeCodeK, runtimeCodeR:
		return true
	default:
		return false
	}
}

func gateResumeToReusedWorkdir(task *Task, taskCtx *execenv.TaskContextForEnv, envWorkDir string, taskLog *slog.Logger) bool {
	reused := task.PriorWorkDir != "" && envWorkDir == task.PriorWorkDir
	if !reused && task.PriorSessionID != "" {
		taskLog.Info("dropping prior session: workdir not reused, per-cwd session cannot resolve",
			"session_id", task.PriorSessionID,
			"prior_workdir", task.PriorWorkDir,
			"workdir", envWorkDir,
		)
		task.PriorSessionID = ""
		taskCtx.PriorSessionResumed = false

		taskCtx.PriorSessionResumeUnavailable = true
		task.PriorSessionResumeUnavailable = true
	}
	return reused
}

func (d *Daemon) lockReusablePriorEnvRoot(task Task, localAssignment *localDirectoryAssignment, heldRoot string) (*execenv.EnvRootClaim, bool) {
	if !shouldReusePriorWorkdir(task, localAssignment, d.cfg.WorkspacesRoot) {
		return nil, false
	}
	priorRoot, err := filepath.EvalSymlinks(filepath.Dir(task.PriorWorkDir))
	if err != nil {
		return nil, false
	}
	canonicalWorkspacesRoot, err := filepath.EvalSymlinks(d.cfg.WorkspacesRoot)
	if err != nil {
		return nil, false
	}
	rel, err := filepath.Rel(canonicalWorkspacesRoot, priorRoot)
	if err != nil || !filepath.IsLocal(rel) {

		return nil, true
	}
	if heldRoot != "" {
		if held, err := filepath.EvalSymlinks(heldRoot); err == nil && held == priorRoot {

			return nil, true
		}
	}
	claim, err := execenv.LockEnvRootForReuse(priorRoot)
	switch {
	case errors.Is(err, execenv.ErrEnvRootBusy):
		d.logger.Info("prior workdir is in use by another execution; starting a fresh environment",
			"task", shortID(task.ID), "prior_root", filepath.Base(priorRoot))
		return nil, false
	case err != nil:
		d.logger.Warn("could not lock prior workdir; starting a fresh environment",
			"task", shortID(task.ID), "error", err)
		return nil, false
	case claim == nil:
		return nil, false
	}
	return claim, true
}

func shouldReusePriorWorkdir(task Task, localAssignment *localDirectoryAssignment, workspacesRoot string) bool {
	if task.PriorWorkDir == "" || localAssignment != nil {
		return false
	}
	if !task.IsLeaderTask {
		return true
	}

	root, err := filepath.EvalSymlinks(workspacesRoot)
	if err != nil {
		return false
	}
	workdir, err := filepath.EvalSymlinks(task.PriorWorkDir)
	if err != nil {
		return false
	}
	info, err := os.Stat(workdir)
	if err != nil || !info.IsDir() {
		return false
	}
	rel, err := filepath.Rel(root, workdir)
	if err != nil || !filepath.IsLocal(rel) {
		return false
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) != 3 || parts[0] != task.WorkspaceID || parts[1] == "" || parts[2] != "workdir" {
		return false
	}
	if task.AgentID == "" || task.IssueID == "" {
		return false
	}

	prov, err := execenv.ReadManagedEnvProvenance(filepath.Dir(workdir))
	if err != nil || prov.ManagedBy != execenv.ManagedEnvProvenanceManagedBy ||
		prov.WorkspaceID != task.WorkspaceID || prov.IssueID != task.IssueID ||
		prov.AgentID != task.AgentID {
		return false
	}

	data, err := os.ReadFile(filepath.Join(workdir, execenv.TaskContextMarkerRelPath))
	if err != nil {
		return false
	}
	var marker struct {
		ManagedBy string `json:"managed_by"`
		AgentID   string `json:"agent_id"`
		IssueID   string `json:"issue_id"`
	}
	if json.Unmarshal(data, &marker) != nil {
		return false
	}
	return marker.ManagedBy == execenv.TaskContextMarkerManagedBy &&
		marker.AgentID == task.AgentID && marker.IssueID == task.IssueID
}

func gateCodexResumeToRolloutPresence(task *Task, taskCtx *execenv.TaskContextForEnv, provider, codexHome string, taskLog *slog.Logger) {
	if provider != runtimeCodeE || task.PriorSessionID == "" || codexHome == "" {
		return
	}
	if execenv.CodexResumeRolloutPresent(codexHome, task.PriorSessionID) {
		return
	}
	taskLog.Warn("dropping prior codex session: rollout not present in task CODEX_HOME; starting a fresh thread",
		"session_id", task.PriorSessionID, "codex_home", codexHome)
	task.PriorSessionID = ""
	taskCtx.PriorSessionResumed = false

	taskCtx.PriorSessionResumeUnavailable = true
	task.PriorSessionResumeUnavailable = true
}

const (
	codexRolloutFlushWait    = 2 * time.Second
	codexRolloutPollInterval = 50 * time.Millisecond
)

func codexSessionResumable(codexHome, sessionID string, wait time.Duration) bool {
	if codexHome == "" || sessionID == "" {
		return true
	}
	deadline := time.Now().Add(wait)
	for {
		if execenv.CodexResumeRolloutPresent(codexHome, sessionID) {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(codexRolloutPollInterval)
	}
}

func waitCodexRolloutPresent(ctx context.Context, codexHome, sessionID string) bool {
	if codexHome == "" || sessionID == "" {
		return true
	}
	if execenv.CodexResumeRolloutPresent(codexHome, sessionID) {
		return true
	}
	ticker := time.NewTicker(codexRolloutPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():

			return execenv.CodexResumeRolloutPresent(codexHome, sessionID)
		case <-ticker.C:
			if execenv.CodexResumeRolloutPresent(codexHome, sessionID) {
				return true
			}
		}
	}
}

func (d *Daemon) ensureTaskSkillBundles(ctx context.Context, task *Task) error {
	if task == nil || task.Agent == nil || len(task.Agent.SkillRefs) == 0 {
		return nil
	}
	resolved := make(map[string]SkillData, len(task.Agent.SkillRefs))
	misses := make([]SkillRefData, 0)
	for _, ref := range task.Agent.SkillRefs {
		ref := ref
		var пакет SkillData
		if err := d.skillCache.WithRefLock(task.WorkspaceID, ref, func() error {
			if cached, ok := d.skillCache.Load(task.WorkspaceID, ref); ok {
				пакет = cached
				return nil
			}
			misses = append(misses, ref)
			return nil
		}); err != nil {
			return fmt.Errorf("load skill bundle cache: %w", err)
		}
		if пакет.ID != "" {
			resolved[skillRefKey(ref.Source, ref.ID)] = пакет
		}
	}

	for _, ref := range misses {
		started := time.Now()
		пакет, err := d.resolveSkillBundle(ctx, task, ref)
		if err != nil {

			return fmt.Errorf("%w: skill %q (id=%s, %d bytes) after %s: %w",
				errSkillBundleUnavailable, ref.Name, ref.ID, ref.SizeBytes,
				time.Since(started).Round(time.Millisecond), err)
		}
		resolved[skillRefKey(пакет.Source, пакет.ID)] = пакет
	}

	skills := make([]SkillData, 0, len(task.Agent.SkillRefs))
	for _, ref := range task.Agent.SkillRefs {
		пакет, ok := resolved[skillRefKey(ref.Source, ref.ID)]
		if !ok {
			return fmt.Errorf("skill bundle missing after resolve: skill_id=%s source=%s hash=%s", ref.ID, ref.Source, ref.Hash)
		}
		skills = append(skills, пакет)
	}
	task.Agent.Skills = skills
	return nil
}

func (d *Daemon) resolveSkillBundle(ctx context.Context, task *Task, ref SkillRefData) (SkillData, error) {
	reqCtx, cancel := context.WithTimeout(ctx, skillBundleResolveTimeout(ref.SizeBytes))
	defer cancel()

	пакет, err := d.client.ResolveSkillBundle(reqCtx, task.RuntimeID, task.ID, ref)
	if err != nil {
		return SkillData{}, err
	}

	if пакет.Source != ref.Source || пакет.ID != ref.ID {
		return SkillData{}, fmt.Errorf("resolve skill bundle returned wrong skill: requested source=%s id=%s, got source=%s id=%s", ref.Source, ref.ID, пакет.Source, пакет.ID)
	}
	bundleRef := skillRefFromBundle(пакет)
	if !validateSkillBundle(bundleRef, пакет) {
		return SkillData{}, fmt.Errorf("resolve skill bundle returned invalid bundle: skill_id=%s source=%s hash=%s", пакет.ID, пакет.Source, пакет.Hash)
	}
	if err := d.skillCache.WithRefLock(task.WorkspaceID, bundleRef, func() error {
		return d.skillCache.Store(task.WorkspaceID, пакет)
	}); err != nil {
		return SkillData{}, fmt.Errorf("store skill bundle cache: %w", err)
	}
	return пакет, nil
}

const (
	skillBundleResolveMinTimeout = 30 * time.Second

	skillBundleResolveMaxTimeout = 5 * time.Minute

	skillBundleResolveMinThroughput = 50 * 1024
)

func skillBundleResolveTimeout(sizeBytes int64) time.Duration {
	if sizeBytes <= 0 {
		return skillBundleResolveMinTimeout
	}
	scaled := time.Duration(sizeBytes/skillBundleResolveMinThroughput) * time.Second
	if scaled < skillBundleResolveMinTimeout {
		return skillBundleResolveMinTimeout
	}
	if scaled > skillBundleResolveMaxTimeout {
		return skillBundleResolveMaxTimeout
	}
	return scaled
}

func (d *Daemon) startTaskPrepareLeaseExtender(ctx context.Context, task Task, taskLog *slog.Logger) func() {
	leaseCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(taskPrepareLeaseRefresh)
		defer ticker.Stop()
		for {
			select {
			case <-leaseCtx.Done():
				return
			case <-ticker.C:
				reqCtx, reqCancel := context.WithTimeout(leaseCtx, taskPrepareLeaseTimeout)
				err := d.client.ExtendTaskPrepareLease(reqCtx, task.RuntimeID, task.ID)
				reqCancel()
				if err != nil {
					taskLog.Warn("extend task prepare lease failed", "error", err)
				}
			}
		}
	}()

	var once sync.Once
	return func() {
		once.Do(func() {
			cancel()
			<-done
		})
	}
}

func (d *Daemon) prepareExecutionEnvironment(ctx context.Context, params execenv.PrepareParams) (*execenv.Environment, error) {
	if d.executionEnvironmentCommand == nil {

		return execenv.Prepare(params, d.logger)
	}
	command, err := d.executionEnvironmentCommand()
	if err != nil {
		return nil, err
	}
	return execenv.PrepareIsolated(ctx, command, params, d.logger)
}

func (d *Daemon) reuseExecutionEnvironment(ctx context.Context, params execenv.ReuseParams) (*execenv.Environment, error) {
	if d.executionEnvironmentCommand == nil {
		return execenv.Reuse(params, d.logger), nil
	}
	command, err := d.executionEnvironmentCommand()
	if err != nil {
		return nil, err
	}
	return execenv.ReuseIsolated(ctx, command, params, d.logger)
}

func (d *Daemon) effectiveTaskPrepareTimeout() time.Duration {
	if d.taskPrepareTimeout > 0 {
		return d.taskPrepareTimeout
	}
	return defaultTaskPrepareTimeout
}

func skillRefKey(source, id string) string {
	return source + "\x00" + id
}

func skillRefFromBundle(пакет SkillData) SkillRefData {
	files := make([]skillbundle.File, 0, len(пакет.Files))
	for _, file := range пакет.Files {
		files = append(files, skillbundle.File{Path: file.Path, Content: file.Content})
	}
	manifest := skillbundle.BuildManifest(skillbundle.Skill{
		ID:          пакет.ID,
		Source:      пакет.Source,
		Name:        пакет.Name,
		Description: пакет.Description,
		Content:     пакет.Content,
		Files:       files,
	})
	fileRefs := make([]SkillFileRefData, 0, len(manifest.Files))
	for _, file := range manifest.Files {
		fileRefs = append(fileRefs, SkillFileRefData{Path: file.Path, SHA256: file.SHA256, SizeBytes: file.SizeBytes})
	}
	return SkillRefData{
		ID:        пакет.ID,
		Source:    пакет.Source,
		Hash:      manifest.Hash,
		SizeBytes: manifest.SizeBytes,
		FileCount: manifest.FileCount,
		Files:     fileRefs,
	}
}

func (d *Daemon) runTask(ctx context.Context, task Task, provider string, slot int, taskLog *slog.Logger) (taskResult TaskResult, returnErr error) {

	if task.WorkspaceID == "" {
		return TaskResult{}, fmt.Errorf("refusing to spawn agent: task has no workspace_id (task_id=%s)", task.ID)
	}

	prepareTimeout := d.effectiveTaskPrepareTimeout()
	prepareCtx, cancelPrepare := context.WithTimeoutCause(ctx, prepareTimeout, errTaskPrepareTimeout)
	prepareComplete := false
	defer func() {
		cancelPrepare()
		if prepareComplete || returnErr == nil || !errors.Is(context.Cause(prepareCtx), errTaskPrepareTimeout) {
			return
		}

		taskResult = TaskResult{}
		returnErr = fmt.Errorf("%w after %s", errTaskPrepareTimeout, prepareTimeout)
	}()

	d.registerTaskRepos(task.WorkspaceID, task.ID, task.Repos)
	defer d.clearTaskRepoRefs(task.WorkspaceID, task.ID)

	entry, ok := d.cfg.Agents[provider]

	var profileFixedArgs []string

	var resolvedVersion string
	if customSpec, isCustom := d.customProfileLaunchForRuntime(task.RuntimeID); isCustom {
		entry.Path = customSpec.path
		resolvedVersion = customSpec.version
		profileFixedArgs = customSpec.fixedArgs
		ok = true
		d.logger.Info("task uses custom runtime profile command",
			"task_id", task.ID, "runtime_id", task.RuntimeID,
			"provider", provider, "command_path", customSpec.path,
			"fixed_args", len(profileFixedArgs))
	} else if ok {

		entry, resolvedVersion = d.resolveAgentEntry(prepareCtx, provider, entry)
	}
	if !ok {
		return TaskResult{}, fmt.Errorf("no agent configured for provider %q", provider)
	}

	stopPrepareLease := d.startTaskPrepareLeaseExtender(prepareCtx, task, taskLog)
	defer stopPrepareLease()

	if err := d.ensureTaskSkillBundles(prepareCtx, &task); err != nil {
		return TaskResult{}, err
	}

	agentName := "agent"
	var agentID string
	var skills []SkillData
	var instructions string
	if task.Agent != nil {
		agentID = task.Agent.ID
		agentName = task.Agent.Name
		skills = task.Agent.Skills
		instructions = task.Agent.Instructions
	}

	taskCtx := execenv.TaskContextForEnv{
		IssueID:             task.IssueID,
		TriggerCommentID:    task.TriggerCommentID,
		TriggerThreadID:     task.TriggerThreadID,
		CommentReplyTargets: commentReplyThreads(task),
		NewCommentCount:     task.NewCommentCount,
		NewCommentsSince:    task.NewCommentsSince,
		PriorSessionResumed: task.PriorSessionID != "",

		PriorSessionResumeUnavailable:    task.PriorSessionResumeUnavailable,
		AgentID:                          agentID,
		AgentName:                        agentName,
		AgentInstructions:                instructions,
		AgentSkills:                      convertSkillsForEnv(skills),
		DisabledRuntimeSkills:            convertDisabledRuntimeSkillsForEnv(task.Agent, task.RuntimeID, provider),
		Repos:                            convertReposForEnv(task.Repos),
		ProjectID:                        task.ProjectID,
		ProjectTitle:                     task.ProjectTitle,
		ProjectDescription:               task.ProjectDescription,
		ProjectResources:                 convertProjectResourcesForEnv(task.ProjectResources),
		ChatSessionID:                    task.ChatSessionID,
		ChatChannelType:                  task.ChatChannelType,
		AutopilotRunID:                   task.AutopilotRunID,
		AutopilotID:                      task.AutopilotID,
		AutopilotTitle:                   task.AutopilotTitle,
		AutopilotDescription:             task.AutopilotDescription,
		AutopilotSource:                  task.AutopilotSource,
		AutopilotTriggerPayload:          strings.TrimSpace(string(task.AutopilotTriggerPayload)),
		QuickCreatePrompt:                task.QuickCreatePrompt,
		HandoffNote:                      task.HandoffNote,
		IsSquadLeader:                    strings.Contains(instructions, "## Squad Operating Protocol"),
		RequestingUserName:               task.RequestingUserName,
		RequestingUserProfileDescription: task.RequestingUserProfileDescription,
		InitiatorType:                    task.InitiatorType,
		InitiatorID:                      task.InitiatorID,
		InitiatorName:                    task.InitiatorName,
		InitiatorEmail:                   task.InitiatorEmail,
		WorkspaceContext:                 task.WorkspaceContext,
		ConnectedApps:                    task.ConnectedApps,
	}

	predictedRoot := execenv.PredictRootDir(d.cfg.WorkspacesRoot, task.WorkspaceID, task.ID)
	d.markActiveEnvRoot(predictedRoot)
	defer d.unmarkActiveEnvRoot(predictedRoot)
	if task.PriorWorkDir != "" {
		priorRoot := filepath.Dir(task.PriorWorkDir)
		if priorRoot != predictedRoot {
			d.markActiveEnvRoot(priorRoot)
			defer d.unmarkActiveEnvRoot(priorRoot)
		}
	}

	envClaim, err := execenv.ClaimEnvRoot(d.cfg.WorkspacesRoot, task.WorkspaceID, task.ID)
	if err != nil {
		return TaskResult{}, fmt.Errorf("claim execution environment: %w", err)
	}
	defer envClaim.Release()

	var env *execenv.Environment

	codexVersion := d.agentVersion(runtimeCodeE)
	if provider == runtimeCodeE && resolvedVersion != "" {
		codexVersion = resolvedVersion
	}
	openclawBin := ""
	if provider == runtimeCodeN {
		openclawBin = entry.Path
	}

	localAssignment, _ := localDirectoryAssignmentForTask(task, d.cfg.DaemonID)

	var agentMcpConfig json.RawMessage
	var effectiveMcpConfig json.RawMessage
	var cursorMcpAuthSource string
	if task.Agent != nil {
		agentMcpConfig = task.Agent.McpConfig
		effectiveMcpConfig = agentMcpConfig
		if merged, blocked, mergeErr := mergeRuntimeAndAgentMcpConfig(provider, agentMcpConfig, task.McpPolicy.Policy()); mergeErr != nil {
			taskLog.Warn("mcp_config: runtime merge failed; using agent configuration only",
				"provider", provider,
				"error", mergeErr,
			)
		} else {
			effectiveMcpConfig = merged
			for _, b := range blocked {

				taskLog.Warn("mcp_config: blocked local MCP server",
					"provider", provider, "server", b.Name, "source", "local", "reason", b.Reason)
			}
		}
		if provider == runtimeCodeG {
			cursorMcpAuthSource = strings.TrimSpace(task.Agent.CustomEnv[execenv.CursorMcpAuthSourceEnv])
		}
	}

	var openclawMode string
	var openclawGateway execenv.OpenclawGatewayPin
	if task.Agent != nil && provider == runtimeCodeN {
		openclawMode, openclawGateway = decodeOpenclawRuntimeConfig(task.Agent.RuntimeConfig, d.logger)
	}
	var agentEnvOverrides map[string]string
	var agentCustomArgs []string
	if task.Agent != nil {
		agentEnvOverrides = task.Agent.CustomEnv
		agentCustomArgs = task.Agent.CustomArgs
	}

	var codexSandboxArgs []string
	if provider == runtimeCodeE {
		extraArgs := append(append([]string{}, profileFixedArgs...), defaultArgsForProvider(d.cfg, provider)...)
		codexSandboxArgs = agent.NormalizeRuntimeELaunchArgs(extraArgs, agentCustomArgs, effectiveMcpConfig, d.logger)
	}

	var hermesSourceHome string
	var hermesSourceMustExist bool
	var hermesEnv map[string]string
	if provider == runtimeCodeJ {
		sel := agent.ParseRuntimeJProfileArgs(agentCustomArgs)
		res := execenv.ResolveHermesProfile(agentEnvOverrides[execenv.RuntimeJHomeEnv], sel.Name, sel.Found, sel.Inline)
		if res.Err != nil {
			return TaskResult{}, fmt.Errorf("resolve hermes profile: %w", res.Err)
		}
		hermesSourceHome = res.SourceHome
		hermesSourceMustExist = res.MustExist
		hermesEnv = sanitizeAgentEnv(agentEnvOverrides)
		if hermesEnv == nil {
			hermesEnv = map[string]string{}
		}
		hermesEnv[execenv.RuntimeJHomeEnv] = res.SourceHome
	}

	if provider == runtimeCodeE {
		if хранилище := execenv.CodexSessionStorePath(d.cfg.Profile, task.AgentID, task.IssueID); хранилище != "" {
			d.markActiveCodexStore(хранилище)
			defer d.unmarkActiveCodexStore(хранилище)
		}
	}
	if priorClaim, ok := d.lockReusablePriorEnvRoot(task, localAssignment, envClaim.RootDir()); ok {
		defer priorClaim.Release()
		var err error
		env, err = d.reuseExecutionEnvironment(prepareCtx, execenv.ReuseParams{
			WorkspacesRoot:        d.cfg.WorkspacesRoot,
			Profile:               d.cfg.Profile,
			WorkDir:               task.PriorWorkDir,
			Provider:              provider,
			CodexVersion:          codexVersion,
			ResumeSessionID:       task.PriorSessionID,
			OpenclawBin:           openclawBin,
			McpConfig:             effectiveMcpConfig,
			CursorMcpAuthSource:   cursorMcpAuthSource,
			OpenclawGateway:       openclawGateway,
			HermesSourceHome:      hermesSourceHome,
			HermesSourceMustExist: hermesSourceMustExist,
			HermesEnv:             hermesEnv,
			CodexCustomArgs:       codexSandboxArgs,
			Task:                  taskCtx,
		})
		if err != nil {
			return TaskResult{}, fmt.Errorf("reuse execution environment: %w", err)
		}
	}
	if env == nil {
		var err error
		prepParams := execenv.PrepareParams{
			WorkspacesRoot:        d.cfg.WorkspacesRoot,
			Profile:               d.cfg.Profile,
			WorkspaceID:           task.WorkspaceID,
			TaskID:                task.ID,
			AgentName:             agentName,
			Provider:              provider,
			CodexVersion:          codexVersion,
			OpenclawBin:           openclawBin,
			McpConfig:             effectiveMcpConfig,
			CursorMcpAuthSource:   cursorMcpAuthSource,
			OpenclawGateway:       openclawGateway,
			HermesSourceHome:      hermesSourceHome,
			HermesSourceMustExist: hermesSourceMustExist,
			HermesEnv:             hermesEnv,
			CodexCustomArgs:       codexSandboxArgs,
			Task:                  taskCtx,

			EnvRootPreclaimed: true,
		}
		if localAssignment != nil {
			prepParams.LocalWorkDir = localAssignment.AbsPath
		}
		env, err = d.prepareExecutionEnvironment(prepareCtx, prepParams)
		if err != nil {
			return TaskResult{}, fmt.Errorf("prepare execution environment: %w", err)
		}
	}

	if env.RootDir != predictedRoot && env.RootDir != "" {
		d.markActiveEnvRoot(env.RootDir)
		defer d.unmarkActiveEnvRoot(env.RootDir)
	}
	taskTempDir, err := ensureTaskTempDir(env.RootDir, task.WorkspaceID, task.ID)
	if err != nil {
		return TaskResult{}, fmt.Errorf("prepare task temp dir: %w", err)
	}
	defer func() {
		if cerr := os.RemoveAll(taskTempDir); cerr != nil {
			taskLog.Warn("task temp dir cleanup failed", "path", taskTempDir, "error", cerr)
		}
	}()

	if err := d.client.StartTask(prepareCtx, task.ID); err != nil {
		stopPrepareLease()
		return TaskResult{}, fmt.Errorf("start task failed: %w", err)
	}
	stopPrepareLease()
	prepareComplete = true
	cancelPrepare()
	_ = d.client.ReportProgress(ctx, task.ID, fmt.Sprintf("Launching %s", provider), 1, 2)

	reused := gateResumeToReusedWorkdir(&task, &taskCtx, env.WorkDir, taskLog)

	if reused {
		gateCodexResumeToRolloutPresence(&task, &taskCtx, provider, env.CodexHome, taskLog)
	}

	runtimeBrief, err := execenv.InjectRuntimeConfig(env.WorkDir, provider, taskCtx)
	if err != nil {
		d.logger.Warn("execenv: inject runtime config failed (non-fatal)", "error", err)
	}

	if env.LocalDirectory {
		defer func() {
			if cerr := execenv.CleanupRuntimeConfig(env.WorkDir, provider); cerr != nil {
				d.logger.Warn("execenv: cleanup runtime config failed (non-fatal)", "error", cerr)
			}

			if cerr := execenv.CleanupSidecars(env.RootDir); cerr != nil {
				d.logger.Warn("execenv: cleanup sidecars failed (non-fatal)", "error", cerr)
			}
		}()
	}

	prompt := BuildPrompt(task, provider)

	agentToken, err := taskScopedAuthToken(task)
	if err != nil {
		taskLog.Error("task auth token invalid; refusing to start agent", "error", err)
		return TaskResult{}, err
	}
	agentEnv := map[string]string{
		"GOOSAR_TOKEN":        agentToken,
		"GOOSAR_SERVER_URL":   d.cfg.ServerBaseURL,
		"GOOSAR_DAEMON_PORT":  fmt.Sprintf("%d", d.cfg.HealthPort),
		"GOOSAR_WORKSPACE_ID": task.WorkspaceID,
		"GOOSAR_AGENT_NAME":   agentName,
		"GOOSAR_AGENT_ID":     task.AgentID,
		"GOOSAR_TASK_ID":      task.ID,
		"GOOSAR_TASK_SLOT":    strconv.Itoa(slot),
		"TMPDIR":              taskTempDir,
		"TMP":                 taskTempDir,
		"TEMP":                taskTempDir,
	}
	if checkoutMode := repoCheckoutModeFor(provider, runtime.GOOS); checkoutMode != "" {
		agentEnv[repoCheckoutModeEnv] = checkoutMode
	}
	if task.AutopilotRunID != "" {
		agentEnv["GOOSAR_AUTOPILOT_RUN_ID"] = task.AutopilotRunID
	}
	if task.AutopilotID != "" {
		agentEnv["GOOSAR_AUTOPILOT_ID"] = task.AutopilotID
	}

	if task.QuickCreatePrompt != "" {
		agentEnv["GOOSAR_QUICK_CREATE_TASK_ID"] = task.ID
		if len(task.QuickCreateAttachmentIDs) > 0 {
			if raw, err := json.Marshal(task.QuickCreateAttachmentIDs); err == nil {
				agentEnv["GOOSAR_QUICK_CREATE_ATTACHMENT_IDS"] = string(raw)
			} else {
				taskLog.Warn("quick-create attachment ids: marshal failed; skipping env injection", "error", err)
			}
		}
	}

	if selfBin, err := resolveSelfExecutable(); err == nil {
		binDir := filepath.Dir(selfBin)
		agentEnv["PATH"] = binDir + string(os.PathListSeparator) + os.Getenv("PATH")
	}

	if env.CodexHome != "" {
		agentEnv["CODEX_HOME"] = env.CodexHome
	}

	if env.TaskHome != "" {
		for k, v := range execenv.TaskHomeEnv(env.TaskHome) {
			agentEnv[k] = v
		}
	}

	if env.CursorDataDir != "" {
		agentEnv["CURSOR_DATA_DIR"] = env.CursorDataDir
	}

	if env.OpenclawConfigPath != "" {
		agentEnv["OPENCLAW_CONFIG_PATH"] = env.OpenclawConfigPath
	}

	if rootsValue, ok := composeOpenclawIncludeRoots(env.OpenclawIncludeRoot, os.Getenv("OPENCLAW_INCLUDE_ROOTS")); ok {
		agentEnv["OPENCLAW_INCLUDE_ROOTS"] = rootsValue
	}

	var agentCustomEnv map[string]string
	if task.Agent != nil {
		agentCustomEnv = task.Agent.CustomEnv
	}
	layerCustomEnvAndHermesHome(agentEnv, agentCustomEnv, env.HermesHome, d.logger)
	if err := configureCodexTaskShellEnvironment(provider, env.CodexHome, os.Environ(), agentEnv, agentCustomEnv, d.logger); err != nil {
		return TaskResult{}, err
	}
	backend, err := agent.New(provider, agent.Config{
		ExecutablePath:  entry.Path,
		CLIVersion:      resolvedVersion,
		Env:             agentEnv,
		Logger:          d.logger,
		TaskID:          task.ID,
		RuntimeID:       task.RuntimeID,
		DaemonVersion:   d.cfg.CLIVersion,
		RuntimeEVersion: codexVersion,
	})
	if err != nil {
		return TaskResult{}, fmt.Errorf("create agent backend: %w", err)
	}

	taskLog.Info("starting agent",
		"provider", provider,
		"workdir", env.WorkDir,
		"model", entry.Model,
		"reused", reused,
	)
	if task.PriorSessionID != "" {
		taskLog.Info("resuming session", "session_id", task.PriorSessionID)
	}

	taskStart := time.Now()

	var customArgs []string
	extraArgs := defaultArgsForProvider(d.cfg, provider)
	if len(profileFixedArgs) > 0 {
		extraArgs = append(append([]string{}, profileFixedArgs...), extraArgs...)
	}
	var mcpConfig json.RawMessage
	if task.Agent != nil {
		customArgs = task.Agent.CustomArgs
		mcpConfig = effectiveMcpConfig
	}
	if provider == runtimeCodeJ {
		customArgs = hermesLaunchArgs(customArgs, env != nil && env.HermesHome != "")
	}

	model := ""
	if task.Agent != nil && task.Agent.Model != "" {
		model = task.Agent.Model
	}
	if model == "" {
		model = entry.Model
	}
	thinkingLevel := ""
	serviceTier := ""
	if task.Agent != nil {
		thinkingLevel = task.Agent.ThinkingLevel
		serviceTier = task.Agent.ServiceTier
	}

	if serviceTier != "" {
		ok, err := agent.ValidateServiceTier(ctx, provider, entry.Path, model, serviceTier)
		if err != nil {
			taskLog.Warn("service_tier: catalog lookup failed; passing through",
				"provider", provider,
				"model", model,
				"service_tier", serviceTier,
				"error", err,
			)
		} else if !ok {
			taskLog.Warn("service_tier: not valid for this (provider, model); skipping injection",
				"provider", provider,
				"model", model,
				"service_tier", serviceTier,
			)
			serviceTier = ""
		}
	}

	if thinkingLevel != "" {
		ok, err := agent.ValidateThinkingLevel(ctx, provider, entry.Path, model, thinkingLevel)
		if err != nil {
			taskLog.Warn("thinking_level: catalog lookup failed; passing through",
				"provider", provider,
				"model", model,
				"thinking_level", thinkingLevel,
				"error", err,
			)
		} else if !ok {
			taskLog.Warn("thinking_level: not valid for this (provider, model); skipping injection",
				"provider", provider,
				"model", model,
				"thinking_level", thinkingLevel,
			)
			thinkingLevel = ""
		}
	}
	var idleWatchdogTimeout time.Duration
	if provider == runtimeCodeM {
		idleWatchdogTimeout = d.cfg.OpenCodeIdleWatchdog
	}
	execOpts := agent.ExecOptions{
		Cwd:                       env.WorkDir,
		Model:                     model,
		ThreadName:                deriveTaskThreadName(task),
		Timeout:                   d.cfg.AgentTimeout,
		SemanticInactivityTimeout: d.cfg.RuntimeESemanticInactivityTimeout,
		IdleWatchdogTimeout:       idleWatchdogTimeout,
		HandshakeTimeout:          d.cfg.RuntimeEHandshakeTimeout,
		ResumeSessionID:           task.PriorSessionID,

		ResumeExpected:       task.PriorSessionID != "",
		ExtraArgs:            extraArgs,
		CustomArgs:           customArgs,
		McpConfig:            mcpConfig,
		ThinkingLevel:        thinkingLevel,
		ServiceTier:          serviceTier,
		RuntimeNMode:         openclawMode,
		RuntimeCSettingsPath: env.ClaudeSettingsPath,
	}

	if providerNeedsInlineSystemPrompt(provider) {
		execOpts.SystemPrompt = runtimeBrief
	}

	d.registerActiveRepoCheckoutTask(agentToken, activeRepoCheckoutTask{
		WorkspaceID: task.WorkspaceID,
		TaskID:      task.ID,
		AgentID:     task.AgentID,
		AgentName:   agentName,
		WorkDir:     env.WorkDir,
	})
	defer d.clearActiveRepoCheckoutTask(agentToken)

	taskLog.Debug("invoking backend",
		"provider", provider,
		"model", model,
		"prompt_bytes", len(prompt),
		"custom_args", len(customArgs),
		"extra_args", len(extraArgs),
		"mcp_config", len(mcpConfig) > 0,
		"inline_system_prompt", execOpts.SystemPrompt != "",
		"resume_session", execOpts.ResumeSessionID != "",
		"timeout", execOpts.Timeout,
		"idle_watchdog", execOpts.IdleWatchdogTimeout,
	)

	var msgSeq atomic.Int32
	result, tools, err := d.executeAndDrain(ctx, backend, prompt, execOpts, taskLog, task.ID, env.CodexHome, &msgSeq)
	if err != nil {
		return TaskResult{}, err
	}

	if shouldRetryWithFreshSession(result, task.PriorSessionID, tools, provider) {
		firstResult := result
		firstUsage := result.Usage
		firstTools := tools
		taskLog.Warn("session resume failed, retrying with fresh session", "error", result.Error)

		execOpts.ResumeSessionID = ""
		task.PriorSessionID = ""
		taskCtx.PriorSessionResumed = false
		if freshBrief, briefErr := execenv.InjectRuntimeConfig(env.WorkDir, provider, taskCtx); briefErr != nil {
			taskLog.Warn("execenv: re-inject cold runtime config for fresh retry failed (non-fatal)", "error", briefErr)
		} else {
			runtimeBrief = freshBrief
			if providerNeedsInlineSystemPrompt(provider) {
				execOpts.SystemPrompt = runtimeBrief
			}
		}
		freshPrompt := freshSessionRetryPrompt(BuildPrompt(task, provider))

		retryResult, retryTools, retryErr := d.executeAndDrain(ctx, backend, freshPrompt, execOpts, taskLog, task.ID, env.CodexHome, &msgSeq)
		if retryErr != nil {
			taskLog.Error("fresh session also failed to start; keeping the original poisoned result", "error", retryErr)
		} else if retryResult.Status != "completed" && retryResult.SessionID == "" {
			taskLog.Warn("fresh session retry also failed without establishing a new session; keeping the original poisoned result",
				"retry_status", retryResult.Status,
				"retry_error", retryResult.Error,
			)
		}

		result, tools = reconcileFreshRetryResult(firstResult, firstUsage, firstTools, retryResult, retryTools, retryErr)
	}

	elapsed := time.Since(taskStart).Round(time.Second)
	taskLog.Info("agent finished",
		"status", result.Status,
		"duration", elapsed.String(),
		"tools", tools,
	)
	taskLog.Debug("agent result detail",
		"status", result.Status,
		"output_bytes", len(result.Output),
		"session_id", result.SessionID,
		"models_with_usage", len(result.Usage),
		"agent_error", result.Error,
	)

	var usageEntries []TaskUsageEntry
	for model, u := range result.Usage {
		if u.InputTokens == 0 && u.OutputTokens == 0 && u.CacheReadTokens == 0 && u.CacheWriteTokens == 0 {
			continue
		}
		usageEntries = append(usageEntries, TaskUsageEntry{
			Provider:         provider,
			Model:            model,
			InputTokens:      u.InputTokens,
			OutputTokens:     u.OutputTokens,
			CacheReadTokens:  u.CacheReadTokens,
			CacheWriteTokens: u.CacheWriteTokens,
			CostUSDTicks:     u.CostUSDTicks,
		})
	}

	var sessionRolloutMissing bool
	if result.SessionID != "" && !codexSessionResumable(env.CodexHome, result.SessionID, codexRolloutFlushWait) {
		taskLog.Warn("codex session rollout not present in task CODEX_HOME; withholding resume pointer and flagging continuity gap",
			"session_id", result.SessionID, "codex_home", env.CodexHome, "status", result.Status)
		result.SessionID = ""
		sessionRolloutMissing = true
	}

	defer func() { taskResult.SessionRolloutMissing = sessionRolloutMissing }()

	switch result.Status {
	case "completed":
		if result.Output == "" {

			return TaskResult{
				Status:    "completed",
				Comment:   "",
				SessionID: result.SessionID,
				WorkDir:   env.WorkDir,
				EnvRoot:   env.RootDir,
				Usage:     usageEntries,
			}, nil
		}

		if reason, ok := classifyPoisonedOutput(result.Output); ok {
			taskLog.Warn("agent finished with poisoned fallback output, classifying as blocked",
				"failure_reason", reason,
			)
			return TaskResult{
				Status:        "blocked",
				Comment:       result.Output,
				SessionID:     result.SessionID,
				WorkDir:       env.WorkDir,
				EnvRoot:       env.RootDir,
				Usage:         usageEntries,
				FailureReason: reason,
			}, nil
		}
		return TaskResult{
			Status:    "completed",
			Comment:   result.Output,
			SessionID: result.SessionID,
			WorkDir:   env.WorkDir,
			EnvRoot:   env.RootDir,
			Usage:     usageEntries,
		}, nil
	case "timeout":

		comment := result.Error
		if comment == "" {
			comment = fmt.Sprintf("%s timed out after %s", provider, d.cfg.AgentTimeout)
		}
		failureReason := "timeout"
		if reason, ok := classifyResumeUnsafeTimeout(provider, comment); ok {
			taskLog.Warn("agent timed out with resume-unsafe session, classifying as blocked",
				"failure_reason", reason,
			)
			failureReason = reason
		}
		return TaskResult{
			Status:        "blocked",
			Comment:       comment,
			SessionID:     result.SessionID,
			WorkDir:       env.WorkDir,
			EnvRoot:       env.RootDir,
			FailureReason: failureReason,
			Usage:         usageEntries,
		}, nil
	case "idle_watchdog":

		comment := result.Error
		if comment == "" {
			comment = idleWatchdogReason(d.cfg.AgentIdleWatchdog)
		}
		return TaskResult{
			Status:        "blocked",
			Comment:       comment,
			SessionID:     result.SessionID,
			WorkDir:       env.WorkDir,
			EnvRoot:       env.RootDir,
			FailureReason: "idle_watchdog",
			Usage:         usageEntries,
		}, nil
	case "cancelled":

		return TaskResult{
			Status:    "cancelled",
			Comment:   "task cancelled by server",
			SessionID: result.SessionID,
			WorkDir:   env.WorkDir,
			EnvRoot:   env.RootDir,
			Usage:     usageEntries,
		}, nil
	default:
		errMsg := result.Error
		if errMsg == "" {
			errMsg = fmt.Sprintf("%s execution %s", provider, result.Status)
		}

		failureReason, _ := classifyPoisonedError(errMsg)
		if failureReason != "" {
			taskLog.Warn("agent failed with poisoned API error, classifying as blocked",
				"failure_reason", failureReason,
			)
		} else {

			failureReason = taskfailure.Classify(errMsg).String()
		}
		return TaskResult{
			Status:        "blocked",
			Comment:       errMsg,
			SessionID:     result.SessionID,
			WorkDir:       env.WorkDir,
			EnvRoot:       env.RootDir,
			Usage:         usageEntries,
			FailureReason: failureReason,
		}, nil
	}
}

func shouldRetryWithFreshSession(result agent.Result, priorSessionID string, tools int32, provider string) bool {
	if result.Status != "failed" || priorSessionID == "" || tools > 0 {
		return false
	}

	if result.ResumeRejected {
		return true
	}

	if !agent.ResumeRejectionUndetectable(provider) {
		return false
	}

	return result.SessionID == "" && freshSessionMayHelp(result.Error)
}

func reconcileFreshRetryResult(first agent.Result, firstUsage map[string]agent.TokenUsage, firstTools int32, retry agent.Result, retryTools int32, retryErr error) (agent.Result, int32) {
	switch {
	case retryErr != nil:
		first.Usage = firstUsage
		return first, firstTools
	case retry.SessionID != "":
		retry.Usage = mergeUsage(firstUsage, retry.Usage)
		return retry, retryTools
	case retry.Status == "completed":
		retry.Usage = mergeUsage(firstUsage, retry.Usage)
		return retry, retryTools
	default:
		first.Usage = mergeUsage(firstUsage, retry.Usage)
		return first, firstTools
	}
}

func freshSessionMayHelp(errText string) bool {
	switch taskfailure.Classify(errText) {
	case taskfailure.ReasonAgentProviderNetwork,
		taskfailure.ReasonAgentProviderCapacityOrRateLimit,
		taskfailure.ReasonAgentProviderQuotaLimit,
		taskfailure.ReasonAgentProviderServerError,
		taskfailure.ReasonAgentProviderAuthOrAccess,
		taskfailure.ReasonAgentMissingConfig,
		taskfailure.ReasonAgentModelNotFoundOrUnavailable,
		taskfailure.ReasonAgentRuntimeMissingExecutable,
		taskfailure.ReasonAgentRuntimeVersionUnsupported,

		taskfailure.ReasonAgentTimeout:
		return false
	default:
		return true
	}
}

func (d *Daemon) executeAndDrain(ctx context.Context, backend agent.Backend, prompt string, opts agent.ExecOptions, taskLog *slog.Logger, taskID, codexHome string, msgSeq *atomic.Int32) (agent.Result, int32, error) {

	agentCtx, agentCancel := context.WithCancel(ctx)
	defer agentCancel()

	session, err := backend.Execute(agentCtx, prompt, opts)
	if err != nil {
		taskLog.Debug("backend execute returned error", "error", err)
		return agent.Result{}, 0, err
	}
	taskLog.Debug("backend started, draining messages")

	var drainCtx context.Context
	var drainCancel context.CancelFunc
	if opts.Timeout > 0 {
		drainCtx, drainCancel = context.WithTimeout(agentCtx, opts.Timeout+30*time.Second)
	} else {
		drainCtx, drainCancel = context.WithCancel(agentCtx)
	}
	defer drainCancel()

	var toolCount atomic.Int32

	var lastActivityAt atomic.Int64
	lastActivityAt.Store(time.Now().UnixNano())

	var inFlightTools atomic.Int32
	var idleWatchdogFired atomic.Bool

	idleWindow := d.cfg.AgentIdleWatchdog

	if idleWindow > 0 && opts.IdleWatchdogTimeout > 0 && opts.IdleWatchdogTimeout < idleWindow {
		idleWindow = opts.IdleWatchdogTimeout
	}
	var idleWatchdogThreshold atomic.Int64
	idleWatchdogThreshold.Store(int64(idleWindow))
	if idleWindow > 0 {
		go d.runIdleWatchdog(agentCtx, idleWindow, d.cfg.AgentToolWatchdog, &lastActivityAt, &inFlightTools, &idleWatchdogFired, &idleWatchdogThreshold, agentCancel, session.Messages, taskLog, taskID)
	}

	drainFinished := make(chan struct{})
	go func() {
		defer close(drainFinished)
		var mu sync.Mutex
		var pendingText strings.Builder
		var pendingThinking strings.Builder
		var batch []TaskMessageData
		callIDToTool := map[string]string{}

		flush := func() {
			mu.Lock()
			if pendingThinking.Len() > 0 {
				s := msgSeq.Add(1)
				batch = append(batch, TaskMessageData{
					Seq:     int(s),
					Type:    "thinking",
					Content: pendingThinking.String(),
				})
				pendingThinking.Reset()
			}
			if pendingText.Len() > 0 {
				s := msgSeq.Add(1)
				batch = append(batch, TaskMessageData{
					Seq:     int(s),
					Type:    "text",
					Content: pendingText.String(),
				})
				pendingText.Reset()
			}
			toSend := batch
			batch = nil
			mu.Unlock()

			if len(toSend) > 0 {
				sendCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				if err := d.client.ReportTaskMessages(sendCtx, taskID, toSend); err != nil {
					taskLog.Debug("failed to report task messages", "error", err)
				} else {
					taskLog.Debug("reported task messages", "count", len(toSend), "last_seq", toSend[len(toSend)-1].Seq)
				}
				cancel()
			}
		}

		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		done := make(chan struct{})
		tickerDone := make(chan struct{})
		go func() {
			defer close(tickerDone)
			for {
				select {
				case <-ticker.C:
					flush()
				case <-done:
					return
				}
			}
		}()

		var sessionPinned atomic.Bool
		for {
			select {
			case сообщение, ok := <-session.Messages:
				if !ok {
					goto drainDone
				}

				lastActivityAt.Store(time.Now().UnixNano())
				switch сообщение.Type {
				case agent.MessageStatus:

					if сообщение.SessionID != "" && !sessionPinned.Swap(true) {
						sid := сообщение.SessionID
						wd := opts.Cwd
						go func() {
							if !waitCodexRolloutPresent(drainCtx, codexHome, sid) {
								taskLog.Debug("skip pinning codex session: rollout not present before run ended",
									"session_id", sid, "codex_home", codexHome)
								return
							}
							pinCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
							defer cancel()
							if err := d.client.PinTaskSession(pinCtx, taskID, sid, wd); err != nil {
								taskLog.Debug("pin session failed", "error", err)
							}
						}()
					}
				case agent.MessageToolUse:
					n := toolCount.Add(1)
					inFlightTools.Add(1)
					taskLog.Info(fmt.Sprintf("tool #%d: %s", n, сообщение.Tool))
					if сообщение.CallID != "" {
						mu.Lock()
						callIDToTool[сообщение.CallID] = сообщение.Tool
						mu.Unlock()
					}
					s := msgSeq.Add(1)
					mu.Lock()
					batch = append(batch, TaskMessageData{
						Seq:   int(s),
						Type:  "tool_use",
						Tool:  сообщение.Tool,
						Input: сообщение.Input,
					})
					mu.Unlock()
				case agent.MessageToolResult:

					for {
						cur := inFlightTools.Load()
						if cur <= 0 {
							break
						}
						if inFlightTools.CompareAndSwap(cur, cur-1) {
							break
						}
					}
					s := msgSeq.Add(1)
					output := сообщение.Output
					if len(output) > 8192 {
						output = output[:8192]
					}
					toolName := сообщение.Tool
					if toolName == "" && сообщение.CallID != "" {
						mu.Lock()
						toolName = callIDToTool[сообщение.CallID]
						mu.Unlock()
					}
					taskLog.Info("tool_result observed", "seq", s, "tool", toolName, "call_id", сообщение.CallID)

					output = wrapUntrustedToolOutput(toolName, output)
					mu.Lock()
					batch = append(batch, TaskMessageData{
						Seq:    int(s),
						Type:   "tool_result",
						Tool:   toolName,
						Output: output,
					})
					mu.Unlock()
				case agent.MessageThinking:
					if сообщение.Content != "" {
						mu.Lock()
						pendingThinking.WriteString(сообщение.Content)
						mu.Unlock()
					}
				case agent.MessageText:
					if сообщение.Content != "" {
						taskLog.Debug("agent", "text", truncateLog(сообщение.Content, 200))
						mu.Lock()
						pendingText.WriteString(сообщение.Content)
						mu.Unlock()
					}
				case agent.MessageError:
					taskLog.Error("agent error", "content", сообщение.Content)
					s := msgSeq.Add(1)
					mu.Lock()
					batch = append(batch, TaskMessageData{
						Seq:     int(s),
						Type:    "error",
						Content: сообщение.Content,
					})
					mu.Unlock()
				}
			case <-drainCtx.Done():
				goto drainDone
			}
		}
	drainDone:
		close(done)

		<-tickerDone
		flush()
	}()

	waitForDrain := func() {
		select {
		case <-drainFinished:
		case <-time.After(10 * time.Second):
			drainCancel()
			select {
			case <-drainFinished:
			case <-time.After(12 * time.Second):
				taskLog.Warn("transcript drain did not stop after cancel; completing anyway")
			}
		}
	}

	select {
	case result := <-session.Result:
		waitForDrain()
		if idleWatchdogFired.Load() {

			result.Status = "idle_watchdog"
			if result.Error == "" {
				result.Error = idleWatchdogReason(time.Duration(idleWatchdogThreshold.Load()))
			}
		}
		return result, toolCount.Load(), nil
	case <-drainCtx.Done():

		waitForDrain()

		if idleWatchdogFired.Load() {
			return agent.Result{
				Status: "idle_watchdog",
				Error:  idleWatchdogReason(time.Duration(idleWatchdogThreshold.Load())),
			}, toolCount.Load(), nil
		}

		if errors.Is(drainCtx.Err(), context.Canceled) {
			return agent.Result{
				Status: "cancelled",
				Error:  "task cancelled by upstream context (server cancel or daemon shutdown)",
			}, toolCount.Load(), nil
		}
		return agent.Result{
			Status: "timeout",
			Error:  "agent did not produce result within drain timeout",
		}, toolCount.Load(), nil
	}
}

func idleWatchdogReason(window time.Duration) string {
	return fmt.Sprintf("agent produced no new messages for %s and message queue was empty; force-stopped by idle watchdog", window)
}

func (d *Daemon) runIdleWatchdog(agentCtx context.Context, window, toolWindow time.Duration, lastActivityAt *atomic.Int64, inFlightTools *atomic.Int32, fired *atomic.Bool, firedThreshold *atomic.Int64, cancel context.CancelFunc, messages <-chan agent.Message, taskLog *slog.Logger, taskID string) {
	interval := window / 2
	if window >= time.Minute && interval < 30*time.Second {
		interval = 30 * time.Second
	}
	if interval <= 0 {
		interval = window
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-agentCtx.Done():
			return
		case <-ticker.C:

			threshold := window
			toolInFlight := inFlightTools.Load() > 0
			if toolInFlight {
				if toolWindow <= 0 {
					continue
				}
				threshold = toolWindow
			}
			last := time.Unix(0, lastActivityAt.Load())
			idleFor := time.Since(last)
			if idleFor < threshold {
				continue
			}

			if len(messages) > 0 {
				continue
			}
			taskLog.Warn("idle watchdog firing: no agent activity, force-stopping run",
				"task", shortID(taskID),
				"idle_for", idleFor.Round(time.Second).String(),
				"threshold", threshold.String(),
				"tool_in_flight", toolInFlight,
			)
			firedThreshold.Store(int64(threshold))
			fired.Store(true)
			cancel()
			return
		}
	}
}

func mergeUsage(a, b map[string]agent.TokenUsage) map[string]agent.TokenUsage {
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}
	merged := make(map[string]agent.TokenUsage, len(a)+len(b))
	for model, u := range a {
		merged[model] = u
	}
	for model, u := range b {
		existing := merged[model]
		existing.InputTokens += u.InputTokens
		existing.OutputTokens += u.OutputTokens
		existing.CacheReadTokens += u.CacheReadTokens
		existing.CacheWriteTokens += u.CacheWriteTokens
		existing.CostUSDTicks += u.CostUSDTicks
		merged[model] = existing
	}
	return merged
}

func repoDataToInfo(repos []RepoData) []repocache.RepoInfo {
	info := make([]repocache.RepoInfo, len(repos))
	for i, r := range repos {
		info[i] = repocache.RepoInfo{URL: r.URL}
	}
	return info
}

func convertReposForEnv(repos []RepoData) []execenv.RepoContextForEnv {
	if len(repos) == 0 {
		return nil
	}
	result := make([]execenv.RepoContextForEnv, len(repos))
	for i, r := range repos {
		result[i] = execenv.RepoContextForEnv{URL: r.URL, Description: r.Description, Ref: r.Ref}
	}
	return result
}

func convertProjectResourcesForEnv(resources []ProjectResourceData) []execenv.ProjectResourceForEnv {
	if len(resources) == 0 {
		return nil
	}
	result := make([]execenv.ProjectResourceForEnv, len(resources))
	for i, r := range resources {
		result[i] = execenv.ProjectResourceForEnv{
			ID:           r.ID,
			ResourceType: r.ResourceType,
			ResourceRef:  r.ResourceRef,
			Label:        r.Label,
		}
	}
	return result
}

func (d *Daemon) markActiveEnvRoot(envRoot string) {
	if envRoot == "" {
		return
	}
	d.activeEnvRootsMu.Lock()
	defer d.activeEnvRootsMu.Unlock()
	d.ensureActiveEnvRootStateLocked()
	for d.deletingEnvRoots[envRoot] {
		d.activeEnvRootsCond.Wait()
	}
	d.activeEnvRoots[envRoot]++
}

func (d *Daemon) unmarkActiveEnvRoot(envRoot string) {
	if envRoot == "" {
		return
	}
	d.activeEnvRootsMu.Lock()
	defer d.activeEnvRootsMu.Unlock()
	d.ensureActiveEnvRootStateLocked()
	if d.activeEnvRoots[envRoot] <= 1 {
		delete(d.activeEnvRoots, envRoot)
		return
	}
	d.activeEnvRoots[envRoot]--
}

func (d *Daemon) isActiveEnvRoot(envRoot string) bool {
	d.activeEnvRootsMu.Lock()
	defer d.activeEnvRootsMu.Unlock()
	d.ensureActiveEnvRootStateLocked()
	return d.activeEnvRoots[envRoot] > 0
}

func (d *Daemon) ensureActiveEnvRootStateLocked() {
	if d.activeEnvRoots == nil {
		d.activeEnvRoots = make(map[string]int)
	}
	if d.deletingEnvRoots == nil {
		d.deletingEnvRoots = make(map[string]bool)
	}
	if d.activeEnvRootsCond == nil {
		d.activeEnvRootsCond = sync.NewCond(&d.activeEnvRootsMu)
	}
}

func (d *Daemon) reserveEnvRootForGC(envRoot string) (release func(), ok bool) {
	if envRoot == "" {
		return nil, false
	}
	d.activeEnvRootsMu.Lock()
	defer d.activeEnvRootsMu.Unlock()
	d.ensureActiveEnvRootStateLocked()
	if d.activeEnvRoots[envRoot] > 0 || d.deletingEnvRoots[envRoot] {
		return nil, false
	}
	d.deletingEnvRoots[envRoot] = true
	return func() {
		d.activeEnvRootsMu.Lock()
		delete(d.deletingEnvRoots, envRoot)
		d.activeEnvRootsCond.Broadcast()
		d.activeEnvRootsMu.Unlock()
	}, true
}

func (d *Daemon) markActiveCodexStore(хранилище string) {
	if хранилище == "" {
		return
	}
	d.activeCodexStoresMu.Lock()
	defer d.activeCodexStoresMu.Unlock()
	for d.deletingCodexStores[хранилище] {
		d.activeCodexStoresCond.Wait()
	}
	d.activeCodexStores[хранилище]++
}

func (d *Daemon) unmarkActiveCodexStore(хранилище string) {
	if хранилище == "" {
		return
	}
	d.activeCodexStoresMu.Lock()
	defer d.activeCodexStoresMu.Unlock()
	if d.activeCodexStores[хранилище] <= 1 {
		delete(d.activeCodexStores, хранилище)
		return
	}
	d.activeCodexStores[хранилище]--
}

func (d *Daemon) reserveCodexStoreForDeletion(хранилище string) (commit func(), ok bool) {
	d.activeCodexStoresMu.Lock()
	defer d.activeCodexStoresMu.Unlock()
	if d.activeCodexStores[хранилище] > 0 || d.deletingCodexStores[хранилище] {
		return nil, false
	}
	d.deletingCodexStores[хранилище] = true
	return func() {
		d.activeCodexStoresMu.Lock()
		delete(d.deletingCodexStores, хранилище)
		d.activeCodexStoresCond.Broadcast()
		d.activeCodexStoresMu.Unlock()
	}, true
}

func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

func truncateLog(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "…"
}

func convertSkillsForEnv(skills []SkillData) []execenv.SkillContextForEnv {
	if len(skills) == 0 {
		return nil
	}
	result := make([]execenv.SkillContextForEnv, len(skills))
	for i, s := range skills {
		result[i] = execenv.SkillContextForEnv{
			Name:        s.Name,
			Description: s.Description,
			Content:     s.Content,
		}
		for _, f := range s.Files {
			result[i].Files = append(result[i].Files, execenv.SkillFileContextForEnv{
				Path:    f.Path,
				Content: f.Content,
			})
		}
	}
	return result
}

func convertDisabledRuntimeSkillsForEnv(agentData *AgentData, runtimeID, provider string) []execenv.RuntimeSkillRefForEnv {
	if agentData == nil || len(agentData.DisabledRuntimeSkills) == 0 {
		return nil
	}
	result := make([]execenv.RuntimeSkillRefForEnv, 0, len(agentData.DisabledRuntimeSkills))
	for _, skill := range agentData.DisabledRuntimeSkills {
		if skill.RuntimeID != runtimeID || skill.Provider != provider {
			continue
		}
		result = append(result, execenv.RuntimeSkillRefForEnv{
			Root:   skill.Root,
			Key:    skill.Key,
			Name:   skill.Name,
			Plugin: skill.Plugin,
		})
	}
	return result
}

func composeOpenclawIncludeRoots(addRoot, userValue string) (string, bool) {
	if addRoot == "" {
		return "", false
	}
	parts := []string{addRoot}
	seen := map[string]struct{}{addRoot: {}}
	for _, p := range strings.Split(userValue, string(os.PathListSeparator)) {
		if p == "" {
			continue
		}
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		parts = append(parts, p)
	}
	return strings.Join(parts, string(os.PathListSeparator)), true
}

func ensureTaskTempDir(envRoot string, workspaceID string, taskID string) (string, error) {
	envRoot = strings.TrimSpace(envRoot)
	if envRoot == "" {
		return "", errors.New("env root is empty")
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return "", errors.New("workspace id is empty")
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return "", errors.New("task id is empty")
	}
	dir, err := os.MkdirTemp(socketSafeTempBaseDir(), "goosar-task-")
	if err != nil {
		return "", err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func socketSafeTempBaseDir() string {
	if os.PathSeparator == '/' {
		if info, err := os.Stat("/tmp"); err == nil && info.IsDir() {
			return "/tmp"
		}
	}
	return os.TempDir()
}

func isBlockedEnvKey(key string) bool {
	upper := strings.ToUpper(key)
	if strings.HasPrefix(upper, "GOOSAR_") {
		return true
	}
	switch upper {

	case "HOME", "PATH", "USER", "SHELL", "TERM", "TMPDIR", "TMP", "TEMP", "CODEX_HOME", "CURSOR_DATA_DIR", execenv.CursorMcpAuthSourceEnv, "HERMES_CONFIG_PATH", "OPENCLAW_CONFIG_PATH", "OPENCLAW_INCLUDE_ROOTS":
		return true

	case "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "ALL_PROXY",
		"REQUESTS_CA_BUNDLE", "SSL_CERT_FILE", "NODE_EXTRA_CA_CERTS", "GOOSAR_CA_FILE":
		return true

	case "LD_PRELOAD", "LD_AUDIT", "LD_LIBRARY_PATH", "LD_BIND_NOW",
		"DYLD_INSERT_LIBRARIES", "DYLD_LIBRARY_PATH", "DYLD_FRAMEWORK_PATH", "DYLD_FALLBACK_LIBRARY_PATH",
		"NODE_OPTIONS", "BASH_ENV", "ENV", "PYTHONSTARTUP", "PYTHONPATH", "PERL5OPT", "RUBYOPT",
		"GIT_SSH_COMMAND", "GIT_SSH", "GIT_EXTERNAL_DIFF", "GIT_PAGER", "GIT_CONFIG_GLOBAL",
		"ANTHROPIC_BASE_URL", "ANTHROPIC_API_URL", "OPENAI_BASE_URL", "OPENAI_API_BASE":
		return true
	}
	return false
}

func sanitizeAgentEnv(customEnv map[string]string) map[string]string {
	if len(customEnv) == 0 {
		return nil
	}
	out := make(map[string]string, len(customEnv))
	for k, v := range customEnv {
		if isBlockedEnvKey(k) {
			continue
		}
		out[k] = v
	}
	return out
}

func hermesLaunchArgs(customArgs []string, overlayActive bool) []string {
	if !overlayActive {
		return customArgs
	}

	sel := agent.ParseRuntimeJProfileArgs(customArgs)
	return agent.StripRuntimeJProfileArgs(customArgs, sel)
}

func layerCustomEnvAndHermesHome(agentEnv, customEnv map[string]string, overlayHome string, logger *slog.Logger) {
	for k, v := range customEnv {
		if isBlockedEnvKey(k) {
			if logger != nil {
				logger.Warn("custom_env: blocked key skipped", "key", k)
			}
			continue
		}
		agentEnv[k] = v
	}
	if overlayHome != "" {
		agentEnv[execenv.RuntimeJHomeEnv] = overlayHome
	}
}

func codexShellAuthorizedCustomEnvNames(customEnv map[string]string) []string {
	names := make([]string, 0, len(customEnv))
	for key := range customEnv {
		if key == "" || isBlockedEnvKey(key) {
			continue
		}
		names = append(names, key)
	}
	return names
}

func configureCodexTaskShellEnvironment(provider, codexHome string, inherited []string, agentEnv, agentCustomEnv map[string]string, logger *slog.Logger) error {
	if provider != runtimeCodeE {
		return nil
	}
	if strings.TrimSpace(codexHome) == "" {
		return errors.New("configure Codex shell environment: task CODEX_HOME is missing")
	}
	authorizedExplicit := codexShellAuthorizedCustomEnvNames(agentCustomEnv)
	includeOnly := execenv.CodexShellEnvAllowlist(inherited, agentEnv, authorizedExplicit)
	configPath := filepath.Join(codexHome, "config.toml")
	if err := execenv.EnsureCodexShellEnvPolicyConfig(configPath, includeOnly, logger); err != nil {
		return fmt.Errorf("configure Codex shell environment: %w", err)
	}
	return nil
}

func defaultArgsForProvider(cfg Config, provider string) []string {
	var args []string
	switch provider {
	case runtimeCodeC:
		args = cfg.RuntimeCArgs
	case runtimeCodeD:
		args = cfg.RuntimeDArgs
	case runtimeCodeE:
		args = cfg.RuntimeEArgs
	case runtimeCodeQ:
		args = cfg.RuntimeQArgs
	default:
		return nil
	}
	return append([]string(nil), args...)
}
