package repocache

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.Default()
}

func TestGitEnv(t *testing.T) {
	t.Parallel()
	env := gitEnv()

	found := false
	for _, entry := range env {
		if entry == "GIT_TERMINAL_PROMPT=0" {
			found = true
			break
		}
	}
	if !found {
		t.Error("gitEnv() must include GIT_TERMINAL_PROMPT=0")
	}

	home := os.Getenv("HOME")
	if home == "" {
		t.Skip("HOME not set in test environment")
	}
	foundHome := false
	for _, entry := range env {
		if entry == "HOME="+home {
			foundHome = true
			break
		}
	}
	if !foundHome {
		t.Error("gitEnv() must include HOME from os.Environ()")
	}

	envHas := func(env []string, want string) bool {
		for _, e := range env {
			if e == want {
				return true
			}
		}
		return false
	}
	if !envHas(env, "GIT_CONFIG_KEY_0=safe.directory") {
		t.Error("gitEnv() must include GIT_CONFIG_KEY_0=safe.directory (no pre-existing config)")
	}
	if !envHas(env, "GIT_CONFIG_VALUE_0=*") {
		t.Error("gitEnv() must include GIT_CONFIG_VALUE_0=*")
	}
}

func TestGitEnvPreservesExistingConfig(t *testing.T) {

	t.Setenv("GIT_CONFIG_COUNT", "2")
	t.Setenv("GIT_CONFIG_KEY_0", "url.https://github.com/.insteadOf")
	t.Setenv("GIT_CONFIG_VALUE_0", "gh:")
	t.Setenv("GIT_CONFIG_KEY_1", "http.extraHeader")
	t.Setenv("GIT_CONFIG_VALUE_1", "Authorization: Bearer tok")

	env := gitEnv()

	envHas := func(want string) bool {
		for _, e := range env {
			if e == want {
				return true
			}
		}
		return false
	}

	if !envHas("GIT_CONFIG_COUNT=3") {
		t.Error("expected GIT_CONFIG_COUNT=3")
	}
	if !envHas("GIT_CONFIG_KEY_2=safe.directory") {
		t.Error("expected GIT_CONFIG_KEY_2=safe.directory")
	}
	if !envHas("GIT_CONFIG_VALUE_2=*") {
		t.Error("expected GIT_CONFIG_VALUE_2=*")
	}

	if !envHas("GIT_CONFIG_KEY_0=url.https://github.com/.insteadOf") {
		t.Error("existing GIT_CONFIG_KEY_0 was lost")
	}
	if !envHas("GIT_CONFIG_VALUE_0=gh:") {
		t.Error("existing GIT_CONFIG_VALUE_0 was lost")
	}
	if !envHas("GIT_CONFIG_KEY_1=http.extraHeader") {
		t.Error("existing GIT_CONFIG_KEY_1 was lost")
	}
}

func TestRunGitOutputTimesOut(t *testing.T) {
	_, err := runGitOutputWithTimeout(0, "--version")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("runGitOutputWithTimeout error = %v, want deadline exceeded", err)
	}
	if !strings.Contains(err.Error(), "timed out after 0s") {
		t.Fatalf("runGitOutputWithTimeout error = %v, want timeout context", err)
	}
}

