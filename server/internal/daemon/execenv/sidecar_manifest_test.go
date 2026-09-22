package execenv

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func walkRelative(t *testing.T, root string) []string {
	t.Helper()
	var entries []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if rel == "." {
			entries = append(entries, ".")
			return nil
		}
		if d.IsDir() {
			entries = append(entries, rel+string(os.PathSeparator))
			return nil
		}
		entries = append(entries, rel)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	sort.Strings(entries)
	return entries
}

type workdirSnapshot struct {
	entries []string
	files   map[string]string
}

func snapshot(t *testing.T, root string) workdirSnapshot {
	t.Helper()
	snap := workdirSnapshot{files: map[string]string{}}
	snap.entries = walkRelative(t, root)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		snap.files[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatalf("walk-for-content %s: %v", root, err)
	}
	return snap
}

func assertSnapshotEqual(t *testing.T, label string, want, got workdirSnapshot) {
	t.Helper()
	if !reflect.DeepEqual(want.entries, got.entries) {
		t.Errorf("[%s] directory listing differs\n want: %v\n  got: %v", label, want.entries, got.entries)
	}
	if !reflect.DeepEqual(want.files, got.files) {

		for k, wv := range want.files {
			gv, ok := got.files[k]
			if !ok {
				t.Errorf("[%s] missing file %s after round-trip", label, k)
				continue
			}
			if wv != gv {
				t.Errorf("[%s] file %s differs\n want: %q\n  got: %q", label, k, wv, gv)
			}
		}
		for k := range got.files {
			if _, ok := want.files[k]; !ok {
				t.Errorf("[%s] orphan file %s after round-trip", label, k)
			}
		}
	}
}

func runPrepareLikeCycle(t *testing.T, workDir, envRoot, provider string, ctx TaskContextForEnv) {
	t.Helper()
	manifest := &sidecarManifest{}
	if err := writeContextFiles(workDir, provider, ctx, manifest); err != nil {
		t.Fatalf("writeContextFiles(%s): %v", provider, err)
	}
	if err := writeSidecarManifest(envRoot, manifest); err != nil {
		t.Fatalf("writeSidecarManifest(%s): %v", provider, err)
	}
	if _, err := InjectRuntimeConfig(workDir, provider, ctx); err != nil {
		t.Fatalf("InjectRuntimeConfig(%s): %v", provider, err)
	}

	if err := CleanupRuntimeConfig(workDir, provider); err != nil {
		t.Fatalf("CleanupRuntimeConfig(%s): %v", provider, err)
	}
	if err := CleanupSidecars(envRoot); err != nil {
		t.Fatalf("CleanupSidecars(%s): %v", provider, err)
	}
}

var allFileBasedProviders = []string{
	"runtime-c",
	"runtime-d",
	"runtime-e",
	"runtime-f",
	"runtime-m",
	"runtime-n",
	"runtime-j",
	"runtime-o",
	"runtime-g",
	"runtime-k",
	"runtime-l",
	"runtime-a",
	"runtime-q",
}

func TestPrepareThenCleanupSidecarsRoundTripEmptyWorkdir(t *testing.T) {
	t.Parallel()
	for _, provider := range allFileBasedProviders {
		provider := provider
		t.Run(provider, func(t *testing.T) {
			t.Parallel()
			workDir := t.TempDir()
			envRoot := t.TempDir()
			before := snapshot(t, workDir)

			ctx := TaskContextForEnv{
				IssueID: "11111111-2222-3333-4444-555555555555",
				AgentSkills: []SkillContextForEnv{
					{
						Name:        "Issue Review",
						Description: "Review GH issues",
						Content:     "Steps to review",
						Files: []SkillFileContextForEnv{
							{Path: "templates/checklist.md", Content: "- [ ] check"},
						},
					},
					{
						Name:    "PR Review",
						Content: "Review PR diffs",
					},
				},
				ProjectID:    "proj-1",
				ProjectTitle: "Demo",
			}

			runPrepareLikeCycle(t, workDir, envRoot, provider, ctx)

			after := snapshot(t, workDir)
			assertSnapshotEqual(t, provider, before, after)
		})
	}
}

