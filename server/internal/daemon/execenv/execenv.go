// Пакет execenv управляет изолированными окружениями выполнения задач демона.
// Каждая задача получает свой каталог с внедрёнными контекстными файлами;
// репозитории агент выкачивает по требованию через goosar repo checkout.
package execenv

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/adanman/goosar/server/internal/runtimeapps"
)

type RepoContextForEnv struct {
	URL         string
	Description string
	Ref         string
}

type ProjectResourceForEnv struct {
	ID           string          `json:"id"`
	ResourceType string          `json:"resource_type"`
	ResourceRef  json.RawMessage `json:"resource_ref"`
	Label        string          `json:"label,omitempty"`
}

type PrepareParams struct {
	WorkspacesRoot string
	WorkspaceID    string
	TaskID         string
	AgentName      string

	EnvRootPreclaimed bool

	Profile      string
	Provider     string
	CodexVersion string
	OpenclawBin  string

	McpConfig json.RawMessage

	CursorMcpAuthSource string

	OpenclawGateway OpenclawGatewayPin

	LocalWorkDir string

	HermesSourceHome string

	HermesSourceMustExist bool

	HermesEnv map[string]string

	CodexCustomArgs []string
	Task            TaskContextForEnv
}

type TaskContextForEnv struct {
	IssueID          string
	TriggerCommentID string
	TriggerThreadID  string

	CommentReplyTargets []ThreadReplyTarget
	NewCommentCount     int
	NewCommentsSince    string
	PriorSessionResumed bool

	PriorSessionResumeUnavailable bool
	AgentID                       string
	AgentName                     string
	AgentInstructions             string
	AgentSkills                   []SkillContextForEnv
	DisabledRuntimeSkills         []RuntimeSkillRefForEnv
	Repos                         []RepoContextForEnv
	ProjectID                     string
	ProjectTitle                  string
	ProjectDescription            string
	ProjectResources              []ProjectResourceForEnv
	ChatSessionID                 string

	ChatChannelType         string
	AutopilotRunID          string
	AutopilotID             string
	AutopilotTitle          string
	AutopilotDescription    string
	AutopilotSource         string
	AutopilotTriggerPayload string
	QuickCreatePrompt       string
	HandoffNote             string
	IsSquadLeader           bool

	WorkspaceContext string

	ConnectedApps []runtimeapps.ConnectedApp

	RequestingUserName               string
	RequestingUserProfileDescription string

	InitiatorType  string
	InitiatorID    string
	InitiatorName  string
	InitiatorEmail string
}

type SkillContextForEnv struct {
	Name        string
	Description string
	Content     string
	Files       []SkillFileContextForEnv
}

type SkillFileContextForEnv struct {
	Path    string
	Content string
}

type Environment struct {
	RootDir string

	WorkDir string

	LocalDirectory bool

	lockFile *os.File

	CodexHome string

	ClaudeSettingsPath string

	TaskHome string

	OpenclawConfigPath string

	OpenclawIncludeRoot string

	CursorDataDir string

	HermesHome string

	logger *slog.Logger
}

func PredictRootDir(workspacesRoot, workspaceID, taskID string) string {
	if workspacesRoot == "" || workspaceID == "" || taskID == "" {
		return ""
	}
	return filepath.Join(workspacesRoot, workspaceID, taskKey(taskID))
}