func TestBareDirName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input, want string
	}{
		{"https://github.com/org/my-repo.git", "github.com+org+my-repo.git"},
		{"https://github.com/org/my-repo", "github.com+org+my-repo.git"},
		{"git@github.com:org/my-repo.git", "github.com+org+my-repo.git"},
		{"git@github.com:org/my-repo", "github.com+org+my-repo.git"},
		{"https://github.com/org/repo/", "github.com+org+repo.git"},
		{"ssh://git@gitlab.example.com:22/group/sub/repo.git", "gitlab.example.com%3A22+group+sub+repo.git"},

		{"ssh://git@gitlab.example.com:22/relisty/app.git", "gitlab.example.com%3A22+relisty+app.git"},
		{"ssh://git@gitlab.example.com:22/listbridge/app.git", "gitlab.example.com%3A22+listbridge+app.git"},
		{"my-repo", "my-repo.git"},
		{"", "repo.git"},
	}
	for _, tt := range tests {
		if got := bareDirName(tt.input); got != tt.want {
			t.Errorf("bareDirName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestBareDirNameDistinctsSegmentBoundaryColliders(t *testing.T) {
	t.Parallel()
	pairs := [][2]string{
		{"git@github.com:foo/bar-baz.git", "git@github.com:foo-bar/baz.git"},
		{"https://github.com/foo/bar-baz.git", "https://github.com/foo-bar/baz.git"},
	}
	for _, p := range pairs {
		a, b := bareDirName(p[0]), bareDirName(p[1])
		if a == b {
			t.Errorf("bareDirName collision: %q and %q both → %q", p[0], p[1], a)
		}
	}
}

func TestBareDirNameDistinctsSameRepoNameAcrossHosts(t *testing.T) {
	t.Parallel()
	pairs := [][2]string{
		{"git@github.com:org/repo.git", "git@gitlab.example.com:org/repo.git"},
		{"https://github.com/org/repo.git", "https://gitlab.example.com/org/repo.git"},
		{"ssh://git@github.com/org/repo.git", "ssh://git@gitlab.example.com/org/repo.git"},
	}
	for _, p := range pairs {
		a, b := bareDirName(p[0]), bareDirName(p[1])
		if a == b {
			t.Errorf("bareDirName collision across hosts: %q and %q both → %q", p[0], p[1], a)
		}
	}
}

func TestBareDirNameDistinctsHostPortFromDashedHostname(t *testing.T) {
	t.Parallel()
	pairs := [][2]string{

		{"ssh://git@gitlab.example.com:22/org/repo.git", "git@gitlab.example.com-22:org/repo.git"},

		{"ssh://git@host.example.com:443/a/b.git", "git@host.example.com-443:a/b.git"},
	}
	for _, p := range pairs {
		a, b := bareDirName(p[0]), bareDirName(p[1])
		if a == b {
			t.Errorf("bareDirName collision between host:port and host-port: %q and %q both → %q", p[0], p[1], a)
		}
	}
}

func TestIsBareRepo(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644)
	if !isBareRepo(dir) {
		t.Error("expected bare repo to be detected")
	}

	emptyDir := t.TempDir()
	if isBareRepo(emptyDir) {
		t.Error("expected empty dir to not be detected as bare repo")
	}
}

func createTestRepo(t *testing.T) string {
	t.Helper()
	return createTestRepoAt(t, t.TempDir())
}

func createTestRepoAt(t *testing.T, dir string) string {
	t.Helper()
	for _, args := range [][]string{
		{"init", dir},
		{"-C", dir, "commit", "--allow-empty", "-m", "initial"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git setup failed: %s: %v", out, err)
		}
	}
	return dir
}

func TestSyncAndLookup(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()

	cache := New(cacheRoot, testLogger())

	err := cache.Sync("ws-123", []RepoInfo{
		{URL: sourceRepo},
	})
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	path := cache.Lookup("ws-123", sourceRepo)
	if path == "" {
		t.Fatal("expected to find cached repo")
	}
	if !isBareRepo(path) {
		t.Fatalf("expected bare repo at %s", path)
	}

	if got := cache.Lookup("ws-123", "https://github.com/org/unknown"); got != "" {
		t.Fatalf("expected empty for unknown URL, got %q", got)
	}

	if got := cache.Lookup("ws-999", sourceRepo); got != "" {
		t.Fatalf("expected empty for unknown workspace, got %q", got)
	}
}

func TestSyncKeepsDistinctCachesForSegmentBoundaryColliders(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	srcA := filepath.Join(parent, "foo", "bar-baz")
	srcB := filepath.Join(parent, "foo-bar", "baz")
	if err := os.MkdirAll(srcA, 0o755); err != nil {
		t.Fatalf("mkdir srcA: %v", err)
	}
	if err := os.MkdirAll(srcB, 0o755); err != nil {
		t.Fatalf("mkdir srcB: %v", err)
	}
	createTestRepoAt(t, srcA)
	createTestRepoAt(t, srcB)

	if err := os.WriteFile(filepath.Join(srcA, "A.txt"), []byte("A\n"), 0o644); err != nil {
		t.Fatalf("write A: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcB, "B.txt"), []byte("B\n"), 0o644); err != nil {
		t.Fatalf("write B: %v", err)
	}
	runGitAuthored(t, srcA, "add", ".")
	runGitAuthored(t, srcA, "commit", "-m", "A-content")
	runGitAuthored(t, srcB, "add", ".")
	runGitAuthored(t, srcB, "commit", "-m", "B-content")

	cache := New(t.TempDir(), testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: srcA}, {URL: srcB}}); err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	pathA := cache.Lookup("ws-1", srcA)
	pathB := cache.Lookup("ws-1", srcB)
	if pathA == "" || pathB == "" {
		t.Fatalf("missing cache entry: A=%q B=%q", pathA, pathB)
	}
	if pathA == pathB {
		t.Fatalf("collider URLs share a bare cache path: %s", pathA)
	}

	if got := gitConfigGet(t, pathA, "remote.origin.url"); got != srcA {
		t.Errorf("cacheA origin.url = %q, want %q", got, srcA)
	}
	if got := gitConfigGet(t, pathB, "remote.origin.url"); got != srcB {
		t.Errorf("cacheB origin.url = %q, want %q", got, srcB)
	}

	if !cachedRepoHasFile(t, pathA, "A.txt") {
		t.Errorf("cacheA (%s) should contain A.txt from srcA", pathA)
	}
	if !cachedRepoHasFile(t, pathB, "B.txt") {
		t.Errorf("cacheB (%s) should contain B.txt from srcB", pathB)
	}
}

func gitConfigGet(t *testing.T, repoPath, key string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", repoPath, "config", "--get", key).Output()
	if err != nil {
		t.Fatalf("git config --get %s in %s: %v", key, repoPath, err)
	}
	return strings.TrimSpace(string(out))
}

func cachedRepoHasFile(t *testing.T, barePath, filename string) bool {
	t.Helper()
	ref := getRemoteDefaultBranch(barePath)
	if ref == "" {
		return false
	}
	out, err := exec.Command("git", "-C", barePath, "ls-tree", "-r", "--name-only", ref).Output()
	if err != nil {
		t.Fatalf("git ls-tree %s in %s: %v", ref, barePath, err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(line) == filename {
			return true
		}
	}
	return false
}

func TestSyncFetchesExisting(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()

	cache := New(cacheRoot, testLogger())

	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("first sync failed: %v", err)
	}

	barePath := cache.Lookup("ws-1", sourceRepo)
	oldHead := gitRefCommit(t, barePath, getRemoteDefaultBranch(barePath))

	addEmptyCommit(t, sourceRepo, "second")
	sourceHead := gitHead(t, sourceRepo)
	if sourceHead == oldHead {
		t.Fatal("source HEAD should differ after new commit")
	}

	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("second sync failed: %v", err)
	}

	newHead := gitRefCommit(t, barePath, getRemoteDefaultBranch(barePath))
	if newHead == oldHead {
		t.Fatal("expected cache remote-tracking head to be updated after fetch")
	}
	if newHead != sourceHead {
		t.Fatalf("expected cache head %s to match source head %s", newHead, sourceHead)
	}
}

func gitHead(t *testing.T, repoPath string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", repoPath, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD failed in %s: %v", repoPath, err)
	}
	return strings.TrimSpace(string(out))
}