func TestPrepareThenCleanupSidecarsPreservesUserSkillSibling(t *testing.T) {
	t.Parallel()

	cases := []struct {
		provider      string
		userSkillRel  string
		userSkillFile string
	}{
		{"runtime-c", filepath.Join(".claude", "skills", "my-own"), "SKILL.md"},
		{"runtime-d", filepath.Join(".codebuddy", "skills", "my-own"), "SKILL.md"},
		{"runtime-f", filepath.Join(".github", "skills", "my-own"), "SKILL.md"},
		{"runtime-m", filepath.Join(".opencode", "skills", "my-own"), "SKILL.md"},
		{"runtime-n", filepath.Join("skills", "my-own"), "SKILL.md"},
		{"runtime-o", filepath.Join(".pi", "skills", "my-own"), "SKILL.md"},
		{"runtime-g", filepath.Join(".cursor", "skills", "my-own"), "SKILL.md"},
		{"runtime-k", filepath.Join(".kimi", "skills", "my-own"), "SKILL.md"},
		{"runtime-l", filepath.Join(".kiro", "skills", "my-own"), "SKILL.md"},
		{"runtime-a", filepath.Join(".agents", "skills", "my-own"), "SKILL.md"},
		{"runtime-q", filepath.Join(".qwen", "skills", "my-own"), "SKILL.md"},
		{"runtime-j", filepath.Join(".agent_context", "skills", "my-own"), "SKILL.md"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.provider, func(t *testing.T) {
			t.Parallel()
			workDir := t.TempDir()
			envRoot := t.TempDir()

			userDir := filepath.Join(workDir, tc.userSkillRel)
			if err := os.MkdirAll(userDir, 0o755); err != nil {
				t.Fatalf("seed user skill dir: %v", err)
			}
			userBody := "---\nname: my-own\n---\n\nUser-authored.\n"
			if err := os.WriteFile(filepath.Join(userDir, tc.userSkillFile), []byte(userBody), 0o644); err != nil {
				t.Fatalf("seed user skill file: %v", err)
			}

			before := snapshot(t, workDir)

			ctx := TaskContextForEnv{
				IssueID: "11111111-2222-3333-4444-555555555555",
				AgentSkills: []SkillContextForEnv{
					{Name: "Issue Review", Content: "ours"},
				},
			}
			runPrepareLikeCycle(t, workDir, envRoot, tc.provider, ctx)

			after := snapshot(t, workDir)
			assertSnapshotEqual(t, tc.provider, before, after)

			got, err := os.ReadFile(filepath.Join(userDir, tc.userSkillFile))
			if err != nil {
				t.Fatalf("user skill went missing after round-trip: %v", err)
			}
			if string(got) != userBody {
				t.Errorf("user skill content changed\n want: %q\n  got: %q", userBody, string(got))
			}
		})
	}
}

func TestPrepareThenCleanupSidecarsPreservesUnrelatedUserFiles(t *testing.T) {
	t.Parallel()
	cases := []struct {
		provider string
		userFile string
	}{
		{"runtime-c", filepath.Join(".claude", "settings.json")},
		{"runtime-d", filepath.Join(".codebuddy", "settings.json")},
		{"runtime-f", filepath.Join(".github", "CODEOWNERS")},
		{"runtime-m", filepath.Join(".opencode", "config.json")},
		{"runtime-o", filepath.Join(".pi", "config.toml")},
		{"runtime-g", filepath.Join(".cursor", "settings.json")},
		{"runtime-k", filepath.Join(".kimi", "config.json")},
		{"runtime-l", filepath.Join(".kiro", "config.json")},
		{"runtime-a", filepath.Join(".agents", "config.json")},
		{"runtime-q", filepath.Join(".qwen", "settings.json")},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.provider, func(t *testing.T) {
			t.Parallel()
			workDir := t.TempDir()
			envRoot := t.TempDir()

			userPath := filepath.Join(workDir, tc.userFile)
			if err := os.MkdirAll(filepath.Dir(userPath), 0o755); err != nil {
				t.Fatalf("seed user dir: %v", err)
			}
			userBody := "user content " + tc.provider
			if err := os.WriteFile(userPath, []byte(userBody), 0o644); err != nil {
				t.Fatalf("seed user file: %v", err)
			}

			before := snapshot(t, workDir)

			ctx := TaskContextForEnv{
				IssueID: "11111111-2222-3333-4444-555555555555",
				AgentSkills: []SkillContextForEnv{
					{Name: "Issue Review", Content: "ours"},
				},
			}
			runPrepareLikeCycle(t, workDir, envRoot, tc.provider, ctx)

			after := snapshot(t, workDir)
			assertSnapshotEqual(t, tc.provider, before, after)
		})
	}
}