func Prepare(params PrepareParams, logger *slog.Logger) (*Environment, error) {
	if params.WorkspacesRoot == "" {
		return nil, fmt.Errorf("execenv: workspaces root is required")
	}
	if params.WorkspaceID == "" {
		return nil, fmt.Errorf("execenv: workspace ID is required")
	}
	if params.TaskID == "" {
		return nil, fmt.Errorf("execenv: task ID is required")
	}

	envRoot := PredictRootDir(params.WorkspacesRoot, params.WorkspaceID, params.TaskID)

	if err := EnsureWorkspacesRootMarker(params.WorkspacesRoot); err != nil && logger != nil {
		logger.Warn("execenv: workspaces root marker not written; fail-closed guard limited to the task workdir", "error", err)
	}

	var lockFile *os.File
	lockClaimed := false
	if params.EnvRootPreclaimed {

		if err := os.MkdirAll(envRoot, 0o755); err != nil {
			return nil, fmt.Errorf("execenv: create env root %s: %w", envRoot, err)
		}
	} else {
		lock, reset, err := claimEnvRoot(envRoot, params.TaskID)
		if err != nil {
			return nil, fmt.Errorf("execenv: %w", err)
		}
		lockFile = lock

		lockClaimed = true
		defer func() {
			if lockClaimed {
				releaseEnvRootLock(lockFile)
			}
		}()

		if reset {
			if err := resetEnvRootContents(envRoot); err != nil {
				return nil, fmt.Errorf("execenv: reset existing env: %w", err)
			}
		}
	}

	workDir := filepath.Join(envRoot, "workdir")
	scratchDirs := []string{filepath.Join(envRoot, "output"), filepath.Join(envRoot, "logs")}
	if params.LocalWorkDir == "" {
		scratchDirs = append(scratchDirs, workDir)
	} else {
		workDir = params.LocalWorkDir
	}
	for _, dir := range scratchDirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("execenv: create directory %s: %w", dir, err)
		}
	}

	env := &Environment{
		RootDir:        envRoot,
		WorkDir:        workDir,
		LocalDirectory: params.LocalWorkDir != "",
		lockFile:       lockFile,
		logger:         logger,
	}

	manifest := &sidecarManifest{}

	prepareSucceeded := false
	if params.LocalWorkDir != "" {
		defer func() {
			if prepareSucceeded {
				return
			}
			if err := rollBackPreparedSidecars(*manifest); err != nil && logger != nil {
				logger.Warn("execenv: roll back sidecars after failed prepare", "work_dir", workDir, "error", err)
			}
		}()
	}

	if err := writeContextFiles(workDir, params.Provider, params.Task, manifest); err != nil {
		return nil, fmt.Errorf("execenv: write context files: %w", err)
	}

	if params.LocalWorkDir == "" && params.Task.IssueID != "" {
		if err := WriteManagedEnvProvenance(envRoot, ManagedEnvProvenance{
			WorkspaceID: params.WorkspaceID,
			IssueID:     params.Task.IssueID,
			AgentID:     params.Task.AgentID,
		}); err != nil && logger != nil {
			logger.Warn("execenv: write managed env provenance failed (non-fatal); a follow-up may start a fresh session", "error", err)
		}
	}

	if params.Provider == runtimeCodeE {
		codexHome := filepath.Join(envRoot, codexHomeDirName)

		taskHome, writableRoots, err := prepareCodexSandboxHome(envRoot, "", params.CodexVersion, logger)
		if err != nil {
			return nil, fmt.Errorf("execenv: prepare task home: %w", err)
		}
		env.TaskHome = taskHome
		if err := prepareCodexHomeWithOpts(codexHome, CodexHomeOptions{CodexVersion: params.CodexVersion, IsLocalDirectory: params.LocalWorkDir != "", SessionStoreKey: codexSessionStoreKey(params.Profile, params.Task.AgentID, params.Task.IssueID), WritableRoots: writableRoots, CodexCustomArgs: params.CodexCustomArgs}, logger); err != nil {
			return nil, fmt.Errorf("execenv: prepare codex-home: %w", err)
		}
		if err := hydrateCodexSkills(codexHome, params.Task.AgentSkills, params.Task.DisabledRuntimeSkills, logger); err != nil {
			return nil, fmt.Errorf("execenv: hydrate codex skills: %w", err)
		}
		env.CodexHome = codexHome
	}

	if params.Provider == runtimeCodeC {
		settingsPath, err := prepareClaudeSkillSettings(envRoot, params.Task.DisabledRuntimeSkills, params.Task.AgentSkills)
		if err != nil {
			return nil, fmt.Errorf("execenv: prepare claude skill settings: %w", err)
		}
		env.ClaudeSettingsPath = settingsPath
	}

	if params.Provider == runtimeCodeJ && len(params.Task.AgentSkills) > 0 {
		hermesHome := filepath.Join(envRoot, "hermes-home")
		if err := prepareHermesHome(hermesHome, params.HermesSourceHome, params.HermesSourceMustExist, params.Task.AgentSkills, params.HermesEnv, logger); err != nil {
			return nil, fmt.Errorf("execenv: prepare hermes-home: %w", err)
		}
		env.HermesHome = hermesHome
	}

	if params.Provider == runtimeCodeG {
		cursorDataDir, err := prepareCursorMcpConfig(envRoot, workDir, params.McpConfig, params.CursorMcpAuthSource, manifest)
		if err != nil {
			return nil, fmt.Errorf("execenv: prepare cursor mcp config: %w", err)
		}
		env.CursorDataDir = cursorDataDir
	}

	if err := writeSidecarManifest(envRoot, manifest); err != nil {

		if params.LocalWorkDir != "" {
			return nil, fmt.Errorf("execenv: write sidecar manifest: %w", err)
		}
		logger.Warn("execenv: write sidecar manifest failed (non-fatal)", "error", err)
	}

	if params.Provider == runtimeCodeN {
		result, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{
			OpenclawBin: params.OpenclawBin,
			McpConfig:   params.McpConfig,
			Gateway:     params.OpenclawGateway,
		})
		if err != nil {
			return nil, fmt.Errorf("execenv: prepare openclaw config: %w", err)
		}
		env.OpenclawConfigPath = result.ConfigPath
		env.OpenclawIncludeRoot = result.IncludeRoot
	}

	logger.Info("execenv: prepared env", "root", envRoot, "repos_available", len(params.Task.Repos))
	prepareSucceeded = true

	lockClaimed = false
	return env, nil
}