func TestWorktreeFromCache(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()

	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	barePath := cache.Lookup("ws-1", sourceRepo)
	if barePath == "" {
		t.Fatal("expected cached repo")
	}

	worktreeDir := filepath.Join(t.TempDir(), "work")
	cmd := exec.Command("git", "-C", barePath, "worktree", "add", "-b", "test-branch", worktreeDir, "HEAD")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("worktree add failed: %s: %v", out, err)
	}
	defer exec.Command("git", "-C", barePath, "worktree", "remove", "--force", worktreeDir).Run()

	cmd = exec.Command("git", "-C", worktreeDir, "branch", "--show-current")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("show branch failed: %v", err)
	}
	if got := trimLine(string(out)); got != "test-branch" {
		t.Fatalf("expected branch 'test-branch', got %q", got)
	}
}

func TestCreateWorktree(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()

	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	workDir := t.TempDir()
	result, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID: "ws-1",
		RepoURL:     sourceRepo,
		WorkDir:     workDir,
		AgentName:   "Code Reviewer",
		TaskID:      "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
	})
	if err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}

	if _, err := os.Stat(result.Path); os.IsNotExist(err) {
		t.Fatalf("worktree path does not exist: %s", result.Path)
	}

	if !strings.HasPrefix(result.BranchName, "agent/code-reviewer/") {
		t.Errorf("expected branch to start with 'agent/code-reviewer/', got %q", result.BranchName)
	}

	cmd := exec.Command("git", "-C", result.Path, "branch", "--show-current")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("show branch failed: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != result.BranchName {
		t.Errorf("expected branch %q, got %q", result.BranchName, got)
	}
}

func TestCreateWorktreeWithIsolatedGitMetadata(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cache := New(t.TempDir(), testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	barePath := cache.Lookup("ws-1", sourceRepo)
	baseRef := getRemoteDefaultBranch(barePath)
	baseCommit := gitRefCommit(t, barePath, baseRef)
	workDir := t.TempDir()
	result, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID:         "ws-1",
		RepoURL:             sourceRepo,
		WorkDir:             workDir,
		AgentName:           "Linux Codex",
		TaskID:              "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
		IsolatedGitMetadata: true,
	})
	if err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}

	gitInfo, err := os.Stat(filepath.Join(result.Path, ".git"))
	if err != nil || !gitInfo.IsDir() {
		t.Fatalf("isolated checkout .git must be a directory, info=%v err=%v", gitInfo, err)
	}
	if !isIsolatedCheckout(result.Path) {
		t.Fatal("isolated checkout marker missing")
	}
	if _, err := os.Stat(filepath.Join(result.Path, ".git", "objects", "info", "alternates")); !os.IsNotExist(err) {
		t.Fatalf("isolated checkout must not depend on cache alternates, err=%v", err)
	}

	origin, err := runGitOutput("-C", result.Path, "remote", "get-url", "origin")
	if err != nil {
		t.Fatalf("get origin URL: %v", err)
	}
	if got := strings.TrimSpace(string(origin)); got != sourceRepo {
		t.Fatalf("origin URL = %q, want %q", got, sourceRepo)
	}
	if got := gitHead(t, result.Path); got != baseCommit {
		t.Fatalf("checkout HEAD = %s, want %s", got, baseCommit)
	}
	if err := runGit("-C", result.Path, "rev-parse", "--verify", baseRef); err != nil {
		t.Fatalf("isolated checkout missing cache ref %s: %v", baseRef, err)
	}
	if err := runGit("-C", barePath, "show-ref", "--verify", "refs/heads/"+result.BranchName); err == nil {
		t.Fatalf("isolated branch %s leaked into shared bare cache", result.BranchName)
	}

	if err := os.WriteFile(filepath.Join(result.Path, "isolated.txt"), []byte("writable\n"), 0o644); err != nil {
		t.Fatalf("write isolated file: %v", err)
	}
	runGitAuthored(t, result.Path, "add", "isolated.txt")
	runGitAuthored(t, result.Path, "commit", "-m", "isolated commit")
	if got := gitHead(t, result.Path); got == baseCommit {
		t.Fatal("git commit did not advance isolated checkout")
	}
	if got := gitRefCommit(t, barePath, baseRef); got != baseCommit {
		t.Fatalf("isolated commit mutated shared cache base: got %s, want %s", got, baseCommit)
	}
}

func TestCreateWorktreeReusesIsolatedGitMetadata(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cache := New(t.TempDir(), testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("initial sync failed: %v", err)
	}

	workDir := t.TempDir()
	first, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID:         "ws-1",
		RepoURL:             sourceRepo,
		WorkDir:             workDir,
		AgentName:           "Linux Codex",
		TaskID:              "11111111-1111-1111-1111-111111111111",
		IsolatedGitMetadata: true,
	})
	if err != nil {
		t.Fatalf("first CreateWorktree failed: %v", err)
	}
	const userBranch = "feature/keep-me"
	runGitAuthored(t, first.Path, "checkout", "-b", userBranch)
	addEmptyCommit(t, first.Path, "unpublished feature work")
	userCommit := gitHead(t, first.Path)

	addEmptyCommit(t, sourceRepo, "upstream advance")
	wantHead := gitHead(t, sourceRepo)
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("refresh sync failed: %v", err)
	}

	second, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID:         "ws-1",
		RepoURL:             sourceRepo,
		WorkDir:             workDir,
		AgentName:           "Linux Codex",
		TaskID:              "22222222-2222-2222-2222-222222222222",
		IsolatedGitMetadata: true,
	})
	if err != nil {
		t.Fatalf("second CreateWorktree failed: %v", err)
	}
	if second.Path != first.Path {
		t.Fatalf("reused checkout path = %q, want %q", second.Path, first.Path)
	}
	if second.BranchName == first.BranchName {
		t.Fatalf("reused checkout kept old branch %q", first.BranchName)
	}
	if got := gitHead(t, second.Path); got != wantHead {
		t.Fatalf("reused checkout HEAD = %s, want refreshed upstream %s", got, wantHead)
	}

	if err := runGit("-C", second.Path, "show-ref", "--verify", "refs/heads/"+first.BranchName); err == nil {
		t.Fatalf("stale branch %s survived reuse", first.BranchName)
	}
	if got := gitRefCommit(t, second.Path, "refs/heads/"+userBranch); got != userCommit {
		t.Fatalf("preserved user branch commit = %s, want %s", got, userCommit)
	}
	heads, err := runGitOutput("-C", second.Path, "for-each-ref", "--format=%(refname)", "refs/heads/")
	if err != nil {
		t.Fatalf("list local heads: %v", err)
	}
	wantHeads := "refs/heads/" + second.BranchName + "\nrefs/heads/" + userBranch
	if got := strings.TrimSpace(string(heads)); got != wantHeads {
		t.Fatalf("reused checkout local heads = %q, want %q", got, wantHeads)
	}
}

