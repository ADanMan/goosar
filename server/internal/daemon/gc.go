package daemon

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/adanman/goosar/server/internal/daemon/execenv"
)

func (d *Daemon) gcLoop(ctx context.Context) {
	if !d.cfg.GCEnabled {
		d.logger.Info("gc: disabled")
		return
	}
	d.logger.Info("gc: started",
		"interval", d.cfg.GCInterval,
		"ttl", d.cfg.GCTTL,
		"orphan_ttl", d.cfg.GCOrphanTTL,
		"artifact_ttl", d.cfg.GCArtifactTTL,
		"artifact_patterns", d.cfg.GCArtifactPatterns,
		"managed_artifact_subpaths", execenv.ManagedReclaimableArtifactSubpaths(),
	)

	if err := sleepWithContext(ctx, 30*time.Second); err != nil {
		return
	}
	d.runGC(ctx)

	ticker := time.NewTicker(d.cfg.GCInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.runGC(ctx)
		}
	}
}

type gcStats struct {
	cleaned         int
	orphaned        int
	skipped         int
	artifactDirs    int
	artifactRemoved int
	storesReclaimed int
	bytesReclaimed  int64
	byPattern       map[string]int
}

func (d *Daemon) runGC(ctx context.Context) {
	root := d.cfg.WorkspacesRoot
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		d.logger.Warn("gc: read workspaces root failed", "error", err)
		return
	}

	stats := &gcStats{byPattern: map[string]int{}}
	for _, wsEntry := range entries {
		if !wsEntry.IsDir() || wsEntry.Name() == ".repos" {
			continue
		}
		wsDir := filepath.Join(root, wsEntry.Name())
		d.gcWorkspace(ctx, wsDir, stats)
	}

	d.pruneRepoWorktrees(root)

	if storesRemoved, storeBytes := execenv.PruneCodexSessionStores(d.cfg.Profile, d.cfg.GCCodexSessionTTL, time.Now(), d.reserveCodexStoreForDeletion, d.logger); storesRemoved > 0 {
		stats.storesReclaimed += storesRemoved
		stats.bytesReclaimed += storeBytes
	}

	if stats.cleaned > 0 || stats.orphaned > 0 || stats.artifactDirs > 0 || stats.storesReclaimed > 0 {
		d.logger.Info("gc: cycle complete",
			"cleaned", stats.cleaned,
			"orphaned", stats.orphaned,
			"skipped", stats.skipped,
			"artifact_dirs", stats.artifactDirs,
			"artifact_removed", stats.artifactRemoved,
			"codex_session_stores_reclaimed", stats.storesReclaimed,
			"bytes_reclaimed", stats.bytesReclaimed,
			"by_pattern", stats.byPattern,
		)
	}
}

func (d *Daemon) gcWorkspace(ctx context.Context, wsDir string, stats *gcStats) {
	taskEntries, err := os.ReadDir(wsDir)
	if err != nil {
		d.logger.Warn("gc: read workspace dir failed", "dir", wsDir, "error", err)
		return
	}

	cleanedHere := 0
	issueCandidates := make([]issueGCCandidate, 0, len(taskEntries))
	for _, entry := range taskEntries {
		if ctx.Err() != nil {
			return
		}
		if !entry.IsDir() {
			continue
		}
		taskDir := filepath.Join(wsDir, entry.Name())
		if d.isActiveEnvRoot(taskDir) {
			stats.skipped++
			continue
		}
		meta, metaErr := execenv.ReadGCMeta(taskDir)
		if metaErr == nil && meta.Kind == execenv.GCKindIssue && strings.TrimSpace(meta.IssueID) != "" {
			issueCandidates = append(issueCandidates, issueGCCandidate{taskDir: taskDir, meta: meta})
			continue
		}
		action := d.shouldCleanTaskDir(ctx, taskDir)
		cleanedHere += d.applyGCAction(taskDir, action, stats)
	}
	cleanedHere += d.gcWorkspaceIssues(ctx, filepath.Base(wsDir), issueCandidates, stats)

	if cleanedHere > 0 {
		remaining, _ := os.ReadDir(wsDir)
		if len(remaining) == 0 {
			os.Remove(wsDir)
		}
	}
}

const issueGCBatchSize = 500

type issueGCCandidate struct {
	taskDir string
	meta    *execenv.GCMeta
}