type ReuseParams struct {
	WorkspacesRoot string
	WorkDir        string
	Provider       string
	CodexVersion   string

	ResumeSessionID string
	OpenclawBin     string

	McpConfig json.RawMessage

	CursorMcpAuthSource string

	OpenclawGateway OpenclawGatewayPin

	Profile string

	LocalDirectory bool

	HermesSourceHome      string
	HermesSourceMustExist bool
	HermesEnv             map[string]string

	CodexCustomArgs []string
	Task            TaskContextForEnv
}

func Reuse(params ReuseParams, logger *slog.Logger) *Environment {
	if _, err := os.Stat(params.WorkDir); err != nil {
		return nil
	}

	if params.WorkspacesRoot != "" {
		if err := EnsureWorkspacesRootMarker(params.WorkspacesRoot); err != nil && logger != nil {
			logger.Warn("execenv: workspaces root marker not written on reuse; fail-closed guard limited to the task workdir", "error", err)
		}
	}

	rootDir := filepath.Dir(params.WorkDir)
	if params.LocalDirectory {

		rootDir = ""
	}
	env := &Environment{
		RootDir:        rootDir,
		WorkDir:        params.WorkDir,
		LocalDirectory: params.LocalDirectory,
		logger:         logger,
	}

	if env.RootDir != "" {
		if err := removeReusedManagedSkillDirs(env.RootDir, skillsDirPath(params.WorkDir, params.Provider)); err != nil {
			logger.Warn("execenv: reclaim managed skill dirs on reuse failed", "error", err)
		}
		if err := CleanupSidecars(env.RootDir); err != nil {
			logger.Warn("execenv: roll back prior sidecars on reuse failed", "error", err)
		}
	}

	manifest := &sidecarManifest{}
	if err := writeContextFiles(params.WorkDir, params.Provider, params.Task, manifest); err != nil {
		logger.Warn("execenv: refresh context files failed", "error", err)
	}

	if params.Provider == runtimeCodeE {
		codexHome := filepath.Join(env.RootDir, codexHomeDirName)

		taskHome, writableRoots, err := prepareCodexSandboxHome(env.RootDir, "", params.CodexVersion, logger)
		if err != nil {
			logger.Warn("execenv: refresh task home failed", "error", err)
		}
		env.TaskHome = taskHome
		if err := prepareCodexHomeWithOpts(codexHome, CodexHomeOptions{CodexVersion: params.CodexVersion, ResumeSessionID: params.ResumeSessionID, IsLocalDirectory: params.LocalDirectory, SessionStoreKey: codexSessionStoreKey(params.Profile, params.Task.AgentID, params.Task.IssueID), WritableRoots: writableRoots, CodexCustomArgs: params.CodexCustomArgs}, logger); err != nil {
			logger.Warn("execenv: refresh codex-home failed", "error", err)
		} else {
			env.CodexHome = codexHome
			if err := hydrateCodexSkills(codexHome, params.Task.AgentSkills, params.Task.DisabledRuntimeSkills, logger); err != nil {
				logger.Warn("execenv: refresh codex skills failed", "error", err)
			}
		}
	}

	if params.Provider == runtimeCodeC && env.RootDir != "" {
		settingsPath, err := prepareClaudeSkillSettings(env.RootDir, params.Task.DisabledRuntimeSkills, params.Task.AgentSkills)
		if err != nil {
			logger.Warn("execenv: refresh claude skill settings failed", "error", err)
		} else {
			env.ClaudeSettingsPath = settingsPath
		}
	}

	if params.Provider == runtimeCodeJ && env.RootDir != "" {
		hermesHome := filepath.Join(env.RootDir, "hermes-home")
		if len(params.Task.AgentSkills) > 0 {
			if err := prepareHermesHome(hermesHome, params.HermesSourceHome, params.HermesSourceMustExist, params.Task.AgentSkills, params.HermesEnv, logger); err != nil {

				logger.Warn("execenv: refresh hermes-home failed; forcing fresh prepare", "error", err)
				return nil
			}
			env.HermesHome = hermesHome
		} else {
			env.HermesHome = ""
			if err := os.RemoveAll(hermesHome); err != nil {
				logger.Warn("execenv: remove stale hermes-home failed", "error", err)
			}
		}
	}

	if params.Provider == runtimeCodeG && env.RootDir != "" {
		cursorDataDir, err := prepareCursorMcpConfig(env.RootDir, params.WorkDir, params.McpConfig, params.CursorMcpAuthSource, manifest)
		if err != nil {
			logger.Warn("execenv: refresh cursor mcp config failed", "error", err)
			return nil
		}
		env.CursorDataDir = cursorDataDir
	}

	if env.RootDir != "" {
		if err := writeSidecarManifest(env.RootDir, manifest); err != nil {
			logger.Warn("execenv: refresh sidecar manifest failed", "error", err)
		}
	}

	if params.Provider == runtimeCodeN {
		result, err := prepareOpenclawConfig(env.RootDir, params.WorkDir, OpenclawConfigPrep{
			OpenclawBin: params.OpenclawBin,
			McpConfig:   params.McpConfig,
			Gateway:     params.OpenclawGateway,
		})
		if err != nil {
			logger.Warn("execenv: refresh openclaw config failed", "error", err)
			return nil
		}
		env.OpenclawConfigPath = result.ConfigPath
		env.OpenclawIncludeRoot = result.IncludeRoot
	}

	logger.Info("execenv: reusing env", "workdir", params.WorkDir)
	return env
}

