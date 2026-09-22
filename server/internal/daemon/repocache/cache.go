// Пакет repocache управляет кэшем bare-клонов git-репозиториев воркспейса.
// Демон использует его как источник для создания worktree под задачи.
package repocache

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

func gitEnv() []string {
	base := os.Environ()

	existing := 0
	for _, e := range base {
		if strings.HasPrefix(e, "GIT_CONFIG_COUNT=") {
			if n, err := strconv.Atoi(strings.TrimPrefix(e, "GIT_CONFIG_COUNT=")); err == nil {
				existing = n
			}
		}
	}

	idx := strconv.Itoa(existing)
	return append(base,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_COUNT="+strconv.Itoa(existing+1),
		"GIT_CONFIG_KEY_"+idx+"=safe.directory",
		"GIT_CONFIG_VALUE_"+idx+"=*",
	)
}

var agentGitExcludePatterns = []string{
	".agent_context",
	"CLAUDE.md",
	"AGENTS.md",
	".claude",
	".opencode",
	".deveco",
	"CODEBUDDY.md",
	".codebuddy",
}

const repoCacheGitTimeout = 10 * time.Minute

func newGitCommand(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", args...)

	cmd.Dir = filepath.VolumeName(os.TempDir()) + string(os.PathSeparator)
	cmd.Env = gitEnv()
	cmd.WaitDelay = 5 * time.Second
	return cmd
}

func runGitCombinedOutput(args ...string) ([]byte, error) {
	return runGitCombinedOutputWithTimeout(repoCacheGitTimeout, args...)
}

func runGitCombinedOutputWithTimeout(timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := newGitCommand(ctx, args...)
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return out, fmt.Errorf("git command timed out after %s: %w", timeout, ctx.Err())
	}
	return out, err
}

func runGitOutput(args ...string) ([]byte, error) {
	return runGitOutputWithTimeout(repoCacheGitTimeout, args...)
}

func runGitOutputWithTimeout(timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := newGitCommand(ctx, args...)
	out, err := cmd.Output()
	if ctx.Err() == context.DeadlineExceeded {
		return out, fmt.Errorf("git command timed out after %s: %w", timeout, ctx.Err())
	}
	return out, err
}

func runGit(args ...string) error {
	return runGitWithTimeout(repoCacheGitTimeout, args...)
}

func runGitWithTimeout(timeout time.Duration, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := newGitCommand(ctx, args...)
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("git command timed out after %s: %w", timeout, ctx.Err())
	}
	return err
}

type RepoInfo struct {
	URL string
}

type CachedRepo struct {
	URL       string
	LocalPath string
}

type Cache struct {
	root   string
	logger *slog.Logger

	repoLocks sync.Map
}

func New(root string, logger *slog.Logger) *Cache {
	return &Cache{root: root, logger: logger}
}

func (c *Cache) lockForRepo(barePath string) *sync.Mutex {
	if l, ok := c.repoLocks.Load(barePath); ok {
		return l.(*sync.Mutex)
	}
	newLock := &sync.Mutex{}
	actual, _ := c.repoLocks.LoadOrStore(barePath, newLock)
	return actual.(*sync.Mutex)
}

func (c *Cache) Sync(workspaceID string, repos []RepoInfo) error {
	wsDir := filepath.Join(c.root, workspaceID)
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		return fmt.Errorf("create workspace cache dir: %w", err)
	}

	var firstErr error
	for _, repo := range repos {
		if repo.URL == "" {
			continue
		}
		barePath := filepath.Join(wsDir, bareDirName(repo.URL))

		repoLock := c.lockForRepo(barePath)
		repoLock.Lock()
		if isBareRepo(barePath) {

			c.logger.Info("repo cache: fetching", "url", repo.URL, "path", barePath)
			if err := gitFetch(barePath); err != nil {
				c.logger.Warn("repo cache: fetch failed", "url", repo.URL, "error", err)
				if firstErr == nil {
					firstErr = err
				}
			}
		} else {

			c.logger.Info("repo cache: cloning", "url", repo.URL, "path", barePath)
			if err := gitCloneBare(repo.URL, barePath); err != nil {
				c.logger.Error("repo cache: clone failed", "url", repo.URL, "error", err)
				if firstErr == nil {
					firstErr = err
				}
			}
		}
		repoLock.Unlock()
	}
	return firstErr
}