func TestCreateWorktreeMigratesLinkedWorktreeToIsolatedMetadata(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cache := New(t.TempDir(), testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	workDir := t.TempDir()
	linked, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID: "ws-1",
		RepoURL:     sourceRepo,
		WorkDir:     workDir,
		AgentName:   "Linux Codex",
		TaskID:      "11111111-1111-1111-1111-111111111111",
	})
	if err != nil {
		t.Fatalf("linked CreateWorktree failed: %v", err)
	}
	if !isGitWorktree(linked.Path) {
		t.Fatal("test setup did not create a linked worktree")
	}

	isolated, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID:         "ws-1",
		RepoURL:             sourceRepo,
		WorkDir:             workDir,
		AgentName:           "Linux Codex",
		TaskID:              "22222222-2222-2222-2222-222222222222",
		IsolatedGitMetadata: true,
	})
	if err != nil {
		t.Fatalf("isolated CreateWorktree failed: %v", err)
	}
	if isolated.Path != linked.Path {
		t.Fatalf("migrated checkout path = %q, want %q", isolated.Path, linked.Path)
	}
	if !isIsolatedCheckout(isolated.Path) {
		t.Fatal("linked worktree was not migrated to isolated metadata")
	}

	barePath := cache.Lookup("ws-1", sourceRepo)
	out, err := runGitOutput("-C", barePath, "worktree", "list", "--porcelain")
	if err != nil {
		t.Fatalf("list cache worktrees: %v", err)
	}
	if strings.Contains(string(out), "worktree "+linked.Path+"\n") {
		t.Fatalf("shared cache retained migrated worktree admin entry:\n%s", out)
	}
}

func TestCreateWorktreeExcludesOpenCodeSkills(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()

	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	workDir := t.TempDir()
	result, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID: "ws-1",
		RepoURL:     sourceRepo,
		WorkDir:     workDir,
		AgentName:   "OpenCode",
		TaskID:      "opencode-exclude-test",
	})
	if err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}

	exclude := gitInfoExclude(t, result.Path)
	if !strings.Contains(exclude, ".opencode\n") {
		t.Fatalf("expected .git/info/exclude to contain .opencode, got:\n%s", exclude)
	}
	if strings.Contains(exclude, ".config/opencode") {
		t.Fatalf("expected .git/info/exclude to not contain stale .config/opencode, got:\n%s", exclude)
	}
}

func TestCreateWorktreeExcludesCodebuddySidecars(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()

	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	workDir := t.TempDir()
	result, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID: "ws-1",
		RepoURL:     sourceRepo,
		WorkDir:     workDir,
		AgentName:   "CodeBuddy",
		TaskID:      "codebuddy-exclude-test",
	})
	if err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}

	exclude := gitInfoExclude(t, result.Path)
	if !strings.Contains(exclude, ".codebuddy\n") {
		t.Fatalf("expected .git/info/exclude to contain .codebuddy, got:\n%s", exclude)
	}
	if !strings.Contains(exclude, "CODEBUDDY.md\n") {
		t.Fatalf("expected .git/info/exclude to contain CODEBUDDY.md, got:\n%s", exclude)
	}
}

func gitInfoExclude(t *testing.T, worktreePath string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", worktreePath, "rev-parse", "--git-dir")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse --git-dir failed in %s: %v", worktreePath, err)
	}
	gitDir := strings.TrimSpace(string(out))
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(worktreePath, gitDir)
	}
	data, err := os.ReadFile(filepath.Join(gitDir, "info", "exclude"))
	if err != nil {
		t.Fatalf("read .git/info/exclude failed: %v", err)
	}
	return string(data)
}

func TestCreateWorktreeNotCached(t *testing.T) {
	t.Parallel()
	cacheRoot := t.TempDir()
	cache := New(cacheRoot, testLogger())

	_, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID: "ws-1",
		RepoURL:     "https://github.com/org/nonexistent",
		WorkDir:     t.TempDir(),
		AgentName:   "Agent",
		TaskID:      "test-task-id",
	})
	if err == nil {
		t.Fatal("expected error for uncached repo")
	}
	if !strings.Contains(err.Error(), "not found in cache") {
		t.Errorf("expected 'not found in cache' error, got: %v", err)
	}
}

func TestCreateWorktreeWithRequestedBranchRef(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	defaultHead := gitHead(t, sourceRepo)

	runGitAuthored(t, sourceRepo, "checkout", "-b", "review-branch")
	if err := os.WriteFile(filepath.Join(sourceRepo, "review.txt"), []byte("review\n"), 0o644); err != nil {
		t.Fatalf("write review file: %v", err)
	}
	runGitAuthored(t, sourceRepo, "add", ".")
	runGitAuthored(t, sourceRepo, "commit", "-m", "review branch commit")
	reviewHead := gitHead(t, sourceRepo)
	if reviewHead == defaultHead {
		t.Fatal("test setup failed: review branch did not advance")
	}

	cache := New(t.TempDir(), testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	result, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID: "ws-1",
		RepoURL:     sourceRepo,
		WorkDir:     t.TempDir(),
		Ref:         "review-branch",
		AgentName:   "Reviewer",
		TaskID:      "review-task-id",
	})
	if err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}

	if got := gitHead(t, result.Path); got != reviewHead {
		t.Fatalf("worktree HEAD = %s, want requested branch head %s", got, reviewHead)
	}
	if _, err := os.Stat(filepath.Join(result.Path, "review.txt")); err != nil {
		t.Fatalf("requested branch file missing: %v", err)
	}
}