func (d *Daemon) gcWorkspaceIssues(ctx context.Context, workspaceID string, candidates []issueGCCandidate, stats *gcStats) int {
	if len(candidates) == 0 {
		return 0
	}

	issueIDs := make([]string, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		issueID := strings.TrimSpace(candidate.meta.IssueID)
		if _, ok := seen[issueID]; ok {
			continue
		}
		seen[issueID] = struct{}{}
		issueIDs = append(issueIDs, issueID)
	}

	results := make(map[string]IssueGCCheckResult, len(issueIDs))
	for start := 0; start < len(issueIDs); start += issueGCBatchSize {
		if ctx.Err() != nil {
			break
		}
		end := min(start+issueGCBatchSize, len(issueIDs))
		chunkResults, err := d.client.GetIssueGCChecks(ctx, workspaceID, issueIDs[start:end])
		if err != nil {
			d.logger.Warn("gc: batch issue check failed",
				"workspace", workspaceID,
				"count", end-start,
				"error", err,
			)
			continue
		}
		for issueID, result := range chunkResults {
			results[issueID] = result
		}
	}

	cleaned := 0
	for i, candidate := range candidates {
		if ctx.Err() != nil {
			stats.skipped += len(candidates) - i
			break
		}
		issueID := strings.TrimSpace(candidate.meta.IssueID)
		result, ok := results[issueID]
		if !ok || result.Err != nil {
			stats.skipped++
			continue
		}
		action := d.gcDecisionIssueResult(candidate.taskDir, candidate.meta, result)
		action = d.applyLocalDirectoryGCOverride(candidate.meta, action)
		cleaned += d.applyGCAction(candidate.taskDir, action, stats)
	}
	return cleaned
}

func (d *Daemon) applyGCAction(taskDir string, action gcAction, stats *gcStats) int {
	if action != gcActionSkip {
		release, ok := d.reserveEnvRootForGC(taskDir)
		if !ok {
			stats.skipped++
			return 0
		}
		defer release()
	}
	switch action {
	case gcActionClean:
		bytes := dirSize(taskDir)
		d.cleanTaskDir(taskDir)
		stats.cleaned++
		stats.bytesReclaimed += bytes
		return 1
	case gcActionOrphan:
		bytes := dirSize(taskDir)
		d.cleanTaskDir(taskDir)
		stats.orphaned++
		stats.bytesReclaimed += bytes
		return 1
	case gcActionCleanArtifacts:
		removed, bytes, perPattern := d.cleanTaskArtifacts(taskDir, d.cfg.GCArtifactPatterns)
		recordArtifactCleanup(stats, removed, bytes, perPattern)
		stats.skipped++
	case gcActionCleanManagedArtifacts:
		removed, bytes, perPattern := d.cleanManagedTaskArtifacts(taskDir)
		recordArtifactCleanup(stats, removed, bytes, perPattern)
		stats.skipped++
	default:
		stats.skipped++
	}
	return 0
}

func recordArtifactCleanup(stats *gcStats, removed int, bytes int64, perPattern map[string]int) {
	if removed == 0 {
		return
	}
	stats.artifactDirs++
	stats.artifactRemoved += removed
	stats.bytesReclaimed += bytes
	if stats.byPattern == nil {
		stats.byPattern = map[string]int{}
	}
	for pattern, count := range perPattern {
		stats.byPattern[pattern] += count
	}
}

type gcAction int

const (
	gcActionSkip gcAction = iota
	gcActionClean
	gcActionOrphan
	gcActionCleanArtifacts
	gcActionCleanManagedArtifacts
)

func (d *Daemon) shouldCleanTaskDir(ctx context.Context, taskDir string) gcAction {

	if d.isActiveEnvRoot(taskDir) {
		return gcActionSkip
	}

	meta, err := execenv.ReadGCMeta(taskDir)
	if err != nil {
		return d.orphanByMTime(taskDir, "no meta")
	}

	action := d.shouldCleanTaskDirForKind(ctx, taskDir, meta)
	return d.applyLocalDirectoryGCOverride(meta, action)
}

func (d *Daemon) applyLocalDirectoryGCOverride(meta *execenv.GCMeta, action gcAction) gcAction {
	if !meta.LocalDirectory {
		return action
	}

	if d.cfg.GCArtifactTTL <= 0 {
		return gcActionSkip
	}
	switch action {
	case gcActionClean:
		return gcActionCleanArtifacts
	case gcActionOrphan:
		return gcActionCleanManagedArtifacts
	default:
		return action
	}
}

func (d *Daemon) shouldCleanTaskDirForKind(ctx context.Context, taskDir string, meta *execenv.GCMeta) gcAction {
	switch meta.Kind {
	case execenv.GCKindIssue:
		return d.gcDecisionIssue(ctx, taskDir, meta)
	case execenv.GCKindChat:
		return d.gcDecisionChat(ctx, taskDir, meta)
	case execenv.GCKindAutopilotRun:
		return d.gcDecisionAutopilotRun(ctx, taskDir, meta)
	case execenv.GCKindQuickCreate:
		return d.gcDecisionQuickCreate(ctx, taskDir, meta)
	default:

		return d.orphanByMTime(taskDir, "unknown kind")
	}
}