func (c *Cache) Lookup(workspaceID, url string) string {
	barePath := filepath.Join(c.root, workspaceID, bareDirName(url))
	if isBareRepo(barePath) {
		return barePath
	}
	return ""
}

func (c *Cache) WithRepoLock(barePath string, fn func() error) error {
	repoLock := c.lockForRepo(barePath)
	repoLock.Lock()
	defer repoLock.Unlock()
	return fn()
}

func (c *Cache) Fetch(barePath string) error {
	return c.WithRepoLock(barePath, func() error {
		return gitFetch(barePath)
	})
}

func bareDirName(rawURL string) string {
	rawURL = strings.TrimRight(rawURL, "/")

	host, path := splitHostAndPath(rawURL)
	host = strings.ToLower(strings.TrimSpace(host))

	host = strings.ReplaceAll(host, ":", "%3A")

	var parts []string
	if host != "" {
		parts = append(parts, host)
	}
	for _, seg := range strings.Split(path, "/") {
		if seg != "" {
			parts = append(parts, seg)
		}
	}

	name := strings.Join(parts, "+")
	if !strings.HasSuffix(name, ".git") {
		name += ".git"
	}
	if name == "" || name == ".git" {
		name = "repo.git"
	}
	return name
}

func splitHostAndPath(rawURL string) (host, path string) {
	if u, err := url.Parse(rawURL); err == nil && u.Scheme != "" && u.Host != "" {
		return u.Host, strings.TrimPrefix(u.Path, "/")
	}
	s := rawURL
	if i := strings.Index(s, "@"); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.Index(s, ":"); i >= 0 {
		return s[:i], s[i+1:]
	}
	return "", s
}

func isBareRepo(path string) bool {

	_, err := os.Stat(filepath.Join(path, "HEAD"))
	return err == nil
}

const modernFetchRefspec = "+refs/heads/*:refs/remotes/origin/*"

func gitCloneBare(url, dest string) error {
	if out, err := runGitCombinedOutput("clone", "--bare", url, dest); err != nil {

		os.RemoveAll(dest)
		return fmt.Errorf("git clone --bare: %s: %w", strings.TrimSpace(string(out)), err)
	}

	if err := ensureRemoteTrackingLayout(dest); err != nil {
		os.RemoveAll(dest)
		return fmt.Errorf("configure fetch refspec: %w", err)
	}
	return nil
}

func gitFetch(barePath string) error {
	if err := ensureRemoteTrackingLayout(barePath); err != nil {
		return fmt.Errorf("ensure refspec: %w", err)
	}
	if err := runGitFetch(barePath); err != nil {
		return err
	}

	_ = runGit("-C", barePath, "remote", "set-head", "origin", "--auto")
	return nil
}