func TestCreateWorktreeWithRequestedCommitRef(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	firstCommit := gitHead(t, sourceRepo)
	addEmptyCommit(t, sourceRepo, "second commit")

	cache := New(t.TempDir(), testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	result, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID: "ws-1",
		RepoURL:     sourceRepo,
		WorkDir:     t.TempDir(),
		Ref:         firstCommit,
		AgentName:   "Reviewer",
		TaskID:      "commit-task-id",
	})
	if err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}

	if got := gitHead(t, result.Path); got != firstCommit {
		t.Fatalf("worktree HEAD = %s, want requested commit %s", got, firstCommit)
	}
}

func TestCreateWorktreeWithRequestedTagRef(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	taggedCommit := gitHead(t, sourceRepo)
	runGitAuthored(t, sourceRepo, "tag", "v1")

	addEmptyCommit(t, sourceRepo, "post-tag commit")

	cache := New(t.TempDir(), testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	result, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID: "ws-1",
		RepoURL:     sourceRepo,
		WorkDir:     t.TempDir(),
		Ref:         "v1",
		AgentName:   "Reviewer",
		TaskID:      "tag-task-id",
	})
	if err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}

	if got := gitHead(t, result.Path); got != taggedCommit {
		t.Fatalf("worktree HEAD = %s, want tagged commit %s", got, taggedCommit)
	}
}

func TestCreateWorktreeWithUnknownRequestedRef(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cache := New(t.TempDir(), testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	_, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID: "ws-1",
		RepoURL:     sourceRepo,
		WorkDir:     t.TempDir(),
		Ref:         "missing-ref",
		AgentName:   "Reviewer",
		TaskID:      "missing-ref-task-id",
	})
	if err == nil {
		t.Fatal("expected unknown ref error")
	}
	if !strings.Contains(err.Error(), "cannot resolve requested ref") {
		t.Fatalf("expected requested ref error, got: %v", err)
	}
}

func trimLine(s string) string {
	return strings.TrimSpace(s)
}

func gitRefCommit(t *testing.T, repoPath, ref string) string {
	t.Helper()
	if ref == "" {
		t.Fatalf("empty ref in %s", repoPath)
	}
	cmd := exec.Command("git", "-C", repoPath, "rev-parse", ref)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse %s failed in %s: %v", ref, repoPath, err)
	}
	return strings.TrimSpace(string(out))
}

func addEmptyCommit(t *testing.T, repoPath, message string) {
	t.Helper()
	cmd := exec.Command("git", "-C", repoPath, "commit", "--allow-empty", "-m", message)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit failed in %s: %s: %v", repoPath, out, err)
	}
}

func runGitAuthored(t *testing.T, repoPath string, args ...string) {
	t.Helper()
	full := append([]string{"-C", repoPath}, args...)
	cmd := exec.Command("git", full...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %s: %v", args, repoPath, out, err)
	}
}

func TestCreateWorktreeFetchesDespiteAgentBranchOnRemote(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)

	defaultBranch := currentBranchName(t, sourceRepo)

	runGitAuthored(t, sourceRepo, "checkout", "--detach", "HEAD")

	cacheRoot := t.TempDir()
	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	workDir1 := t.TempDir()
	result1, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID: "ws-1",
		RepoURL:     sourceRepo,
		WorkDir:     workDir1,
		AgentName:   "agent",
		TaskID:      "t1111111-0000-0000-0000-000000000000",
	})
	if err != nil {
		t.Fatalf("first CreateWorktree failed: %v", err)
	}

	if err := os.WriteFile(filepath.Join(result1.Path, "hello.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGitAuthored(t, result1.Path, "add", ".")
	runGitAuthored(t, result1.Path, "commit", "-m", "first task")
	runGitAuthored(t, result1.Path, "push", "origin", result1.BranchName)

	runGitAuthored(t, sourceRepo, "checkout", defaultBranch)
	addEmptyCommit(t, sourceRepo, "new commit on default branch")
	sourceHead := gitRefCommit(t, sourceRepo, "refs/heads/"+defaultBranch)
	runGitAuthored(t, sourceRepo, "checkout", "--detach", "HEAD")

	workDir2 := t.TempDir()
	result2, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID: "ws-1",
		RepoURL:     sourceRepo,
		WorkDir:     workDir2,
		AgentName:   "agent",
		TaskID:      "t2222222-0000-0000-0000-000000000000",
	})
	if err != nil {
		t.Fatalf("second CreateWorktree failed: %v", err)
	}

	if got := gitHead(t, result2.Path); got != sourceHead {
		t.Fatalf("second worktree HEAD = %s, want %s (remote default head after new commit)", got, sourceHead)
	}
}

func currentBranchName(t *testing.T, repoPath string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", repoPath, "symbolic-ref", "--short", "HEAD").Output()
	if err != nil {
		t.Fatalf("symbolic-ref --short HEAD in %s: %v", repoPath, err)
	}
	name := strings.TrimSpace(string(out))
	if name == "" {
		t.Fatalf("empty branch name in %s", repoPath)
	}
	return name
}