func (d *Daemon) orphanByMTime(taskDir, reason string) gcAction {
	info, err := os.Stat(taskDir)
	if err != nil {
		return gcActionSkip
	}
	if time.Since(info.ModTime()) > d.cfg.GCOrphanTTL {
		d.logger.Info("gc: orphan directory", "dir", taskDir, "reason", reason, "age", time.Since(info.ModTime()).Round(time.Hour))
		return gcActionOrphan
	}
	return gcActionSkip
}

func isAccessNotFound(err error) bool {
	var reqErr *requestError
	return errors.As(err, &reqErr) && reqErr.StatusCode == http.StatusNotFound
}

func (d *Daemon) gcDecisionIssue(ctx context.Context, taskDir string, meta *execenv.GCMeta) gcAction {
	if strings.TrimSpace(meta.IssueID) == "" {
		return d.orphanByMTime(taskDir, "empty issue id")
	}

	status, err := d.client.GetIssueGCCheck(ctx, meta.IssueID)
	if err != nil {
		if isAccessNotFound(err) {

			return d.orphanByMTime(taskDir, "issue not accessible")
		}
		return gcActionSkip
	}

	return d.gcDecisionIssueResult(taskDir, meta, IssueGCCheckResult{
		ID:        meta.IssueID,
		Found:     true,
		Status:    status.Status,
		UpdatedAt: status.UpdatedAt,
	})
}

func (d *Daemon) gcDecisionIssueResult(taskDir string, meta *execenv.GCMeta, result IssueGCCheckResult) gcAction {
	if !result.Found {
		return d.orphanByMTime(taskDir, "issue not accessible")
	}

	if (result.Status == "done" || result.Status == "cancelled") &&
		time.Since(result.UpdatedAt) > d.cfg.GCTTL {
		d.logger.Info("gc: eligible for cleanup",
			"dir", filepath.Base(taskDir),
			"kind", "issue",
			"issue", meta.IssueID,
			"status", result.Status,
			"updated_at", result.UpdatedAt.Format(time.RFC3339),
		)
		return gcActionClean
	}

	if d.cfg.GCArtifactTTL > 0 && !meta.CompletedAt.IsZero() && time.Since(meta.CompletedAt) > d.cfg.GCArtifactTTL {
		d.logger.Info("gc: eligible for artifact cleanup",
			"dir", filepath.Base(taskDir),
			"kind", "issue",
			"issue", meta.IssueID,
			"status", result.Status,
			"completed_at", meta.CompletedAt.Format(time.RFC3339),
		)
		return gcActionCleanArtifacts
	}

	if d.cfg.GCArtifactTTL > 0 && meta.CompletedAt.IsZero() {
		if age, ok := gcMetaFileAge(taskDir); ok && age > d.cfg.GCOrphanTTL {
			d.logger.Info("gc: legacy task eligible for managed artifact cleanup",
				"dir", filepath.Base(taskDir),
				"kind", "issue",
				"issue", meta.IssueID,
				"status", result.Status,
				"age", age.Round(time.Hour),
			)
			return gcActionCleanManagedArtifacts
		}
	}

	return gcActionSkip
}

func gcMetaFileAge(taskDir string) (time.Duration, bool) {
	info, err := os.Stat(filepath.Join(taskDir, ".gc_meta.json"))
	if err != nil {
		return 0, false
	}
	return time.Since(info.ModTime()), true
}

func (d *Daemon) gcDecisionChat(ctx context.Context, taskDir string, meta *execenv.GCMeta) gcAction {
	if strings.TrimSpace(meta.ChatSessionID) == "" {
		return d.orphanByMTime(taskDir, "empty chat session id")
	}

	status, err := d.client.GetChatSessionGCCheck(ctx, meta.ChatSessionID)
	if err != nil {
		if isAccessNotFound(err) {

			d.logger.Info("gc: eligible for cleanup",
				"dir", filepath.Base(taskDir),
				"kind", "chat",
				"chat_session", meta.ChatSessionID,
				"reason", "session not accessible (hard-deleted)",
			)
			return gcActionClean
		}
		return gcActionSkip
	}

	switch status.Status {
	case "active":

		return gcActionSkip
	case "archived":
		if time.Since(status.UpdatedAt) > d.cfg.GCTTL {
			d.logger.Info("gc: eligible for cleanup",
				"dir", filepath.Base(taskDir),
				"kind", "chat",
				"chat_session", meta.ChatSessionID,
				"status", status.Status,
				"updated_at", status.UpdatedAt.Format(time.RFC3339),
			)
			return gcActionClean
		}
	}
	return gcActionSkip
}