func TestPrepareThenCleanupSidecarsRepeatedCycles(t *testing.T) {
	t.Parallel()
	for _, provider := range allFileBasedProviders {
		provider := provider
		t.Run(provider, func(t *testing.T) {
			t.Parallel()
			workDir := t.TempDir()
			envRoot := t.TempDir()
			before := snapshot(t, workDir)

			ctx := TaskContextForEnv{
				IssueID: "11111111-2222-3333-4444-555555555555",
				AgentSkills: []SkillContextForEnv{
					{Name: "Issue Review", Content: "ours"},
				},
			}
			for i := 0; i < 3; i++ {
				runPrepareLikeCycle(t, workDir, envRoot, provider, ctx)
				after := snapshot(t, workDir)
				assertSnapshotEqual(t, provider, before, after)
			}
		})
	}
}

func TestPrepareThenCleanupSidecarsWithProjectResources(t *testing.T) {
	t.Parallel()
	for _, provider := range allFileBasedProviders {
		provider := provider
		t.Run(provider, func(t *testing.T) {
			t.Parallel()
			workDir := t.TempDir()
			envRoot := t.TempDir()
			before := snapshot(t, workDir)

			ctx := TaskContextForEnv{
				IssueID:      "11111111-2222-3333-4444-555555555555",
				ProjectID:    "proj-1",
				ProjectTitle: "Demo project",
				ProjectResources: []ProjectResourceForEnv{
					{
						ID:           "res-1",
						ResourceType: "github_repo",
						ResourceRef:  []byte(`{"url":"https://github.com/example/repo"}`),
					},
				},
			}
			runPrepareLikeCycle(t, workDir, envRoot, provider, ctx)

			after := snapshot(t, workDir)
			assertSnapshotEqual(t, provider, before, after)
		})
	}
}

func TestCleanupSidecarsNoOpWhenManifestMissing(t *testing.T) {
	t.Parallel()
	envRoot := t.TempDir()
	if err := CleanupSidecars(envRoot); err != nil {
		t.Errorf("CleanupSidecars on empty envRoot returned error: %v", err)
	}
	if err := CleanupSidecars(""); err != nil {
		t.Errorf("CleanupSidecars with empty envRoot returned error: %v", err)
	}
}