func TestEnsureRemoteTrackingLayoutMigratesLegacyCache(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()
	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	barePath := cache.Lookup("ws-1", sourceRepo)

	if err := setFetchRefspec(barePath, "+refs/heads/*:refs/heads/*"); err != nil {
		t.Fatalf("set legacy refspec: %v", err)
	}

	_ = exec.Command("git", "-C", barePath, "update-ref", "-d", "refs/remotes/origin/HEAD").Run()
	if err := exec.Command("sh", "-c", "rm -rf '"+filepath.Join(barePath, "refs", "remotes")+"'").Run(); err != nil {
		t.Fatalf("wipe refs/remotes: %v", err)
	}

	if cur, _ := readFetchRefspec(barePath); cur != "+refs/heads/*:refs/heads/*" {
		t.Fatalf("precondition failed: refspec is %q, want legacy mirror", cur)
	}

	if err := ensureRemoteTrackingLayout(barePath); err != nil {
		t.Fatalf("ensureRemoteTrackingLayout failed: %v", err)
	}

	cur, err := readFetchRefspec(barePath)
	if err != nil {
		t.Fatalf("read refspec after migration: %v", err)
	}
	if cur != modernFetchRefspec {
		t.Errorf("refspec = %q, want %q", cur, modernFetchRefspec)
	}

	ref := getRemoteDefaultBranch(barePath)
	if !strings.HasPrefix(ref, "refs/remotes/origin/") {
		t.Errorf("getRemoteDefaultBranch = %q, want refs/remotes/origin/*", ref)
	}
}

func TestCreateWorktreePathCollisionDoesNotLeakBranch(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()
	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	barePath := cache.Lookup("ws-1", sourceRepo)

	workDir := t.TempDir()
	dirName := repoNameFromURL(sourceRepo)
	worktreePath := filepath.Join(workDir, dirName)
	if err := os.MkdirAll(worktreePath, 0o755); err != nil {
		t.Fatalf("pre-create worktree path: %v", err)
	}
	if err := os.WriteFile(filepath.Join(worktreePath, "stray.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write stray file: %v", err)
	}

	_, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID: "ws-1",
		RepoURL:     sourceRepo,
		WorkDir:     workDir,
		AgentName:   "agent",
		TaskID:      "t1111111-0000-0000-0000-000000000000",
	})
	if err == nil {
		t.Fatal("expected CreateWorktree to fail when path exists as non-worktree")
	}

	out, runErr := exec.Command("git", "-C", barePath, "for-each-ref", "--format=%(refname)", "refs/heads/agent").Output()
	if runErr != nil {
		t.Fatalf("for-each-ref failed: %v", runErr)
	}
	if leaked := strings.TrimSpace(string(out)); leaked != "" {
		t.Errorf("branch leaked into bare repo after path-collision failure:\n%s", leaked)
	}
}

func TestGetRemoteDefaultBranchScansForCustomDefault(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()
	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	barePath := cache.Lookup("ws-1", sourceRepo)

	existing := getRemoteDefaultBranch(barePath)
	if existing == "" {
		t.Fatalf("precondition: cache should have a default branch right after sync")
	}
	commit := gitRefCommit(t, barePath, existing)

	runGitAuthored(t, barePath, "update-ref", "refs/remotes/origin/develop", commit)

	_ = exec.Command("git", "-C", barePath, "symbolic-ref", "-d", "refs/remotes/origin/HEAD").Run()
	_ = exec.Command("git", "-C", barePath, "update-ref", "-d", "refs/remotes/origin/main").Run()
	_ = exec.Command("git", "-C", barePath, "update-ref", "-d", "refs/remotes/origin/master").Run()

	got := getRemoteDefaultBranch(barePath)
	if got != "refs/remotes/origin/develop" {
		t.Fatalf("getRemoteDefaultBranch = %q, want refs/remotes/origin/develop", got)
	}
}

func TestGetRemoteDefaultBranchFallsBackToBareHead(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()
	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	barePath := cache.Lookup("ws-1", sourceRepo)

	if err := exec.Command("sh", "-c", "rm -rf '"+filepath.Join(barePath, "refs", "remotes")+"'").Run(); err != nil {
		t.Fatalf("wipe refs/remotes: %v", err)
	}

	if out, err := exec.Command("git", "-C", barePath, "for-each-ref", "refs/remotes/origin/").Output(); err == nil && strings.TrimSpace(string(out)) != "" {
		t.Fatalf("precondition failed: refs/remotes/origin/* should be empty, got %s", out)
	}

	got := getRemoteDefaultBranch(barePath)
	if !strings.HasPrefix(got, "refs/heads/") {
		t.Fatalf("getRemoteDefaultBranch = %q, want refs/heads/* fallback", got)
	}

	if err := exec.Command("git", "-C", barePath, "rev-parse", "--verify", got).Run(); err != nil {
		t.Fatalf("resolved ref %q does not exist: %v", got, err)
	}
}