func (d *Daemon) gcDecisionAutopilotRun(ctx context.Context, taskDir string, meta *execenv.GCMeta) gcAction {
	if strings.TrimSpace(meta.AutopilotRunID) == "" {
		return d.orphanByMTime(taskDir, "empty autopilot run id")
	}

	status, err := d.client.GetAutopilotRunGCCheck(ctx, meta.AutopilotRunID)
	if err != nil {
		if isAccessNotFound(err) {
			return d.orphanByMTime(taskDir, "autopilot run not accessible")
		}
		return gcActionSkip
	}

	if isAutopilotRunTerminal(status.Status) {
		d.logger.Info("gc: eligible for cleanup",
			"dir", filepath.Base(taskDir),
			"kind", "autopilot_run",
			"autopilot_run", meta.AutopilotRunID,
			"status", status.Status,
		)
		return gcActionClean
	}
	return gcActionSkip
}

func isAutopilotRunTerminal(status string) bool {
	switch status {
	case "completed", "failed", "skipped", "issue_created":
		return true
	default:
		return false
	}
}

func (d *Daemon) gcDecisionQuickCreate(ctx context.Context, taskDir string, meta *execenv.GCMeta) gcAction {
	if strings.TrimSpace(meta.TaskID) == "" {
		return d.orphanByMTime(taskDir, "empty task id")
	}

	status, err := d.client.GetTaskGCCheck(ctx, meta.TaskID)
	if err != nil {
		if isAccessNotFound(err) {

			return d.orphanByMTime(taskDir, "task not accessible")
		}
		return gcActionSkip
	}

	if isAgentTaskTerminal(status.Status) {
		d.logger.Info("gc: eligible for cleanup",
			"dir", filepath.Base(taskDir),
			"kind", "quick_create",
			"task", meta.TaskID,
			"status", status.Status,
		)
		return gcActionClean
	}
	return gcActionSkip
}

func isAgentTaskTerminal(status string) bool {
	switch status {
	case "completed", "failed", "cancelled":
		return true
	default:
		return false
	}
}

func (d *Daemon) cleanTaskDir(taskDir string) {
	if err := os.RemoveAll(taskDir); err != nil {
		d.logger.Warn("gc: remove task dir failed", "dir", taskDir, "error", err)
	} else {
		d.logger.Info("gc: removed", "dir", taskDir)
	}
}

func (d *Daemon) cleanTaskArtifacts(taskDir string, patterns []string) (removed int, bytes int64, perPattern map[string]int) {
	return d.cleanTaskArtifactsMatching(taskDir, newArtifactMatcher(patterns, execenv.ManagedReclaimableArtifactSubpaths()))
}

func (d *Daemon) cleanManagedTaskArtifacts(taskDir string) (removed int, bytes int64, perPattern map[string]int) {
	return d.cleanTaskArtifactsMatching(taskDir, newArtifactMatcher(nil, execenv.ManagedReclaimableArtifactSubpaths()))
}

func (d *Daemon) cleanTaskArtifactsMatching(taskDir string, matcher artifactMatcher) (removed int, bytes int64, perPattern map[string]int) {
	perPattern = map[string]int{}
	if taskDir == "" || (len(matcher.basenames) == 0 && len(matcher.exactPaths) == 0) {
		return
	}

	absRoot, err := filepath.Abs(taskDir)
	if err != nil {
		return
	}

	walkErr := filepath.WalkDir(absRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if path == absRoot {
			return nil
		}
		if !entry.IsDir() {
			return nil
		}

		if entry.Name() == ".git" {
			return filepath.SkipDir
		}

		info, statErr := os.Lstat(path)
		if statErr != nil {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return filepath.SkipDir
		}
		pattern, ok := matcher.matchDirectory(absRoot, path, entry)
		if !ok {
			return nil
		}
		size := dirSize(path)
		if rmErr := os.RemoveAll(path); rmErr != nil {
			d.logger.Warn("gc: artifact remove failed", "path", path, "error", rmErr)
			return filepath.SkipDir
		}
		removed++
		bytes += size
		perPattern[pattern]++
		d.logger.Info("gc: artifact removed", "path", path, "bytes", size)

		return filepath.SkipDir
	})
	if walkErr != nil {
		d.logger.Warn("gc: artifact walk failed", "dir", taskDir, "error", walkErr)
	}
	return
}