func hydrateCodexSkills(codexHome string, workspaceSkills []SkillContextForEnv, disabledRuntimeSkills []RuntimeSkillRefForEnv, logger *slog.Logger) error {
	skillsDir := filepath.Join(codexHome, "skills")
	if err := os.RemoveAll(skillsDir); err != nil {
		return fmt.Errorf("clear codex skills dir: %w", err)
	}
	if err := seedUserCodexSkills(codexHome, workspaceSkills, logger); err != nil {
		logger.Warn("execenv: seed user codex skills failed", "error", err)
	}
	if len(workspaceSkills) > 0 {
		if err := writeSkillFiles(skillsDir, workspaceSkills, nil); err != nil {
			return err
		}
	}
	return ensureCodexDisabledSkillsConfig(filepath.Join(codexHome, "config.toml"), codexHome, disabledRuntimeSkills, workspaceSkills)
}

type GCMetaKind string

const (
	GCKindIssue        GCMetaKind = "issue"
	GCKindChat         GCMetaKind = "chat"
	GCKindAutopilotRun GCMetaKind = "autopilot_run"
	GCKindQuickCreate  GCMetaKind = "quick_create"
)

type GCMeta struct {
	Kind           GCMetaKind `json:"kind,omitempty"`
	IssueID        string     `json:"issue_id,omitempty"`
	ChatSessionID  string     `json:"chat_session_id,omitempty"`
	AutopilotRunID string     `json:"autopilot_run_id,omitempty"`
	TaskID         string     `json:"task_id,omitempty"`
	WorkspaceID    string     `json:"workspace_id"`
	CompletedAt    time.Time  `json:"completed_at"`

	LocalDirectory bool `json:"local_directory,omitempty"`
}