func TestGitFetchRefreshesOriginHeadAfterDefaultChange(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	initialBranch := currentBranchName(t, sourceRepo)

	cacheRoot := t.TempDir()
	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	barePath := cache.Lookup("ws-1", sourceRepo)

	if got := getRemoteDefaultBranch(barePath); got != "refs/remotes/origin/"+initialBranch {
		t.Fatalf("precondition: getRemoteDefaultBranch = %q, want refs/remotes/origin/%s", got, initialBranch)
	}

	runGitAuthored(t, sourceRepo, "checkout", "-b", "new-default")
	addEmptyCommit(t, sourceRepo, "new-default commit")

	if err := gitFetch(barePath); err != nil {
		t.Fatalf("gitFetch failed: %v", err)
	}

	out, err := exec.Command("git", "-C", barePath, "symbolic-ref", "refs/remotes/origin/HEAD").Output()
	if err != nil {
		t.Fatalf("symbolic-ref origin/HEAD after fetch: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "refs/remotes/origin/new-default" {
		t.Fatalf("origin/HEAD after fetch = %q, want refs/remotes/origin/new-default", got)
	}

	if got := getRemoteDefaultBranch(barePath); got != "refs/remotes/origin/new-default" {
		t.Fatalf("getRemoteDefaultBranch after fetch = %q, want refs/remotes/origin/new-default", got)
	}
}

func TestGetRemoteDefaultBranchUsesBareHeadHintForCustomDefault(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()
	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	barePath := cache.Lookup("ws-1", sourceRepo)

	existing := getRemoteDefaultBranch(barePath)
	if existing == "" {
		t.Fatalf("precondition: cache should have a default branch right after sync")
	}
	commit := gitRefCommit(t, barePath, existing)

	runGitAuthored(t, barePath, "update-ref", "refs/heads/trunk", commit)
	runGitAuthored(t, barePath, "symbolic-ref", "HEAD", "refs/heads/trunk")

	runGitAuthored(t, barePath, "update-ref", "refs/remotes/origin/trunk", commit)
	runGitAuthored(t, barePath, "update-ref", "refs/remotes/origin/feature-alpha", commit)

	_ = exec.Command("git", "-C", barePath, "symbolic-ref", "-d", "refs/remotes/origin/HEAD").Run()
	_ = exec.Command("git", "-C", barePath, "update-ref", "-d", "refs/remotes/origin/main").Run()
	_ = exec.Command("git", "-C", barePath, "update-ref", "-d", "refs/remotes/origin/master").Run()

	got := getRemoteDefaultBranch(barePath)
	if got != "refs/remotes/origin/trunk" {
		t.Fatalf("getRemoteDefaultBranch = %q, want refs/remotes/origin/trunk (via bare-HEAD hint)", got)
	}
}

func TestCreateWorktreeInstallsCoAuthoredByHook(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()

	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	workDir := t.TempDir()
	result, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID:         "ws-1",
		RepoURL:             sourceRepo,
		WorkDir:             workDir,
		AgentName:           "Test Agent",
		TaskID:              "a1b2c3d4-0000-0000-0000-000000000000",
		CoAuthoredByEnabled: true,
	})
	if err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}

	if err := os.WriteFile(filepath.Join(result.Path, "test.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	runGitAuthored(t, result.Path, "add", ".")
	runGitAuthored(t, result.Path, "commit", "-m", "test commit")

	out, err := exec.Command("git", "-C", result.Path, "log", "-1", "--format=%B").Output()
	if err != nil {
		t.Fatalf("git log failed: %v", err)
	}
	commitMsg := string(out)
	expectedTrailer := "Co-authored-by: goosar-agent <github@goosar.ru>"
	if !strings.Contains(commitMsg, expectedTrailer) {
		t.Errorf("commit message missing Co-authored-by trailer.\ngot:\n%s", commitMsg)
	}
}

func TestCoAuthoredByHookIdempotent(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()

	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	workDir := t.TempDir()
	result, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID:         "ws-1",
		RepoURL:             sourceRepo,
		WorkDir:             workDir,
		AgentName:           "Test Agent",
		TaskID:              "b2c3d4e5-0000-0000-0000-000000000000",
		CoAuthoredByEnabled: true,
	})
	if err != nil {
		t.Fatalf("CreateWorktree failed: %v", err)
	}

	trailer := "Co-authored-by: goosar-agent <github@goosar.ru>"
	if err := os.WriteFile(filepath.Join(result.Path, "test.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	runGitAuthored(t, result.Path, "add", ".")
	runGitAuthored(t, result.Path, "commit", "-m", "test commit\n\n"+trailer)

	out, err := exec.Command("git", "-C", result.Path, "log", "-1", "--format=%B").Output()
	if err != nil {
		t.Fatalf("git log failed: %v", err)
	}
	commitMsg := string(out)

	count := strings.Count(commitMsg, trailer)
	if count != 1 {
		t.Errorf("expected exactly 1 Co-authored-by trailer, found %d.\ngot:\n%s", count, commitMsg)
	}
}

func TestCreateWorktreeRemovesCoAuthoredByHookWhenDisabled(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()

	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	workDir1 := t.TempDir()
	if _, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID:         "ws-1",
		RepoURL:             sourceRepo,
		WorkDir:             workDir1,
		AgentName:           "Test Agent",
		TaskID:              "11111111-0000-0000-0000-000000000000",
		CoAuthoredByEnabled: true,
	}); err != nil {
		t.Fatalf("CreateWorktree (enabled) failed: %v", err)
	}

	barePath := cache.Lookup("ws-1", sourceRepo)
	hookPath := filepath.Join(barePath, "hooks", "prepare-commit-msg")
	if _, err := os.Stat(hookPath); err != nil {
		t.Fatalf("precondition: expected hook to be installed at %s: %v", hookPath, err)
	}

	workDir2 := t.TempDir()
	result, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID:         "ws-1",
		RepoURL:             sourceRepo,
		WorkDir:             workDir2,
		AgentName:           "Test Agent",
		TaskID:              "22222222-0000-0000-0000-000000000000",
		CoAuthoredByEnabled: false,
	})
	if err != nil {
		t.Fatalf("CreateWorktree (disabled) failed: %v", err)
	}

	if _, err := os.Stat(hookPath); !os.IsNotExist(err) {
		t.Errorf("expected hook to be removed at %s, stat err=%v", hookPath, err)
	}

	if err := os.WriteFile(filepath.Join(result.Path, "test.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	runGitAuthored(t, result.Path, "add", ".")
	runGitAuthored(t, result.Path, "commit", "-m", "test commit")

	out, err := exec.Command("git", "-C", result.Path, "log", "-1", "--format=%B").Output()
	if err != nil {
		t.Fatalf("git log failed: %v", err)
	}
	commitMsg := string(out)
	if strings.Contains(commitMsg, "Co-authored-by: goosar-agent") {
		t.Errorf("commit unexpectedly carries the Co-authored-by trailer with setting disabled.\ngot:\n%s", commitMsg)
	}
}

func TestCreateWorktreeRemovesLegacyCoAuthoredByHook(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()

	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	const legacyHook = `#!/bin/sh
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

	barePath := cache.Lookup("ws-1", sourceRepo)
	hooksDir := filepath.Join(barePath, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatalf("create hooks dir: %v", err)
	}
	hookPath := filepath.Join(hooksDir, "prepare-commit-msg")
	if err := os.WriteFile(hookPath, []byte(legacyHook), 0o755); err != nil {
		t.Fatalf("seed legacy hook: %v", err)
	}

	workDir := t.TempDir()
	result, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID:         "ws-1",
		RepoURL:             sourceRepo,
		WorkDir:             workDir,
		AgentName:           "Test Agent",
		TaskID:              "44444444-0000-0000-0000-000000000000",
		CoAuthoredByEnabled: false,
	})
	if err != nil {
		t.Fatalf("CreateWorktree (disabled) failed: %v", err)
	}

	if _, err := os.Stat(hookPath); !os.IsNotExist(err) {
		t.Errorf("expected legacy hook to be removed at %s, stat err=%v", hookPath, err)
	}

	if err := os.WriteFile(filepath.Join(result.Path, "test.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	runGitAuthored(t, result.Path, "add", ".")
	runGitAuthored(t, result.Path, "commit", "-m", "test commit")

	out, err := exec.Command("git", "-C", result.Path, "log", "-1", "--format=%B").Output()
	if err != nil {
		t.Fatalf("git log failed: %v", err)
	}
	if commitMsg := string(out); strings.Contains(commitMsg, "Co-authored-by: goosar-agent") {
		t.Errorf("commit unexpectedly carries the Co-authored-by trailer after legacy hook removal.\ngot:\n%s", commitMsg)
	}
}

func TestRemoveCoAuthoredByHookPreservesUserHook(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()

	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	barePath := cache.Lookup("ws-1", sourceRepo)
	hooksDir := filepath.Join(barePath, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatalf("create hooks dir: %v", err)
	}
	hookPath := filepath.Join(hooksDir, "prepare-commit-msg")
	userHook := "#!/bin/sh\n# user hook, not Goosar\nexit 0\n"
	if err := os.WriteFile(hookPath, []byte(userHook), 0o755); err != nil {
		t.Fatalf("seed user hook: %v", err)
	}

	workDir := t.TempDir()
	if _, err := cache.CreateWorktree(WorktreeParams{
		WorkspaceID:         "ws-1",
		RepoURL:             sourceRepo,
		WorkDir:             workDir,
		AgentName:           "Test Agent",
		TaskID:              "33333333-0000-0000-0000-000000000000",
		CoAuthoredByEnabled: false,
	}); err != nil {
		t.Fatalf("CreateWorktree (disabled) failed: %v", err)
	}

	got, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("user hook unexpectedly removed: %v", err)
	}
	if string(got) != userHook {
		t.Errorf("user hook contents changed.\nwant:\n%s\ngot:\n%s", userHook, string(got))
	}
}

func TestGetRemoteDefaultBranchAmbiguousOriginReturnsEmpty(t *testing.T) {
	t.Parallel()
	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()
	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-1", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	barePath := cache.Lookup("ws-1", sourceRepo)

	existing := getRemoteDefaultBranch(barePath)
	if existing == "" {
		t.Fatalf("precondition: cache should have a default branch right after sync")
	}
	commit := gitRefCommit(t, barePath, existing)

	runGitAuthored(t, barePath, "update-ref", "refs/remotes/origin/feature-a", commit)
	runGitAuthored(t, barePath, "update-ref", "refs/remotes/origin/feature-b", commit)

	_ = exec.Command("git", "-C", barePath, "symbolic-ref", "-d", "refs/remotes/origin/HEAD").Run()
	_ = exec.Command("git", "-C", barePath, "update-ref", "-d", "refs/remotes/origin/main").Run()
	_ = exec.Command("git", "-C", barePath, "update-ref", "-d", "refs/remotes/origin/master").Run()
	if bareRef := bareHeadBranch(barePath); bareRef != "" {
		sameName := strings.TrimPrefix(bareRef, "refs/heads/")
		_ = exec.Command("git", "-C", barePath, "update-ref", "-d", "refs/remotes/origin/"+sameName).Run()
	}

	got := getRemoteDefaultBranch(barePath)
	if got != "" {
		t.Fatalf("getRemoteDefaultBranch = %q, want \"\" (ambiguous origin/* must not guess)", got)
	}
}

func TestNewGitCommandUsesStableWorkingDirectory(t *testing.T) {
	cmd := newGitCommand(context.Background(), "--version")
	if cmd.Dir == "" {
		t.Fatal("newGitCommand Dir is empty; Git would inherit the daemon working directory")
	}
	if !filepath.IsAbs(cmd.Dir) {
		t.Fatalf("newGitCommand Dir = %q, want an absolute path", cmd.Dir)
	}
	if info, err := os.Stat(cmd.Dir); err != nil {
		t.Fatalf("newGitCommand Dir = %q is unavailable: %v", cmd.Dir, err)
	} else if !info.IsDir() {
		t.Fatalf("newGitCommand Dir = %q is not a directory", cmd.Dir)
	}
}

func TestSyncSurvivesDeletedProcessWorkingDirectory(t *testing.T) {
	sourceRepo := createTestRepo(t)
	cacheRoot := t.TempDir()
	originalCWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("get original working directory: %v", err)
	}
	deletedRoot := t.TempDir()
	deletedCWD := filepath.Join(deletedRoot, "worktree")
	if err := os.Mkdir(deletedCWD, 0o755); err != nil {
		t.Fatalf("create disposable working directory: %v", err)
	}
	if err := os.Chdir(deletedCWD); err != nil {
		t.Fatalf("chdir to disposable working directory: %v", err)
	}
	defer func() {
		if err := os.Chdir(originalCWD); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	}()
	if err := os.RemoveAll(deletedRoot); err != nil {
		t.Skipf("platform does not allow removing the process working directory: %v", err)
	}

	cache := New(cacheRoot, testLogger())
	if err := cache.Sync("ws-deleted-cwd", []RepoInfo{{URL: sourceRepo}}); err != nil {
		t.Fatalf("Sync with deleted process working directory failed: %v", err)
	}
	if cachedPath := cache.Lookup("ws-deleted-cwd", sourceRepo); !isBareRepo(cachedPath) {
		t.Fatalf("expected synced bare repo, got %q", cachedPath)
	}
}