func dirSize(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(_ string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return nil
		}
		if info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	return total
}

const (
	gitCmdTimeout         = 30 * time.Second
	gitMaintenanceTimeout = 10 * time.Minute
)

func (d *Daemon) pruneRepoWorktrees(workspacesRoot string) {
	reposRoot := filepath.Join(workspacesRoot, ".repos")
	wsEntries, err := os.ReadDir(reposRoot)
	if err != nil {
		return
	}

	for _, wsEntry := range wsEntries {
		if !wsEntry.IsDir() {
			continue
		}
		wsRepoDir := filepath.Join(reposRoot, wsEntry.Name())
		repoEntries, err := os.ReadDir(wsRepoDir)
		if err != nil {
			continue
		}
		for _, repoEntry := range repoEntries {
			if !repoEntry.IsDir() {
				continue
			}
			barePath := filepath.Join(wsRepoDir, repoEntry.Name())
			if !isBareRepo(barePath) {
				continue
			}
			d.pruneWorktree(barePath)
		}
	}
}

func (d *Daemon) pruneWorktree(barePath string) {
	if d.repoCache != nil {
		if err := d.repoCache.WithRepoLock(barePath, func() error {
			d.pruneWorktreeLocked(barePath)
			return nil
		}); err != nil {
			d.logger.Warn("gc: repo lock failed", "repo", barePath, "error", err)
			return
		}
		return
	}

	d.pruneWorktreeLocked(barePath)
}

func (d *Daemon) pruneWorktreeLocked(barePath string) {
	if out, err := runGitGCCommand(barePath, "worktree", "prune"); err != nil {
		d.logger.Warn("gc: worktree prune failed",
			"repo", barePath,
			"output", out,
			"error", err,
		)
	}

	activeBranches, err := agentWorktreeBranches(barePath)
	if err != nil {
		d.logger.Warn("gc: worktree branch scan failed", "repo", barePath, "error", err)
		return
	}

	agentBranches, err := listAgentBranches(barePath)
	if err != nil {
		d.logger.Warn("gc: agent branch scan failed", "repo", barePath, "error", err)
		return
	}

	deleted := 0
	for _, branch := range agentBranches {
		if _, ok := activeBranches[branch]; ok {
			continue
		}
		if out, err := runGitGCCommand(barePath, "branch", "-D", "--", branch); err != nil {
			d.logger.Warn("gc: agent branch delete failed",
				"repo", barePath,
				"branch", branch,
				"output", out,
				"error", err,
			)
			continue
		}
		deleted++
	}
	if deleted == 0 {
		return
	}
	d.logger.Info("gc: deleted stale agent branches", "repo", barePath, "count", deleted)

	maintenance := []struct {
		args    []string
		timeout time.Duration
	}{
		{args: []string{"reflog", "expire", "--expire=30.days", "--all"}, timeout: gitCmdTimeout},
		{args: []string{"gc", "--prune=30.days"}, timeout: gitMaintenanceTimeout},
	}
	for _, step := range maintenance {
		if out, err := runGitCommand(barePath, step.timeout, step.args...); err != nil {
			d.logger.Warn("gc: git maintenance failed",
				"repo", barePath,
				"command", strings.Join(step.args, " "),
				"output", out,
				"error", err,
			)
		}
	}
}

func runGitGCCommand(barePath string, args ...string) (string, error) {
	return runGitCommand(barePath, gitCmdTimeout, args...)
}

func runGitCommand(barePath string, timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmdArgs := append([]string{"-C", barePath}, args...)
	cmd := exec.CommandContext(ctx, "git", cmdArgs...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func agentWorktreeBranches(barePath string) (map[string]struct{}, error) {
	out, err := runGitGCCommand(barePath, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}

	branches := make(map[string]struct{})
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "branch refs/heads/") {
			continue
		}
		branch := strings.TrimPrefix(line, "branch refs/heads/")
		if strings.HasPrefix(branch, "agent/") {
			branches[branch] = struct{}{}
		}
	}
	return branches, nil
}

func listAgentBranches(barePath string) ([]string, error) {

	out, err := runGitGCCommand(barePath, "for-each-ref", "--format=%(refname:short)", "refs/heads/agent/")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}

	var branches []string
	for _, line := range strings.Split(out, "\n") {
		branch := strings.TrimSpace(line)
		if branch == "" {
			continue
		}
		branches = append(branches, branch)
	}
	return branches, nil
}

func isBareRepo(path string) bool {
	if _, err := os.Stat(filepath.Join(path, "HEAD")); err != nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(path, "objects")); err != nil {
		return false
	}
	return true
}