func TestCleanupSidecarsLeavesUserContentInTrackedDirIntact(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()
	envRoot := t.TempDir()

	managedDir := filepath.Join(workDir, ".goosar")
	managedProject := filepath.Join(managedDir, "project")
	managedFile := filepath.Join(managedProject, "resources.json")
	if err := os.MkdirAll(managedProject, 0o755); err != nil {
		t.Fatalf("seed dirs: %v", err)
	}
	if err := os.WriteFile(managedFile, []byte("{}"), 0o644); err != nil {
		t.Fatalf("seed managed file: %v", err)
	}
	userFile := filepath.Join(managedDir, "user-notes.txt")
	if err := os.WriteFile(userFile, []byte("hello"), 0o644); err != nil {
		t.Fatalf("seed user file: %v", err)
	}

	manifest := &sidecarManifest{
		Files: []string{managedFile},
		Dirs:  []string{managedDir, managedProject},
	}
	if err := writeSidecarManifest(envRoot, manifest); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	if err := CleanupSidecars(envRoot); err != nil {
		t.Errorf("CleanupSidecars: %v", err)
	}

	if _, err := os.Stat(managedFile); !os.IsNotExist(err) {
		t.Errorf("managed file %s should be gone, stat err=%v", managedFile, err)
	}
	if _, err := os.Stat(managedProject); !os.IsNotExist(err) {
		t.Errorf("inner managed dir %s should be empty and removed, stat err=%v", managedProject, err)
	}

	got, err := os.ReadFile(userFile)
	if err != nil {
		t.Fatalf("user file went missing: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("user file content changed: %q", string(got))
	}
}

func TestCleanupSidecarsDoesNotRemovePreExistingDirs(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()
	envRoot := t.TempDir()

	userDir := filepath.Join(workDir, ".claude")
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatalf("seed user dir: %v", err)
	}

	manifest := &sidecarManifest{}
	target := filepath.Join(userDir, "skills", "ours")
	if err := recordMkdirAll(target, 0o755, manifest); err != nil {
		t.Fatalf("recordMkdirAll: %v", err)
	}
	for _, d := range manifest.Dirs {
		if d == userDir {
			t.Fatalf("manifest must not record pre-existing user dir %s\nfull dirs: %v", userDir, manifest.Dirs)
		}
	}

	if err := writeSidecarManifest(envRoot, manifest); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := CleanupSidecars(envRoot); err != nil {
		t.Fatalf("CleanupSidecars: %v", err)
	}
	if _, err := os.Stat(userDir); err != nil {
		t.Errorf("pre-existing user dir %s removed by cleanup: %v", userDir, err)
	}
}