func runGitFetch(barePath string) error {
	if out, err := runGitCombinedOutput("-C", barePath, "fetch", "origin"); err != nil {
		return fmt.Errorf("git fetch: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func ensureRemoteTrackingLayout(barePath string) error {
	cur, err := readFetchRefspec(barePath)
	if err != nil {
		return err
	}
	if cur == modernFetchRefspec || cur == strings.TrimPrefix(modernFetchRefspec, "+") {
		return nil
	}
	if err := setFetchRefspec(barePath, modernFetchRefspec); err != nil {
		return err
	}

	if err := runGitFetch(barePath); err != nil {
		return fmt.Errorf("backfill fetch after refspec migration: %w", err)
	}

	_ = runGit("-C", barePath, "remote", "set-head", "origin", "--auto")
	return nil
}

func readFetchRefspec(barePath string) (string, error) {
	out, err := runGitOutput("-C", barePath, "config", "--get", "remote.origin.fetch")
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return "", nil
		}
		return "", fmt.Errorf("read remote.origin.fetch: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func setFetchRefspec(barePath, refspec string) error {
	out, err := runGitCombinedOutput("-C", barePath, "config", "remote.origin.fetch", refspec)
	if err != nil {
		return fmt.Errorf("set remote.origin.fetch: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

type WorktreeParams struct {
	WorkspaceID         string
	RepoURL             string
	WorkDir             string
	Ref                 string
	AgentName           string
	TaskID              string
	CoAuthoredByEnabled bool

	IsolatedGitMetadata bool
}

type WorktreeResult struct {
	Path       string `json:"path"`
	BranchName string `json:"branch_name"`
}

func (c *Cache) CreateWorktree(params WorktreeParams) (*WorktreeResult, error) {
	barePath := c.Lookup(params.WorkspaceID, params.RepoURL)
	if barePath == "" {
		return nil, fmt.Errorf("repo not found in cache: %s (workspace: %s)", params.RepoURL, params.WorkspaceID)
	}

	repoLock := c.lockForRepo(barePath)
	repoLock.Lock()
	defer repoLock.Unlock()

	if err := gitFetch(barePath); err != nil {

		c.logger.Warn("repo checkout: fetch failed, agent will see possibly stale code",
			"url", params.RepoURL,
			"error", err,
		)
	}

	baseRef, err := resolveBaseRef(barePath, params.Ref)
	if err != nil {
		return nil, err
	}

	if baseRef == "" {
		return nil, fmt.Errorf("cannot resolve default branch for %s: bare cache at %s has no usable refs (origin/* is empty or ambiguous and bare HEAD has no match). The cache may be corrupted; delete it and retry", params.RepoURL, barePath)
	}

	branchName := fmt.Sprintf("agent/%s/%s", sanitizeName(params.AgentName), taskKey(params.TaskID))

	dirName := repoNameFromURL(params.RepoURL)
	worktreePath := filepath.Join(params.WorkDir, dirName)

	if params.IsolatedGitMetadata || isIsolatedCheckout(worktreePath) {
		actualBranch, err := c.createOrUpdateIsolatedCheckout(
			barePath,
			params.RepoURL,
			worktreePath,
			branchName,
			baseRef,
		)
		if err != nil {
			return nil, fmt.Errorf("create isolated checkout: %w", err)
		}

		for _, pattern := range agentGitExcludePatterns {
			_ = excludeFromGit(worktreePath, pattern)
		}
		if params.CoAuthoredByEnabled {
			if err := installCoAuthoredByHook(worktreePath); err != nil {
				c.logger.Warn("repo checkout: install co-authored-by hook failed (non-fatal)", "error", err)
			}
		} else {
			if err := removeCoAuthoredByHook(worktreePath); err != nil {
				c.logger.Warn("repo checkout: remove co-authored-by hook failed (non-fatal)", "error", err)
			}
		}

		c.logger.Info("repo checkout: isolated checkout ready",
			"url", params.RepoURL,
			"path", worktreePath,
			"branch", actualBranch,
			"base", baseRef,
		)
		return &WorktreeResult{Path: worktreePath, BranchName: actualBranch}, nil
	}

	if isGitWorktree(worktreePath) {
		actualBranch, err := updateExistingWorktree(worktreePath, branchName, baseRef)
		if err != nil {
			return nil, fmt.Errorf("update existing worktree: %w", err)
		}

		for _, pattern := range agentGitExcludePatterns {
			_ = excludeFromGit(worktreePath, pattern)
		}

		if params.CoAuthoredByEnabled {
			if err := installCoAuthoredByHook(worktreePath); err != nil {
				c.logger.Warn("repo checkout: install co-authored-by hook failed (non-fatal)", "error", err)
			}
		} else {
			if err := removeCoAuthoredByHook(worktreePath); err != nil {
				c.logger.Warn("repo checkout: remove co-authored-by hook failed (non-fatal)", "error", err)
			}
		}

		c.logger.Info("repo checkout: existing worktree updated",
			"url", params.RepoURL,
			"path", worktreePath,
			"branch", actualBranch,
			"base", baseRef,
		)

		return &WorktreeResult{
			Path:       worktreePath,
			BranchName: actualBranch,
		}, nil
	}

	actualBranch, err := createWorktree(barePath, worktreePath, branchName, baseRef)
	if err != nil {
		return nil, fmt.Errorf("create worktree: %w", err)
	}

	for _, pattern := range agentGitExcludePatterns {
		_ = excludeFromGit(worktreePath, pattern)
	}

	if params.CoAuthoredByEnabled {
		if err := installCoAuthoredByHook(worktreePath); err != nil {
			c.logger.Warn("repo checkout: install co-authored-by hook failed (non-fatal)", "error", err)
		}
	} else {
		if err := removeCoAuthoredByHook(worktreePath); err != nil {
			c.logger.Warn("repo checkout: remove co-authored-by hook failed (non-fatal)", "error", err)
		}
	}

	c.logger.Info("repo checkout: worktree created",
		"url", params.RepoURL,
		"path", worktreePath,
		"branch", actualBranch,
		"base", baseRef,
	)

	return &WorktreeResult{
		Path:       worktreePath,
		BranchName: actualBranch,
	}, nil
}

const (
	isolatedCheckoutConfigKey   = "goosar.checkout-mode"
	isolatedCheckoutConfigValue = "isolated"
	isolatedCacheRemoteName     = "goosar-cache"
)

func (c *Cache) createOrUpdateIsolatedCheckout(barePath, repoURL, checkoutPath, branchName, baseRef string) (string, error) {
	baseCommit, err := resolveCommit(barePath, baseRef)
	if err != nil {
		return "", err
	}

	if isIsolatedCheckout(checkoutPath) {
		if err := setIsolatedCheckoutOrigin(checkoutPath, repoURL); err != nil {
			return "", err
		}

		if isPartialClone(barePath) {
			if err := configurePromisorRemote(checkoutPath); err != nil {
				return "", err
			}
		}
		if err := syncIsolatedCheckoutRefs(barePath, checkoutPath, baseRef); err != nil {
			return "", err
		}
		actualBranch, err := updateExistingWorktree(checkoutPath, branchName, baseCommit)
		if err != nil {
			return "", err
		}

		if err := deleteStaleAgentBranches(checkoutPath, actualBranch); err != nil {
			c.logger.Warn("repo checkout: prune stale branches failed (non-fatal)", "error", err)
		}
		return actualBranch, nil
	}

	if isGitWorktree(checkoutPath) {
		if err := removeLinkedWorktree(barePath, checkoutPath); err != nil {
			return "", err
		}
	}
	if _, err := os.Stat(checkoutPath); err == nil {
		return "", fmt.Errorf("checkout path already exists and is not a Goosar isolated checkout: %s", checkoutPath)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("stat checkout path: %w", err)
	}

	return createIsolatedCheckout(barePath, repoURL, checkoutPath, branchName, baseRef, baseCommit)
}

func removeLinkedWorktree(barePath, checkoutPath string) error {
	out, err := runGitOutput("-C", checkoutPath, "rev-parse", "--git-common-dir")
	if err != nil {
		return fmt.Errorf("resolve linked worktree common dir: %w", err)
	}
	commonDir := strings.TrimSpace(string(out))
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(checkoutPath, commonDir)
	}
	if !sameResolvedPath(commonDir, barePath) {
		return fmt.Errorf("linked worktree common dir %s does not match cache %s", commonDir, barePath)
	}
	if out, err := runGitCombinedOutput("-C", barePath, "worktree", "remove", "--force", checkoutPath); err != nil {
		return fmt.Errorf("remove linked worktree: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func sameResolvedPath(a, b string) bool {
	clean := func(path string) string {
		abs, err := filepath.Abs(path)
		if err == nil {
			path = abs
		}
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			path = resolved
		}
		return filepath.Clean(path)
	}
	return clean(a) == clean(b)
}

func createIsolatedCheckout(barePath, repoURL, checkoutPath, branchName, baseRef, baseCommit string) (_ string, retErr error) {
	if out, err := runGitCombinedOutput(
		"clone", "--local", "--no-checkout", "--no-tags",
		"--origin", isolatedCacheRemoteName,
		barePath, checkoutPath,
	); err != nil {

		return "", fmt.Errorf("git clone --local: %s: %w", strings.TrimSpace(string(out)), err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(checkoutPath)
		}
	}()

	if out, err := runGitCombinedOutput("-C", checkoutPath, "remote", "remove", isolatedCacheRemoteName); err != nil {
		return "", fmt.Errorf("remove cache remote: %s: %w", strings.TrimSpace(string(out)), err)
	}
	if out, err := runGitCombinedOutput("-C", checkoutPath, "remote", "add", "origin", repoURL); err != nil {
		return "", fmt.Errorf("add origin remote: %s: %w", strings.TrimSpace(string(out)), err)
	}
	if isPartialClone(barePath) {
		if err := configurePromisorRemote(checkoutPath); err != nil {
			return "", err
		}
	}

	if out, err := runGitCombinedOutput("-C", checkoutPath, "checkout", "--detach", baseCommit); err != nil {
		return "", fmt.Errorf("git checkout --detach: %s: %w", strings.TrimSpace(string(out)), err)
	}
	if err := deleteAllLocalBranches(checkoutPath); err != nil {
		return "", err
	}
	if err := syncIsolatedCheckoutRefs(barePath, checkoutPath, baseRef); err != nil {
		return "", err
	}
	if out, err := runGitCombinedOutput("-C", checkoutPath, "config", isolatedCheckoutConfigKey, isolatedCheckoutConfigValue); err != nil {
		return "", fmt.Errorf("mark isolated checkout: %s: %w", strings.TrimSpace(string(out)), err)
	}

	actualBranch, err := checkoutNewBranch(checkoutPath, branchName, baseCommit)
	if err != nil {
		return "", err
	}
	cleanup = false
	return actualBranch, nil
}

func resolveCommit(repoPath, ref string) (string, error) {
	out, err := runGitOutput("-C", repoPath, "rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("resolve checkout base %q: %w", ref, err)
	}
	commit := strings.TrimSpace(string(out))
	if commit == "" {
		return "", fmt.Errorf("resolve checkout base %q: empty commit", ref)
	}
	return commit, nil
}

func isIsolatedCheckout(path string) bool {
	info, err := os.Stat(filepath.Join(path, ".git"))
	if err != nil || !info.IsDir() {
		return false
	}
	out, err := runGitOutput("-C", path, "config", "--get", isolatedCheckoutConfigKey)
	return err == nil && strings.TrimSpace(string(out)) == isolatedCheckoutConfigValue
}

const partialCloneFilter = "blob:none"

func isPartialClone(repoPath string) bool {
	out, err := runGitOutputWithTimeout(30*time.Second, "-C", repoPath, "config", "--get", "remote.origin.promisor")
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

func configurePromisorRemote(repoPath string) error {
	settings := [][2]string{
		{"remote.origin.promisor", "true"},
		{"remote.origin.partialclonefilter", partialCloneFilter},
	}
	for _, kv := range settings {
		if out, err := runGitCombinedOutput("-C", repoPath, "config", kv[0], kv[1]); err != nil {
			return fmt.Errorf("set %s: %s: %w", kv[0], strings.TrimSpace(string(out)), err)
		}
	}
	return nil
}

func setIsolatedCheckoutOrigin(path, repoURL string) error {
	out, err := runGitCombinedOutput("-C", path, "remote", "set-url", "origin", repoURL)
	if err != nil {
		return fmt.Errorf("set origin remote: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func syncIsolatedCheckoutRefs(barePath, checkoutPath, baseRef string) error {
	refspecs := []string{
		"+refs/remotes/origin/*:refs/remotes/origin/*",
		"+refs/tags/*:refs/tags/*",
	}
	args := []string{"-C", checkoutPath, "fetch", "--force", "--no-tags", barePath}
	args = append(args, refspecs...)
	if out, err := runGitCombinedOutput(args...); err != nil {
		return fmt.Errorf("sync cache refs: %s: %w", strings.TrimSpace(string(out)), err)
	}
	if out, err := runGitCombinedOutput("-C", checkoutPath, "fetch", "--force", "--no-tags", barePath, baseRef); err != nil {
		return fmt.Errorf("fetch checkout base: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func deleteAllLocalBranches(repoPath string) error {
	return deleteLocalBranchesUnder(repoPath, "refs/heads/", "")
}

func deleteStaleAgentBranches(repoPath, keepBranch string) error {
	return deleteLocalBranchesUnder(repoPath, "refs/heads/agent/", "refs/heads/"+keepBranch)
}

func deleteLocalBranchesUnder(repoPath, namespace, keepRef string) error {
	out, err := runGitOutput("-C", repoPath, "for-each-ref", "--format=%(refname)", namespace)
	if err != nil {
		return fmt.Errorf("list local branches: %w", err)
	}
	for _, ref := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		ref = strings.TrimSpace(ref)
		if ref == "" || ref == keepRef {
			continue
		}
		if out, err := runGitCombinedOutput("-C", repoPath, "update-ref", "-d", ref); err != nil {
			return fmt.Errorf("delete local branch %s: %s: %w", ref, strings.TrimSpace(string(out)), err)
		}
	}
	return nil
}

func checkoutNewBranch(repoPath, branchName, baseRef string) (string, error) {
	out, err := runGitCombinedOutput("-C", repoPath, "checkout", "-b", branchName, baseRef)
	if err == nil {
		return branchName, nil
	}
	wrapped := fmt.Errorf("git checkout -b: %s: %w", strings.TrimSpace(string(out)), err)
	if !isBranchCollisionError(wrapped) {
		return "", wrapped
	}
	branchName = fmt.Sprintf("%s-%d", branchName, time.Now().Unix())
	if out2, err2 := runGitCombinedOutput("-C", repoPath, "checkout", "-b", branchName, baseRef); err2 != nil {
		return "", fmt.Errorf("git checkout -b (retry): %s: %w", strings.TrimSpace(string(out2)), err2)
	}
	return branchName, nil
}

func resolveBaseRef(barePath, requestedRef string) (string, error) {
	ref := strings.TrimSpace(requestedRef)
	if ref == "" {
		return getRemoteDefaultBranch(barePath), nil
	}

	candidates := []string{
		"refs/remotes/origin/" + ref,
		"refs/tags/" + ref,
		ref,
	}
	for _, candidate := range candidates {
		if gitRefExists(barePath, candidate+"^{commit}") {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("cannot resolve requested ref %q in repo cache at %s", ref, barePath)
}

func gitRefExists(repoPath, ref string) bool {
	return runGit("-C", repoPath, "rev-parse", "--verify", "--quiet", ref) == nil
}

func createWorktree(gitRoot, worktreePath, branchName, baseRef string) (string, error) {

	if _, err := os.Stat(worktreePath); err == nil {
		return "", fmt.Errorf("worktree path already exists and is not a valid git worktree: %s", worktreePath)
	}

	err := runWorktreeAdd(gitRoot, worktreePath, branchName, baseRef)
	if err != nil && isBranchCollisionError(err) {

		branchName = fmt.Sprintf("%s-%d", branchName, time.Now().Unix())
		err = runWorktreeAdd(gitRoot, worktreePath, branchName, baseRef)
	}
	if err != nil {
		return "", err
	}
	return branchName, nil
}

func runWorktreeAdd(gitRoot, worktreePath, branchName, baseRef string) error {
	if out, err := runGitCombinedOutput("-C", gitRoot, "worktree", "add", "-b", branchName, worktreePath, baseRef); err != nil {
		return fmt.Errorf("git worktree add: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func isBranchCollisionError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())

	return strings.Contains(msg, "a branch named")
}

func isGitWorktree(path string) bool {
	info, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil && !info.IsDir()
}

func updateExistingWorktree(worktreePath, branchName, baseRef string) (string, error) {

	if out, err := runGitCombinedOutput("-C", worktreePath, "reset", "--hard"); err != nil {
		return "", fmt.Errorf("git reset --hard: %s: %w", strings.TrimSpace(string(out)), err)
	}

	if out, err := runGitCombinedOutput("-C", worktreePath, "clean", "-fd"); err != nil {
		return "", fmt.Errorf("git clean -fd: %s: %w", strings.TrimSpace(string(out)), err)
	}

	out, err := runGitCombinedOutput("-C", worktreePath, "checkout", "-b", branchName, baseRef)
	if err == nil {
		return branchName, nil
	}
	wrapped := fmt.Errorf("git checkout -b: %s: %w", strings.TrimSpace(string(out)), err)
	if !isBranchCollisionError(wrapped) {
		return "", wrapped
	}

	branchName = fmt.Sprintf("%s-%d", branchName, time.Now().Unix())
	if out2, err2 := runGitCombinedOutput("-C", worktreePath, "checkout", "-b", branchName, baseRef); err2 != nil {
		return "", fmt.Errorf("git checkout -b (retry): %s: %w", strings.TrimSpace(string(out2)), err2)
	}
	return branchName, nil
}

func getRemoteDefaultBranch(barePath string) string {

	if out, err := runGitOutput("-C", barePath, "symbolic-ref", "refs/remotes/origin/HEAD"); err == nil {
		ref := strings.TrimSpace(string(out))
		if ref != "" {
			if err := runGit("-C", barePath, "rev-parse", "--verify", ref); err == nil {
				return ref
			}
		}
	}

	for _, candidate := range []string{"refs/remotes/origin/main", "refs/remotes/origin/master"} {
		if err := runGit("-C", barePath, "rev-parse", "--verify", candidate); err == nil {
			return candidate
		}
	}

	bareRef := bareHeadBranch(barePath)
	if bareRef != "" {
		originRef := "refs/remotes/origin/" + strings.TrimPrefix(bareRef, "refs/heads/")
		if err := runGit("-C", barePath, "rev-parse", "--verify", originRef); err == nil {
			return originRef
		}
	}

	originCount := 0
	var singleton string
	if out, err := runGitOutput("-C", barePath, "for-each-ref", "--format=%(refname)", "refs/remotes/origin/"); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || line == "refs/remotes/origin/HEAD" {
				continue
			}
			originCount++
			if singleton == "" {
				singleton = line
			}
		}
		if originCount == 1 {
			return singleton
		}
	}

	if originCount == 0 && bareRef != "" {
		return bareRef
	}
	return ""
}

func bareHeadBranch(barePath string) string {
	out, err := runGitOutput("-C", barePath, "symbolic-ref", "HEAD")
	if err != nil {
		return ""
	}
	ref := strings.TrimSpace(string(out))
	if ref == "" {
		return ""
	}
	if err := runGit("-C", barePath, "rev-parse", "--verify", ref); err != nil {
		return ""
	}
	return ref
}

const goosarHookMarker = "# goosar:prepare-commit-msg:co-authored-by"

var daemonInstalledHookSignatures = []string{
	goosarHookMarker,
	"# Installed by the Goosar daemon.",
}

const prepareCommitMsgHook = `#!/bin/sh
# goosar:prepare-commit-msg:co-authored-by
# Goosar: add Co-authored-by trailer for the Goosar Agent.
# Installed by the Goosar daemon. Do not edit — it will be overwritten.

COMMIT_MSG_FILE="$1"
COMMIT_SOURCE="$2"

# Skip merge and squash commits.
case "$COMMIT_SOURCE" in
  merge|squash) exit 0 ;;
esac

TRAILER="Co-authored-by: goosar-agent <github@goosar.ru>"

# Don't add if already present.
if grep -qF "$TRAILER" "$COMMIT_MSG_FILE"; then
  exit 0
fi

# Use git interpret-trailers for proper formatting.
git interpret-trailers --in-place --trailer "$TRAILER" "$COMMIT_MSG_FILE"
`

func installCoAuthoredByHook(worktreePath string) error {
	out, err := runGitOutput("-C", worktreePath, "rev-parse", "--git-common-dir")
	if err != nil {
		return fmt.Errorf("resolve git common dir: %w", err)
	}
	commonDir := strings.TrimSpace(string(out))
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(worktreePath, commonDir)
	}

	hooksDir := filepath.Join(commonDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return fmt.Errorf("create hooks dir: %w", err)
	}

	hookPath := filepath.Join(hooksDir, "prepare-commit-msg")
	if err := os.WriteFile(hookPath, []byte(prepareCommitMsgHook), 0o755); err != nil {
		return fmt.Errorf("write prepare-commit-msg hook: %w", err)
	}
	return nil
}

func isDaemonInstalledHook(contents []byte) bool {
	body := string(contents)
	for _, sig := range daemonInstalledHookSignatures {
		if strings.Contains(body, sig) {
			return true
		}
	}
	return false
}

func removeCoAuthoredByHook(worktreePath string) error {
	out, err := runGitOutput("-C", worktreePath, "rev-parse", "--git-common-dir")
	if err != nil {
		return fmt.Errorf("resolve git common dir: %w", err)
	}
	commonDir := strings.TrimSpace(string(out))
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(worktreePath, commonDir)
	}

	hookPath := filepath.Join(commonDir, "hooks", "prepare-commit-msg")
	contents, err := os.ReadFile(hookPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read prepare-commit-msg hook: %w", err)
	}
	if !isDaemonInstalledHook(contents) {

		return nil
	}
	if err := os.Remove(hookPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove prepare-commit-msg hook: %w", err)
	}
	return nil
}

func excludeFromGit(worktreePath, pattern string) error {
	out, err := runGitOutput("-C", worktreePath, "rev-parse", "--git-dir")
	if err != nil {
		return fmt.Errorf("resolve git dir: %w", err)
	}

	gitDir := strings.TrimSpace(string(out))
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(worktreePath, gitDir)
	}

	excludePath := filepath.Join(gitDir, "info", "exclude")

	if err := os.MkdirAll(filepath.Dir(excludePath), 0o755); err != nil {
		return fmt.Errorf("create info dir: %w", err)
	}

	existing, _ := os.ReadFile(excludePath)
	if strings.Contains(string(existing), pattern) {
		return nil
	}

	f, err := os.OpenFile(excludePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open exclude file: %w", err)
	}
	defer f.Close()

	if _, err := fmt.Fprintf(f, "\n%s\n", pattern); err != nil {
		return fmt.Errorf("write exclude pattern: %w", err)
	}
	return nil
}

func repoNameFromURL(url string) string {
	url = strings.TrimRight(url, "/")
	url = strings.TrimSuffix(url, ".git")

	if i := strings.LastIndex(url, "/"); i >= 0 {
		url = url[i+1:]
	}
	if i := strings.LastIndex(url, ":"); i >= 0 {
		url = url[i+1:]
		if j := strings.LastIndex(url, "/"); j >= 0 {
			url = url[j+1:]
		}
	}

	name := strings.TrimSpace(url)
	if name == "" {
		return "repo"
	}
	return name
}

var nonAlphanumeric = regexp.MustCompile(`[^a-z0-9]+`)

func sanitizeName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = nonAlphanumeric.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 30 {
		s = s[:30]
		s = strings.TrimRight(s, "-")
	}
	if s == "" {
		s = "agent"
	}
	return s
}

const taskKeyLen = 12

func taskKey(uuid string) string {
	s := strings.ReplaceAll(uuid, "-", "")
	if len(s) > taskKeyLen {
		return s[len(s)-taskKeyLen:]
	}
	return s
}

func shortID(uuid string) string {
	s := strings.ReplaceAll(uuid, "-", "")
	if len(s) > 8 {
		return s[:8]
	}
	return s
}