const gcMetaFile = ".gc_meta.json"

func WriteGCMeta(envRoot string, meta GCMeta, logger *slog.Logger) error {
	if envRoot == "" {
		return nil
	}
	if meta.Kind == "" {

		logger.Debug("execenv: skipping .gc_meta.json write: kind is empty", "envRoot", envRoot)
		return nil
	}
	meta.CompletedAt = time.Now().UTC()
	data, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal gc meta: %w", err)
	}
	return os.WriteFile(filepath.Join(envRoot, gcMetaFile), data, 0o644)
}

func ReadGCMeta(envRoot string) (*GCMeta, error) {
	data, err := os.ReadFile(filepath.Join(envRoot, gcMetaFile))
	if err != nil {
		return nil, err
	}
	var meta GCMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	if meta.Kind == "" {
		meta.Kind = GCKindIssue
	}
	return &meta, nil
}

const managedEnvProvenanceFile = ".managed_env.json"

const ManagedEnvProvenanceManagedBy = "goosar-daemon-managed-env"

type ManagedEnvProvenance struct {
	ManagedBy   string `json:"managed_by"`
	WorkspaceID string `json:"workspace_id"`
	IssueID     string `json:"issue_id"`
	AgentID     string `json:"agent_id"`
}

func WriteManagedEnvProvenance(envRoot string, p ManagedEnvProvenance) error {
	if envRoot == "" {
		return nil
	}
	p.ManagedBy = ManagedEnvProvenanceManagedBy
	data, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal managed env provenance: %w", err)
	}
	return os.WriteFile(filepath.Join(envRoot, managedEnvProvenanceFile), data, 0o644)
}