func TestRecordWriteFileRefusesToOverwritePreExistingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "user.md")
	if err := os.WriteFile(target, []byte("user bytes"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	m := &sidecarManifest{}
	err := recordWriteFile(target, []byte("ours"), 0o644, m)
	if !errors.Is(err, errPathPreExists) {
		t.Fatalf("recordWriteFile must return errPathPreExists for a pre-existing target, got: %v", err)
	}
	for _, f := range m.Files {
		if f == target {
			t.Errorf("manifest must not record pre-existing user file %s", target)
		}
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != "user bytes" {
		t.Errorf("user bytes must survive refused write\n want: %q\n  got: %q", "user bytes", string(got))
	}
}

func TestRecordWriteFileRefusesToOverwriteSymlinkOrDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	symlinkPath := filepath.Join(dir, "symlink.md")
	if err := os.Symlink(filepath.Join(dir, "does-not-exist"), symlinkPath); err != nil {
		t.Skipf("symlink not supported on this platform: %v", err)
	}
	if err := recordWriteFile(symlinkPath, []byte("ours"), 0o644, &sidecarManifest{}); !errors.Is(err, errPathPreExists) {
		t.Errorf("recordWriteFile on dangling symlink should refuse, got: %v", err)
	}

	dirPath := filepath.Join(dir, "subdir")
	if err := os.Mkdir(dirPath, 0o755); err != nil {
		t.Fatalf("seed dir: %v", err)
	}
	if err := recordWriteFile(dirPath, []byte("ours"), 0o644, &sidecarManifest{}); !errors.Is(err, errPathPreExists) {
		t.Errorf("recordWriteFile on pre-existing directory should refuse, got: %v", err)
	}
}

func TestSidecarManifestRoundTripJSON(t *testing.T) {
	t.Parallel()
	envRoot := t.TempDir()
	original := &sidecarManifest{
		Files: []string{"/x/.agent_context/issue_context.md"},
		Dirs:  []string{"/x/.agent_context"},
	}
	if err := writeSidecarManifest(envRoot, original); err != nil {
		t.Fatalf("write: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(envRoot, sidecarManifestFile))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for _, want := range []string{"files", "dirs", "issue_context.md", ".agent_context"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("manifest JSON missing %q\n got: %s", want, string(raw))
		}
	}
}

var sameSlugSkillProviderCases = []struct {
	provider string
	skillDir string
}{
	{"runtime-c", filepath.Join(".claude", "skills", "issue-review")},
	{"runtime-d", filepath.Join(".codebuddy", "skills", "issue-review")},
	{"runtime-f", filepath.Join(".github", "skills", "issue-review")},
	{"runtime-m", filepath.Join(".opencode", "skills", "issue-review")},
	{"runtime-n", filepath.Join("skills", "issue-review")},
	{"runtime-o", filepath.Join(".pi", "skills", "issue-review")},
	{"runtime-g", filepath.Join(".cursor", "skills", "issue-review")},
	{"runtime-k", filepath.Join(".kimi", "skills", "issue-review")},
	{"runtime-l", filepath.Join(".kiro", "skills", "issue-review")},
	{"runtime-a", filepath.Join(".agents", "skills", "issue-review")},
	{"runtime-q", filepath.Join(".qwen", "skills", "issue-review")},
	{"runtime-j", filepath.Join(".agent_context", "skills", "issue-review")},
}

func TestPrepareThenCleanupSidecarsSameSlugCollisionPerProvider(t *testing.T) {
	t.Parallel()
	for _, tc := range sameSlugSkillProviderCases {
		tc := tc
		t.Run(tc.provider, func(t *testing.T) {
			t.Parallel()
			workDir := t.TempDir()
			envRoot := t.TempDir()

			userSkillDir := filepath.Join(workDir, tc.skillDir)
			if err := os.MkdirAll(userSkillDir, 0o755); err != nil {
				t.Fatalf("seed user skill dir: %v", err)
			}
			userBody := "---\nname: issue-review\ndescription: user-authored\n---\n\nUser owns this slug.\n"
			userSkillFile := filepath.Join(userSkillDir, "SKILL.md")
			if err := os.WriteFile(userSkillFile, []byte(userBody), 0o644); err != nil {
				t.Fatalf("seed user SKILL.md: %v", err)
			}

			userExtra := filepath.Join(userSkillDir, "notes.md")
			if err := os.WriteFile(userExtra, []byte("private notes"), 0o644); err != nil {
				t.Fatalf("seed user extra file: %v", err)
			}

			before := snapshot(t, workDir)

			ctx := TaskContextForEnv{
				IssueID: "11111111-2222-3333-4444-555555555555",
				AgentSkills: []SkillContextForEnv{
					{
						Name:        "Issue Review",
						Description: "Goosar's version",
						Content:     "---\nname: issue-review\n---\n\nGoosar skill content.\n",
						Files: []SkillFileContextForEnv{
							{Path: "templates/checklist.md", Content: "- [ ] check"},
						},
					},
				},
			}
			runPrepareLikeCycle(t, workDir, envRoot, tc.provider, ctx)

			after := snapshot(t, workDir)
			assertSnapshotEqual(t, tc.provider, before, after)

			gotBody, err := os.ReadFile(userSkillFile)
			if err != nil {
				t.Fatalf("user SKILL.md went missing: %v", err)
			}
			if string(gotBody) != userBody {
				t.Errorf("user SKILL.md mutated\n want: %q\n  got: %q", userBody, string(gotBody))
			}
			gotExtra, err := os.ReadFile(userExtra)
			if err != nil {
				t.Fatalf("user extra file went missing: %v", err)
			}
			if string(gotExtra) != "private notes" {
				t.Errorf("user extra file mutated\n want: %q\n  got: %q", "private notes", string(gotExtra))
			}
		})
	}
}

func TestPrepareThenCleanupSidecarsIssueContextCollisionPerProvider(t *testing.T) {
	t.Parallel()
	for _, provider := range allFileBasedProviders {
		provider := provider
		t.Run(provider, func(t *testing.T) {
			t.Parallel()
			workDir := t.TempDir()
			envRoot := t.TempDir()

			if err := os.MkdirAll(filepath.Join(workDir, ".agent_context"), 0o755); err != nil {
				t.Fatalf("seed dir: %v", err)
			}
			userBody := "# user-authored issue_context.md\n\nDo not touch.\n"
			userPath := filepath.Join(workDir, ".agent_context", "issue_context.md")
			if err := os.WriteFile(userPath, []byte(userBody), 0o644); err != nil {
				t.Fatalf("seed user file: %v", err)
			}

			before := snapshot(t, workDir)

			ctx := TaskContextForEnv{
				IssueID: "11111111-2222-3333-4444-555555555555",
			}
			runPrepareLikeCycle(t, workDir, envRoot, provider, ctx)

			after := snapshot(t, workDir)
			assertSnapshotEqual(t, provider, before, after)

			got, err := os.ReadFile(userPath)
			if err != nil {
				t.Fatalf("user issue_context.md went missing: %v", err)
			}
			if string(got) != userBody {
				t.Errorf("user issue_context.md mutated\n want: %q\n  got: %q", userBody, string(got))
			}
		})
	}
}

func TestPrepareThenCleanupSidecarsProjectResourcesCollisionPerProvider(t *testing.T) {
	t.Parallel()
	for _, provider := range allFileBasedProviders {
		provider := provider
		t.Run(provider, func(t *testing.T) {
			t.Parallel()
			workDir := t.TempDir()
			envRoot := t.TempDir()

			if err := os.MkdirAll(filepath.Join(workDir, ".goosar", "project"), 0o755); err != nil {
				t.Fatalf("seed dir: %v", err)
			}
			userBody := `{"user":"owns this file"}`
			userPath := filepath.Join(workDir, ".goosar", "project", "resources.json")
			if err := os.WriteFile(userPath, []byte(userBody), 0o644); err != nil {
				t.Fatalf("seed user file: %v", err)
			}

			before := snapshot(t, workDir)

			ctx := TaskContextForEnv{
				IssueID:      "11111111-2222-3333-4444-555555555555",
				ProjectID:    "proj-1",
				ProjectTitle: "Demo",
				ProjectResources: []ProjectResourceForEnv{
					{
						ID:           "res-1",
						ResourceType: "github_repo",
						ResourceRef:  []byte(`{"url":"https://github.com/example/repo"}`),
					},
				},
			}
			runPrepareLikeCycle(t, workDir, envRoot, provider, ctx)

			after := snapshot(t, workDir)
			assertSnapshotEqual(t, provider, before, after)

			got, err := os.ReadFile(userPath)
			if err != nil {
				t.Fatalf("user resources.json went missing: %v", err)
			}
			if string(got) != userBody {
				t.Errorf("user resources.json mutated\n want: %q\n  got: %q", userBody, string(got))
			}
		})
	}
}

func TestAllocateCollisionFreeSkillDir(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()

	slug, dir, err := allocateCollisionFreeSkillDir(parent, "issue-review")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if slug != "issue-review" {
		t.Errorf("first allocation should use base slug; got %q", slug)
	}
	if dir != filepath.Join(parent, "issue-review") {
		t.Errorf("first allocation path = %q, want under parent", dir)
	}

	if err := os.MkdirAll(filepath.Join(parent, "issue-review"), 0o755); err != nil {
		t.Fatalf("seed user dir: %v", err)
	}
	slug, dir, err = allocateCollisionFreeSkillDir(parent, "issue-review")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if slug != "issue-review-goosar" {
		t.Errorf("second allocation should bump to `-goosar`; got %q", slug)
	}
	if dir != filepath.Join(parent, "issue-review-goosar") {
		t.Errorf("second allocation path = %q, want under parent", dir)
	}

	if err := os.MkdirAll(filepath.Join(parent, "issue-review-goosar"), 0o755); err != nil {
		t.Fatalf("seed bumped dir: %v", err)
	}
	slug, dir, err = allocateCollisionFreeSkillDir(parent, "issue-review")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if slug != "issue-review-goosar-2" {
		t.Errorf("third allocation should be `-goosar-2`; got %q", slug)
	}
	if dir != filepath.Join(parent, "issue-review-goosar-2") {
		t.Errorf("third allocation path = %q, want under parent", dir)
	}
}

func TestPrepareThenCleanupSidecarsMultiSkillCollisionFreeAllocation(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()
	envRoot := t.TempDir()

	userDir := filepath.Join(workDir, ".claude", "skills", "issue-review")
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	userBody := "user-authored skill\n"
	userFile := filepath.Join(userDir, "SKILL.md")
	if err := os.WriteFile(userFile, []byte(userBody), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	manifest := &sidecarManifest{}
	if err := writeContextFiles(workDir, "runtime-c", TaskContextForEnv{
		IssueID: "11111111-2222-3333-4444-555555555555",
		AgentSkills: []SkillContextForEnv{
			{Name: "Issue Review", Content: "Goosar's version\n"},
		},
	}, manifest); err != nil {
		t.Fatalf("writeContextFiles: %v", err)
	}

	goosarDir := filepath.Join(workDir, ".claude", "skills", "issue-review-goosar")
	if _, err := os.Stat(filepath.Join(goosarDir, "SKILL.md")); err != nil {
		t.Errorf("Goosar sibling skill should exist at %s: %v", goosarDir, err)
	}
	got, err := os.ReadFile(userFile)
	if err != nil {
		t.Fatalf("user SKILL.md went missing during inject: %v", err)
	}
	if string(got) != userBody {
		t.Errorf("user SKILL.md mutated during inject\n want: %q\n  got: %q", userBody, string(got))
	}

	if err := writeSidecarManifest(envRoot, manifest); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := CleanupSidecars(envRoot); err != nil {
		t.Fatalf("CleanupSidecars: %v", err)
	}
	if _, err := os.Stat(goosarDir); !os.IsNotExist(err) {
		t.Errorf("Goosar sibling should be removed by Cleanup; stat err=%v", err)
	}
	got, err = os.ReadFile(userFile)
	if err != nil {
		t.Fatalf("user SKILL.md went missing after cleanup: %v", err)
	}
	if string(got) != userBody {
		t.Errorf("user SKILL.md mutated after cleanup\n want: %q\n  got: %q", userBody, string(got))
	}
}

func TestCleanupSidecarsSwallowsMissingAndNonEmptyDirs(t *testing.T) {
	t.Parallel()

	envRoot1 := t.TempDir()
	missing := filepath.Join(t.TempDir(), "never-existed")
	if err := writeSidecarManifest(envRoot1, &sidecarManifest{Dirs: []string{missing}}); err != nil {
		t.Fatalf("write missing-dir manifest: %v", err)
	}
	if err := CleanupSidecars(envRoot1); err != nil {
		t.Errorf("CleanupSidecars(missing dir) should swallow ENOENT silently, got: %v", err)
	}

	envRoot2 := t.TempDir()
	workDir2 := t.TempDir()
	recordedDir := filepath.Join(workDir2, "recorded")
	if err := os.MkdirAll(recordedDir, 0o755); err != nil {
		t.Fatalf("seed recorded dir: %v", err)
	}
	userFile := filepath.Join(recordedDir, "user.txt")
	if err := os.WriteFile(userFile, []byte("user content"), 0o644); err != nil {
		t.Fatalf("seed user file: %v", err)
	}
	if err := writeSidecarManifest(envRoot2, &sidecarManifest{Dirs: []string{recordedDir}}); err != nil {
		t.Fatalf("write non-empty-dir manifest: %v", err)
	}
	if err := CleanupSidecars(envRoot2); err != nil {
		t.Errorf("CleanupSidecars(non-empty dir) should swallow ENOTEMPTY silently, got: %v", err)
	}
	got, err := os.ReadFile(userFile)
	if err != nil {
		t.Fatalf("user content went missing: %v", err)
	}
	if string(got) != "user content" {
		t.Errorf("user content mutated: %q", string(got))
	}
}

func TestCleanupSidecarsSurfacesEACCESOnEmptyRecordedDir(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("chmod is bypassed for uid 0; cannot synthesize EACCES on rmdir")
	}

	workDir := t.TempDir()
	envRoot := t.TempDir()

	parent := filepath.Join(workDir, "parent")
	recorded := filepath.Join(parent, "empty-dir")
	if err := os.MkdirAll(recorded, 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := writeSidecarManifest(envRoot, &sidecarManifest{Dirs: []string{recorded}}); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	if err := os.Chmod(parent, 0o555); err != nil {
		t.Fatalf("chmod parent: %v", err)
	}

	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })

	err := CleanupSidecars(envRoot)
	if err == nil {
		t.Fatal("CleanupSidecars should surface the EACCES rmdir error, got nil")
	}
	if !strings.Contains(err.Error(), "empty-dir") {
		t.Errorf("expected surfaced error to reference recorded path, got: %v", err)
	}
	if !strings.Contains(err.Error(), "rmdir") {
		t.Errorf("expected surfaced error to come from rmdir branch, got: %v", err)
	}
}

func TestCleanupSidecarsSurfacesEACCESWhenReadDirFailsToo(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("chmod is bypassed for uid 0; cannot synthesize EACCES on rmdir + readdir")
	}

	workDir := t.TempDir()
	envRoot := t.TempDir()

	parent := filepath.Join(workDir, "parent")
	recorded := filepath.Join(parent, "locked-dir")
	if err := os.MkdirAll(recorded, 0o755); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := writeSidecarManifest(envRoot, &sidecarManifest{Dirs: []string{recorded}}); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	if err := os.Chmod(recorded, 0o000); err != nil {
		t.Fatalf("chmod recorded: %v", err)
	}
	if err := os.Chmod(parent, 0o555); err != nil {
		_ = os.Chmod(recorded, 0o755)
		t.Fatalf("chmod parent: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(parent, 0o755)
		_ = os.Chmod(recorded, 0o755)
	})

	err := CleanupSidecars(envRoot)
	if err == nil {
		t.Fatal("CleanupSidecars should surface the rmdir error even when ReadDir also fails, got nil")
	}
	if !strings.Contains(err.Error(), "locked-dir") {
		t.Errorf("expected surfaced error to reference recorded path, got: %v", err)
	}
	if !strings.Contains(err.Error(), "rmdir") {
		t.Errorf("expected surfaced error to be the ORIGINAL rmdir error, not the ReadDir failure, got: %v", err)
	}
}