func ReadManagedEnvProvenance(envRoot string) (*ManagedEnvProvenance, error) {
	data, err := os.ReadFile(filepath.Join(envRoot, managedEnvProvenanceFile))
	if err != nil {
		return nil, err
	}
	var p ManagedEnvProvenance
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (env *Environment) Cleanup(removeAll bool) error {
	if env == nil {
		return nil
	}

	env.ReleaseLock()

	if env.LocalDirectory {

		if removeAll && env.RootDir != "" {
			if err := os.RemoveAll(env.RootDir); err != nil {
				env.logger.Warn("execenv: cleanup local_directory envRoot failed", "error", err)
				return err
			}
		}
		return nil
	}

	if removeAll {
		if err := os.RemoveAll(env.RootDir); err != nil {
			env.logger.Warn("execenv: cleanup removeAll failed", "error", err)
			return err
		}
		return nil
	}

	if err := os.RemoveAll(env.WorkDir); err != nil {
		env.logger.Warn("execenv: cleanup workdir failed", "error", err)
		return err
	}
	return nil
}

type EnvRootClaim struct {
	rootDir string
	lock    *os.File
}

func (c *EnvRootClaim) RootDir() string {
	if c == nil {
		return ""
	}
	return c.rootDir
}

func (c *EnvRootClaim) Release() {
	if c == nil || c.lock == nil {
		return
	}
	releaseEnvRootLock(c.lock)
	c.lock = nil
}

func ClaimEnvRoot(workspacesRoot, workspaceID, taskID string) (*EnvRootClaim, error) {
	if workspacesRoot == "" || workspaceID == "" || taskID == "" {
		return nil, fmt.Errorf("execenv: claim env root: workspaces root, workspace ID and task ID are all required")
	}
	envRoot := PredictRootDir(workspacesRoot, workspaceID, taskID)
	lock, reset, err := claimEnvRoot(envRoot, taskID)
	if err != nil {
		return nil, fmt.Errorf("execenv: %w", err)
	}
	if reset {
		if err := resetEnvRootContents(envRoot); err != nil {
			releaseEnvRootLock(lock)
			return nil, fmt.Errorf("execenv: reset existing env: %w", err)
		}
	}
	return &EnvRootClaim{rootDir: envRoot, lock: lock}, nil
}

var ErrEnvRootBusy = errors.New("env root is held by a running execution")

func LockEnvRootForReuse(envRoot string) (*EnvRootClaim, error) {
	info, err := os.Stat(envRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("execenv: inspect env root %s: %w", envRoot, err)
	}
	if !info.IsDir() {
		return nil, nil
	}
	lock, err := openEnvRootLockFile(filepath.Join(envRoot, envRootLockFile))
	if err != nil {
		return nil, fmt.Errorf("execenv: open env root lock for %s: %w", envRoot, err)
	}
	locked, err := lockFileExclusiveNonBlocking(lock)
	if err != nil {
		lock.Close()
		return nil, fmt.Errorf("execenv: lock env root %s: %w", envRoot, err)
	}
	if !locked {
		lock.Close()
		return nil, fmt.Errorf("execenv: reuse %s: %w", envRoot, ErrEnvRootBusy)
	}
	return &EnvRootClaim{rootDir: envRoot, lock: lock}, nil
}

const envRootOwnerFile = ".task_owner"

const envRootLockFile = ".task_lock"

func claimEnvRoot(envRoot, taskID string) (lockFile *os.File, reset bool, err error) {
	if err := os.MkdirAll(envRoot, 0o755); err != nil {
		return nil, false, fmt.Errorf("create env root %s: %w", envRoot, err)
	}

	lockFile, err = openEnvRootLockFile(filepath.Join(envRoot, envRootLockFile))
	if err != nil {
		return nil, false, fmt.Errorf("open env root lock for %s: %w", envRoot, err)
	}
	locked, err := lockFileExclusiveNonBlocking(lockFile)
	if err != nil {
		lockFile.Close()
		return nil, false, fmt.Errorf("lock env root %s: %w", envRoot, err)
	}
	if !locked {
		lockFile.Close()
		return nil, false, fmt.Errorf("env root %s: %w; refusing to reset it for task %s", envRoot, ErrEnvRootBusy, taskID)
	}

	defer func() {
		if err != nil {
			releaseEnvRootLock(lockFile)
			lockFile = nil
		}
	}()

	owner, err := readEnvRootOwner(envRoot)
	if err != nil {
		return nil, false, fmt.Errorf("read env root owner for %s: %w", envRoot, err)
	}
	switch {
	case owner == taskID:

		return lockFile, true, nil
	case owner != "":
		return nil, false, fmt.Errorf("env root %s belongs to task %s; refusing to reset it for task %s", envRoot, owner, taskID)
	}

	hasWork, err := envRootHoldsWork(envRoot)
	if err != nil {
		return nil, false, fmt.Errorf("inspect env root %s: %w", envRoot, err)
	}
	if hasWork {
		return nil, false, fmt.Errorf("env root %s already holds files but names no owning task; refusing to delete it", envRoot)
	}
	if err := writeEnvRootOwner(envRoot, taskID); err != nil {
		return nil, false, err
	}
	return lockFile, false, nil
}

func (env *Environment) ReleaseLock() {
	if env == nil || env.lockFile == nil {
		return
	}
	releaseEnvRootLock(env.lockFile)
	env.lockFile = nil
}

func releaseEnvRootLock(f *os.File) {
	if f == nil {
		return
	}
	_ = unlockFile(f)
	_ = f.Close()
}

func writeEnvRootOwner(envRoot, taskID string) error {
	path := filepath.Join(envRoot, envRootOwnerFile)
	if err := os.WriteFile(path, []byte(taskID), 0o644); err != nil {
		return fmt.Errorf("record env root owner for %s: %w", envRoot, err)
	}
	return nil
}

func readEnvRootOwner(envRoot string) (string, error) {
	b, err := os.ReadFile(filepath.Join(envRoot, envRootOwnerFile))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func resetEnvRootContents(envRoot string) error {
	entries, err := os.ReadDir(envRoot)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if isEnvRootBookkeeping(e.Name()) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(envRoot, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

func envRootHoldsWork(envRoot string) (bool, error) {
	entries, err := os.ReadDir(envRoot)
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if !isEnvRootBookkeeping(e.Name()) {
			return true, nil
		}
	}
	return false, nil
}

func isEnvRootBookkeeping(name string) bool {
	return name == envRootOwnerFile || name == envRootLockFile
}