func TestDirHasEntries(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	empty := filepath.Join(root, "empty")
	if err := os.Mkdir(empty, 0o755); err != nil {
		t.Fatalf("seed empty: %v", err)
	}
	if has, ok := dirHasEntries(empty); !ok || has {
		t.Errorf("dirHasEntries(empty dir) = (%v, %v), want (false, true)", has, ok)
	}

	full := filepath.Join(root, "full")
	if err := os.Mkdir(full, 0o755); err != nil {
		t.Fatalf("seed full: %v", err)
	}
	if err := os.WriteFile(filepath.Join(full, "file"), []byte(""), 0o644); err != nil {
		t.Fatalf("seed full content: %v", err)
	}
	if has, ok := dirHasEntries(full); !ok || !has {
		t.Errorf("dirHasEntries(non-empty dir) = (%v, %v), want (true, true)", has, ok)
	}

	missing := filepath.Join(root, "missing")
	if has, ok := dirHasEntries(missing); !ok || has {
		t.Errorf("dirHasEntries(missing dir) = (%v, %v), want (false, true) — ENOENT collapses to empty so the rmdir-race resolves cleanly", has, ok)
	}

	regular := filepath.Join(root, "regular.txt")
	if err := os.WriteFile(regular, []byte("not a dir"), 0o644); err != nil {
		t.Fatalf("seed regular: %v", err)
	}
	if has, ok := dirHasEntries(regular); ok || has {
		t.Errorf("dirHasEntries(regular file) = (%v, %v), want (false, false) — ENOTDIR must NOT be laundered as ENOTEMPTY", has, ok)
	}
}
