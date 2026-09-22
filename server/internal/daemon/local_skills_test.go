package daemon

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeTestLocalSkill(t *testing.T, root, rel string, files map[string]string) string {
	t.Helper()

	skillDir := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	for path, content := range files {
		fullPath := filepath.Join(skillDir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("mkdir parents for %s: %v", path, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	return skillDir
}

func writeTestClaudePlugin(t *testing.T, home, id, name string, enabled bool) string {
	t.Helper()
	installPath := filepath.Join(home, ".claude", "plugins", "cache", name, "1.0.0")
	if err := os.MkdirAll(filepath.Join(installPath, ".claude-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name":"` + name + `","skills":"./skills","mcpServers":"./mcp.json"}`
	if err := os.WriteFile(filepath.Join(installPath, ".claude-plugin", "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude", "plugins"), 0o755); err != nil {
		t.Fatal(err)
	}
	installed := `{"version":2,"plugins":{"` + id + `":[{"scope":"user","installPath":"` + installPath + `"}]}}`
	if err := os.WriteFile(filepath.Join(home, ".claude", "plugins", "installed_plugins.json"), []byte(installed), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	settings := `{"enabledPlugins":{"` + id + `":` + map[bool]string{true: "true", false: "false"}[enabled] + `}}`
	if err := os.WriteFile(filepath.Join(home, ".claude", "settings.json"), []byte(settings), 0o644); err != nil {
		t.Fatal(err)
	}
	return installPath
}

func TestListRuntimeLocalSkills_Claude(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeTestLocalSkill(t, filepath.Join(home, ".claude", "skills"), "review-helper", map[string]string{
		"SKILL.md":           "---\nname: Review Helper\ndescription: Review pull requests\n---\n# Review Helper\n",
		"templates/check.md": "checklist",
		"LICENSE":            "ignored",
		".secret":            "ignored",
	})

	skills, supported, err := listRuntimeLocalSkills("runtime-c")
	if err != nil {
		t.Fatalf("listRuntimeLocalSkills: %v", err)
	}
	if !supported {
		t.Fatal("claude should be supported")
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}

	skill := skills[0]
	if skill.Key != "review-helper" {
		t.Fatalf("key = %q, want review-helper", skill.Key)
	}
	if skill.Name != "Review Helper" {
		t.Fatalf("name = %q, want Review Helper", skill.Name)
	}
	if skill.Description != "Review pull requests" {
		t.Fatalf("description = %q", skill.Description)
	}

	if skill.FileCount != 2 {
		t.Fatalf("file_count = %d, want 2", skill.FileCount)
	}
	if skill.SourcePath != "~/.claude/skills/review-helper" {
		t.Fatalf("source_path = %q", skill.SourcePath)
	}
	if !skill.CanDisable {
		t.Fatal("claude runtime skill should be controllable")
	}
}

func TestListRuntimeLocalSkills_Codebuddy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeTestLocalSkill(t, filepath.Join(home, ".codebuddy", "skills"), "review-helper", map[string]string{
		"SKILL.md": "---\nname: CodeBuddy Review\ndescription: Review code with CodeBuddy\n---\n# CodeBuddy Review\n",
	})

	writeTestLocalSkill(t, filepath.Join(home, ".claude", "skills"), "review-helper", map[string]string{
		"SKILL.md": "---\nname: Claude Review\ndescription: Should not appear for codebuddy\n---\n# Claude Review\n",
	})

	skills, supported, err := listRuntimeLocalSkills("runtime-d")
	if err != nil {
		t.Fatalf("listRuntimeLocalSkills: %v", err)
	}
	if !supported {
		t.Fatal("codebuddy should be supported")
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d: %+v", len(skills), skills)
	}

	skill := skills[0]
	if skill.Key != "review-helper" {
		t.Fatalf("key = %q, want review-helper", skill.Key)
	}
	if skill.Name != "CodeBuddy Review" {
		t.Fatalf("name = %q, want CodeBuddy Review", skill.Name)
	}
	if skill.SourcePath != "~/.codebuddy/skills/review-helper" {
		t.Fatalf("source_path = %q, want ~/.codebuddy/skills/review-helper", skill.SourcePath)
	}
	if skill.CanDisable {
		t.Fatal("codebuddy runtime skill controls are not supported yet")
	}
}

func TestRuntimeLocalSkills_CodebuddyExcludesClaudePluginSkills(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeTestLocalSkill(t, filepath.Join(home, ".codebuddy", "skills"), "codebuddy-only", map[string]string{
		"SKILL.md": "---\nname: CodeBuddy Only\n---\n",
	})
	installPath := writeTestClaudePlugin(t, home, "paper-desktop@paper", "paper-desktop", true)
	writeTestLocalSkill(t, filepath.Join(installPath, "skills"), "design-to-code", map[string]string{
		"SKILL.md": "---\nname: Claude Plugin Skill\n---\n",
	})

	skills, supported, err := listRuntimeLocalSkills("runtime-d")
	if err != nil {
		t.Fatalf("listRuntimeLocalSkills: %v", err)
	}
	if !supported {
		t.Fatal("codebuddy should be supported")
	}
	if len(skills) != 1 || skills[0].Key != "codebuddy-only" {
		t.Fatalf("CodeBuddy must not list Claude plugin skills, got %#v", skills)
	}

	пакет, supported, err := loadRuntimeLocalSkillBundle("runtime-d", "paper-desktop:design-to-code")
	if !supported {
		t.Fatal("codebuddy should be supported for import")
	}
	if err == nil || err.Error() != "local skill not found" {
		t.Fatalf("load Claude plugin skill for CodeBuddy err = %v, want local skill not found", err)
	}
	if пакет != nil {
		t.Fatalf("load Claude plugin skill for CodeBuddy bundle = %#v, want nil", пакет)
	}
}

func TestListRuntimeLocalSkills_ClaudeEnabledPlugin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	installPath := writeTestClaudePlugin(t, home, "paper-desktop@paper", "paper-desktop", true)
	writeTestLocalSkill(t, filepath.Join(installPath, "skills"), "design-to-code", map[string]string{
		"SKILL.md": "---\nname: Design to code\ndescription: Turn a design into code\n---\n# Design\n",
	})

	skills, supported, err := listRuntimeLocalSkills("runtime-c")
	if err != nil {
		t.Fatal(err)
	}
	if !supported || len(skills) != 1 {
		t.Fatalf("supported=%v skills=%#v", supported, skills)
	}
	got := skills[0]
	if got.Key != "paper-desktop:design-to-code" || got.Name != "paper-desktop:design-to-code" {
		t.Fatalf("plugin skill identity = %#v", got)
	}
	if got.Root != localSkillRootPlugin || got.Plugin != "paper-desktop@paper" {
		t.Fatalf("plugin skill origin = %#v", got)
	}

	пакет, supported, err := loadRuntimeLocalSkillBundle("runtime-c", got.Key)
	if err != nil || !supported {
		t.Fatalf("load plugin bundle: supported=%v err=%v", supported, err)
	}
	if пакет.Name != got.Key || пакет.Description != "Turn a design into code" {
		t.Fatalf("plugin bundle = %#v", пакет)
	}
}

func TestListRuntimeLocalSkills_ClaudeDisabledPluginIsHidden(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	installPath := writeTestClaudePlugin(t, home, "paper-desktop@paper", "paper-desktop", false)
	writeTestLocalSkill(t, filepath.Join(installPath, "skills"), "design-to-code", map[string]string{
		"SKILL.md": "---\nname: Design to code\n---\n",
	})

	skills, supported, err := listRuntimeLocalSkills("runtime-c")
	if err != nil || !supported {
		t.Fatalf("supported=%v err=%v", supported, err)
	}
	if len(skills) != 0 {
		t.Fatalf("disabled plugin skills should be hidden, got %#v", skills)
	}
}

func TestListRuntimeLocalSkills_Kiro(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeTestLocalSkill(t, filepath.Join(home, ".kiro", "skills"), "review-helper", map[string]string{
		"SKILL.md": "---\nname: Kiro Review\ndescription: Review code with Kiro\n---\n# Kiro Review\n",
	})

	skills, supported, err := listRuntimeLocalSkills("runtime-l")
	if err != nil {
		t.Fatalf("listRuntimeLocalSkills: %v", err)
	}
	if !supported {
		t.Fatal("kiro should be supported")
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Key != "review-helper" {
		t.Fatalf("key = %q, want review-helper", skills[0].Key)
	}
	if skills[0].Name != "Kiro Review" {
		t.Fatalf("name = %q, want Kiro Review", skills[0].Name)
	}
	if skills[0].SourcePath != "~/.kiro/skills/review-helper" {
		t.Fatalf("source_path = %q", skills[0].SourcePath)
	}
}

func TestLocalSkills_DiscoversACPProviderRoots(t *testing.T) {
	tests := []struct {
		provider string
		root     string
		wantPath string
		wantName string
	}{
		{
			provider: "runtime-j",
			root:     filepath.Join(".hermes", "skills"),
			wantPath: "~/.hermes/skills/review-helper",
			wantName: "Hermes Review",
		},
		{
			provider: "runtime-k",
			root:     filepath.Join(".kimi", "skills"),
			wantPath: "~/.kimi/skills/review-helper",
			wantName: "Kimi Review",
		},
		{
			provider: "runtime-p",
			root:     filepath.Join(".qoder", "skills"),
			wantPath: "~/.qoder/skills/review-helper",
			wantName: "Qoder Review",
		},
		{
			provider: "runtime-q",
			root:     filepath.Join(".qwen", "skills"),
			wantPath: "~/.qwen/skills/review-helper",
			wantName: "Qwen Review",
		},
		{
			provider: "runtime-i",
			root:     filepath.Join(".grok", "skills"),
			wantPath: "~/.grok/skills/review-helper",
			wantName: "Grok Review",
		},
	}

	for _, tc := range tests {
		t.Run(tc.provider, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			if tc.provider == "runtime-i" {
				t.Setenv("GROK_HOME", "")
			}
			if tc.provider == "runtime-q" {
				t.Setenv("QWEN_HOME", "")
			}

			writeTestLocalSkill(t, filepath.Join(home, tc.root), "review-helper", map[string]string{
				"SKILL.md": "---\nname: " + tc.wantName + "\ndescription: Review code\n---\n# Review\n",
				"notes.md": "notes",
			})

			skills, supported, err := listRuntimeLocalSkills(tc.provider)
			if err != nil {
				t.Fatalf("listRuntimeLocalSkills: %v", err)
			}
			if !supported {
				t.Fatalf("%s should be supported", tc.provider)
			}
			if len(skills) != 1 {
				t.Fatalf("expected 1 skill, got %d (%v)", len(skills), skills)
			}
			if skills[0].Key != "review-helper" {
				t.Fatalf("key = %q, want review-helper", skills[0].Key)
			}
			if skills[0].Name != tc.wantName {
				t.Fatalf("name = %q, want %q", skills[0].Name, tc.wantName)
			}
			if skills[0].Root != localSkillRootProvider {
				t.Fatalf("root = %q, want %q", skills[0].Root, localSkillRootProvider)
			}
			if skills[0].SourcePath != tc.wantPath {
				t.Fatalf("source_path = %q, want %q", skills[0].SourcePath, tc.wantPath)
			}

			пакет, supported, err := loadRuntimeLocalSkillBundle(tc.provider, "review-helper")
			if err != nil {
				t.Fatalf("loadRuntimeLocalSkillBundle: %v", err)
			}
			if !supported {
				t.Fatalf("%s should be supported for import", tc.provider)
			}
			if пакет.Name != tc.wantName {
				t.Fatalf("bundle name = %q, want %q", пакет.Name, tc.wantName)
			}
			if пакет.SourcePath != tc.wantPath {
				t.Fatalf("bundle source_path = %q, want %q", пакет.SourcePath, tc.wantPath)
			}
			if len(пакет.Files) != 1 {
				t.Fatalf("expected 1 supporting file, got %d", len(пакет.Files))
			}
		})
	}
}

func TestListRuntimeLocalSkills_GrokUsesGROKHOME(t *testing.T) {
	home := t.TempDir()
	grokHome := filepath.Join(t.TempDir(), "custom-grok-home")
	t.Setenv("HOME", home)
	t.Setenv("GROK_HOME", grokHome)
	writeTestLocalSkill(t, filepath.Join(grokHome, "skills"), "review-helper", map[string]string{
		"SKILL.md": "---\nname: Grok Home Review\ndescription: Review code\n---\n# Review\n",
	})
	writeTestLocalSkill(t, filepath.Join(home, ".grok", "skills"), "wrong-home", map[string]string{
		"SKILL.md": "---\nname: Wrong Home\n---\n# Wrong\n",
	})

	skills, supported, err := listRuntimeLocalSkills("runtime-i")
	if err != nil {
		t.Fatalf("listRuntimeLocalSkills: %v", err)
	}
	if !supported {
		t.Fatal("grok should be supported")
	}
	if len(skills) != 1 || skills[0].Key != "review-helper" {
		t.Fatalf("expected only GROK_HOME skill, got %+v", skills)
	}
	if skills[0].SourcePath != filepath.ToSlash(filepath.Join(grokHome, "skills", "review-helper")) {
		t.Fatalf("source_path = %q, want custom GROK_HOME path", skills[0].SourcePath)
	}

	пакет, supported, err := loadRuntimeLocalSkillBundle("runtime-i", "review-helper")
	if err != nil {
		t.Fatalf("loadRuntimeLocalSkillBundle: %v", err)
	}
	if !supported || пакет == nil || пакет.Name != "Grok Home Review" {
		t.Fatalf("unexpected bundle: supported=%v bundle=%+v", supported, пакет)
	}
}

func TestListRuntimeLocalSkills_QwenUsesQWENHOME(t *testing.T) {
	home := t.TempDir()
	qwenHome := filepath.Join(t.TempDir(), "custom-qwen-home")
	t.Setenv("HOME", home)
	t.Setenv("QWEN_HOME", qwenHome)
	writeTestLocalSkill(t, filepath.Join(qwenHome, "skills"), "review-helper", map[string]string{
		"SKILL.md": "---\nname: Qwen Home Review\ndescription: Review code\n---\n# Review\n",
	})
	writeTestLocalSkill(t, filepath.Join(home, ".qwen", "skills"), "wrong-home", map[string]string{
		"SKILL.md": "---\nname: Wrong Home\n---\n# Wrong\n",
	})

	skills, supported, err := listRuntimeLocalSkills("runtime-q")
	if err != nil {
		t.Fatalf("listRuntimeLocalSkills: %v", err)
	}
	if !supported {
		t.Fatal("qwen should be supported")
	}
	if len(skills) != 1 || skills[0].Key != "review-helper" {
		t.Fatalf("expected only QWEN_HOME skill, got %+v", skills)
	}
	if skills[0].SourcePath != filepath.ToSlash(filepath.Join(qwenHome, "skills", "review-helper")) {
		t.Fatalf("source_path = %q, want custom QWEN_HOME path", skills[0].SourcePath)
	}

	пакет, supported, err := loadRuntimeLocalSkillBundle("runtime-q", "review-helper")
	if err != nil {
		t.Fatalf("loadRuntimeLocalSkillBundle: %v", err)
	}
	if !supported || пакет == nil || пакет.Name != "Qwen Home Review" {
		t.Fatalf("unexpected bundle: supported=%v bundle=%+v", supported, пакет)
	}
}

func TestListRuntimeLocalSkills_FollowsSymlinkedSkillDirs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	target := writeTestLocalSkill(t, filepath.Join(home, ".agents", "skills"), "lark-doc", map[string]string{
		"SKILL.md":  "---\nname: Lark Doc\ndescription: Drive lark docs\n---\n# Lark Doc\n",
		"helper.md": "stub",
	})

	skillsRoot := filepath.Join(home, ".claude", "skills")
	if err := os.MkdirAll(skillsRoot, 0o755); err != nil {
		t.Fatalf("mkdir skills root: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(skillsRoot, "lark-doc")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	writeTestLocalSkill(t, skillsRoot, "review-helper", map[string]string{
		"SKILL.md": "---\nname: Review Helper\n---\n",
	})

	skills, supported, err := listRuntimeLocalSkills("runtime-c")
	if err != nil {
		t.Fatalf("listRuntimeLocalSkills: %v", err)
	}
	if !supported {
		t.Fatal("claude should be supported")
	}
	if len(skills) != 2 {
		t.Fatalf("expected 2 skills, got %d (%v)", len(skills), skills)
	}

	bySymlinkKey := skills[0]
	if bySymlinkKey.Key != "lark-doc" {
		bySymlinkKey = skills[1]
	}
	if bySymlinkKey.Key != "lark-doc" {
		t.Fatalf("symlinked skill missing from result: %v", skills)
	}
	if bySymlinkKey.Name != "Lark Doc" {
		t.Fatalf("symlinked skill name = %q, want Lark Doc", bySymlinkKey.Name)
	}

	if bySymlinkKey.SourcePath != "~/.claude/skills/lark-doc" {
		t.Fatalf("symlinked skill source_path = %q", bySymlinkKey.SourcePath)
	}
}

func TestListRuntimeLocalSkills_CodexUsesSharedCODEXHOME(t *testing.T) {
	home := t.TempDir()
	codexHome := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", codexHome)

	writeTestLocalSkill(t, filepath.Join(codexHome, "skills"), "debugger", map[string]string{
		"SKILL.md": "# Debugger\n",
	})
	writeTestLocalSkill(t, filepath.Join(home, ".codex", "skills"), "wrong-home", map[string]string{
		"SKILL.md": "# Wrong Home\n",
	})

	skills, supported, err := listRuntimeLocalSkills("runtime-e")
	if err != nil {
		t.Fatalf("listRuntimeLocalSkills: %v", err)
	}
	if !supported {
		t.Fatal("codex should be supported")
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Key != "debugger" {
		t.Fatalf("key = %q, want debugger", skills[0].Key)
	}
	if skills[0].SourcePath != filepath.Join(codexHome, "skills", "debugger") {
		t.Fatalf("source_path = %q", skills[0].SourcePath)
	}
}

func TestListRuntimeLocalSkills_DescendsIntoNestedSkillDirs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	root := filepath.Join(home, ".config", "opencode", "skills")

	writeTestLocalSkill(t, root, "top", map[string]string{
		"SKILL.md":           "---\nname: Top\n---\n",
		"templates/SKILL.md": "not a real skill — sub-template that happens to share the filename",
	})

	writeTestLocalSkill(t, root, "release/reporter", map[string]string{
		"SKILL.md": "---\nname: Release Reporter\n---\n",
	})

	skills, supported, err := listRuntimeLocalSkills("runtime-m")
	if err != nil {
		t.Fatalf("listRuntimeLocalSkills: %v", err)
	}
	if !supported {
		t.Fatal("opencode should be supported")
	}

	keys := make([]string, 0, len(skills))
	for _, s := range skills {
		keys = append(keys, s.Key)
	}

	wantKeys := []string{"release/reporter", "top"}
	if !reflect.DeepEqual(keys, wantKeys) {
		t.Fatalf("keys = %v, want %v", keys, wantKeys)
	}
}

func TestLoadRuntimeLocalSkillBundle_OpenCode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeTestLocalSkill(t, filepath.Join(home, ".config", "opencode", "skills"), "release/reporter", map[string]string{
		"SKILL.md":           "---\nname: Release Reporter\ndescription: Summarize release notes\n---\n# Release Reporter\n",
		"docs/template.md":   "template body",
		"examples/sample.md": "sample body",
	})

	пакет, supported, err := loadRuntimeLocalSkillBundle("runtime-m", "release/reporter")
	if err != nil {
		t.Fatalf("loadRuntimeLocalSkillBundle: %v", err)
	}
	if !supported {
		t.Fatal("opencode should be supported")
	}
	if пакет.Name != "Release Reporter" {
		t.Fatalf("name = %q", пакет.Name)
	}
	if пакет.Description != "Summarize release notes" {
		t.Fatalf("description = %q", пакет.Description)
	}
	if len(пакет.Files) != 2 {
		t.Fatalf("expected 2 supporting files, got %d", len(пакет.Files))
	}
	if пакет.Files[0].Path != "docs/template.md" || пакет.Files[0].Content != "template body" {
		t.Fatalf("unexpected first file: %+v", пакет.Files[0])
	}
	if пакет.Files[1].Path != "examples/sample.md" || пакет.Files[1].Content != "sample body" {
		t.Fatalf("unexpected second file: %+v", пакет.Files[1])
	}
	if пакет.SourcePath != "~/.config/opencode/skills/release/reporter" {
		t.Fatalf("source_path = %q", пакет.SourcePath)
	}
}

func TestListRuntimeLocalSkills_OpenClaw(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeTestLocalSkill(t, filepath.Join(home, ".openclaw", "skills"), "planner", map[string]string{
		"SKILL.md": "# Planner\n",
	})

	skills, supported, err := listRuntimeLocalSkills("runtime-n")
	if err != nil {
		t.Fatalf("listRuntimeLocalSkills: %v", err)
	}
	if !supported {
		t.Fatal("openclaw should be supported")
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].SourcePath != "~/.openclaw/skills/planner" {
		t.Fatalf("source_path = %q", skills[0].SourcePath)
	}
}

func TestLoadRuntimeLocalSkillBundle_Cursor(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeTestLocalSkill(t, filepath.Join(home, ".cursor", "skills"), "docs-helper", map[string]string{
		"SKILL.md":         "---\nname: Docs Helper\n---\n# Docs Helper\n",
		"notes/tips.md":    "tips",
		"examples/a.txt":   "example",
		".hidden/skip.txt": "ignore",
	})

	пакет, supported, err := loadRuntimeLocalSkillBundle("runtime-g", "docs-helper")
	if err != nil {
		t.Fatalf("loadRuntimeLocalSkillBundle: %v", err)
	}
	if !supported {
		t.Fatal("cursor should be supported")
	}
	if пакет.Name != "Docs Helper" {
		t.Fatalf("name = %q", пакет.Name)
	}
	if len(пакет.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(пакет.Files))
	}
	if пакет.SourcePath != "~/.cursor/skills/docs-helper" {
		t.Fatalf("source_path = %q", пакет.SourcePath)
	}
}

func TestListRuntimeLocalSkills_DiscoversUniversalAgentsRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeTestLocalSkill(t, filepath.Join(home, ".agents", "skills"), "universal-helper", map[string]string{
		"SKILL.md":     "---\nname: Universal Helper\ndescription: Cross-tool skill\n---\n# Universal Helper\n",
		"docs/info.md": "info",
	})

	skills, supported, err := listRuntimeLocalSkills("runtime-c")
	if err != nil {
		t.Fatalf("listRuntimeLocalSkills: %v", err)
	}
	if !supported {
		t.Fatal("claude should be supported")
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d (%v)", len(skills), skills)
	}
	if skills[0].Key != "universal-helper" {
		t.Fatalf("key = %q, want universal-helper", skills[0].Key)
	}
	if skills[0].Name != "Universal Helper" {
		t.Fatalf("name = %q, want Universal Helper", skills[0].Name)
	}
	if skills[0].Root != localSkillRootUniversal {
		t.Fatalf("root = %q, want %q", skills[0].Root, localSkillRootUniversal)
	}
	if skills[0].SourcePath != "~/.agents/skills/universal-helper" {
		t.Fatalf("source_path = %q", skills[0].SourcePath)
	}

	if skills[0].FileCount != 2 {
		t.Fatalf("file_count = %d, want 2", skills[0].FileCount)
	}
}

func TestLoadRuntimeLocalSkillBundle_ImportsFromUniversalRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeTestLocalSkill(t, filepath.Join(home, ".agents", "skills"), "shared-skill", map[string]string{
		"SKILL.md":        "---\nname: Shared Skill\ndescription: Imported from agents root\n---\n# Shared Skill\n",
		"examples/use.md": "usage",
		"scripts/run.sh":  "echo hi",
	})

	пакет, supported, err := loadRuntimeLocalSkillBundle("runtime-c", "shared-skill")
	if err != nil {
		t.Fatalf("loadRuntimeLocalSkillBundle: %v", err)
	}
	if !supported {
		t.Fatal("claude should be supported")
	}
	if пакет.Name != "Shared Skill" {
		t.Fatalf("name = %q, want Shared Skill", пакет.Name)
	}
	if len(пакет.Files) != 2 {
		t.Fatalf("expected 2 supporting files, got %d", len(пакет.Files))
	}
	if пакет.SourcePath != "~/.agents/skills/shared-skill" {
		t.Fatalf("source_path = %q", пакет.SourcePath)
	}
}

func TestLocalSkills_ProviderRootWinsOnKeyConflict(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeTestLocalSkill(t, filepath.Join(home, ".claude", "skills"), "dup", map[string]string{
		"SKILL.md": "---\nname: Provider Copy\n---\n# provider\n",
	})
	writeTestLocalSkill(t, filepath.Join(home, ".agents", "skills"), "dup", map[string]string{
		"SKILL.md": "---\nname: Universal Copy\n---\n# universal\n",
	})

	skills, _, err := listRuntimeLocalSkills("runtime-c")
	if err != nil {
		t.Fatalf("listRuntimeLocalSkills: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 deduped skill, got %d (%v)", len(skills), skills)
	}
	if skills[0].Name != "Provider Copy" {
		t.Fatalf("name = %q, want Provider Copy (provider root must win)", skills[0].Name)
	}
	if skills[0].Root != localSkillRootProvider {
		t.Fatalf("root = %q, want %q", skills[0].Root, localSkillRootProvider)
	}
	if skills[0].SourcePath != "~/.claude/skills/dup" {
		t.Fatalf("source_path = %q, want provider path", skills[0].SourcePath)
	}

	пакет, _, err := loadRuntimeLocalSkillBundle("runtime-c", "dup")
	if err != nil {
		t.Fatalf("loadRuntimeLocalSkillBundle: %v", err)
	}
	if пакет.Name != "Provider Copy" {
		t.Fatalf("bundle name = %q, want Provider Copy", пакет.Name)
	}
	if пакет.SourcePath != "~/.claude/skills/dup" {
		t.Fatalf("bundle source_path = %q, want provider path", пакет.SourcePath)
	}
}

func TestListRuntimeLocalSkills_MergesBothRoots(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeTestLocalSkill(t, filepath.Join(home, ".claude", "skills"), "provider-only", map[string]string{
		"SKILL.md": "---\nname: Provider Only\n---\n",
	})
	writeTestLocalSkill(t, filepath.Join(home, ".agents", "skills"), "universal-only", map[string]string{
		"SKILL.md": "---\nname: Universal Only\n---\n",
	})

	skills, _, err := listRuntimeLocalSkills("runtime-c")
	if err != nil {
		t.Fatalf("listRuntimeLocalSkills: %v", err)
	}
	keys := make([]string, 0, len(skills))
	roots := make(map[string]string)
	for _, s := range skills {
		keys = append(keys, s.Key)
		roots[s.Key] = s.Root
	}

	wantKeys := []string{"provider-only", "universal-only"}
	if !reflect.DeepEqual(keys, wantKeys) {
		t.Fatalf("keys = %v, want %v", keys, wantKeys)
	}
	if roots["provider-only"] != localSkillRootProvider {
		t.Fatalf("provider-only root = %q", roots["provider-only"])
	}
	if roots["universal-only"] != localSkillRootUniversal {
		t.Fatalf("universal-only root = %q", roots["universal-only"])
	}
}

func TestListRuntimeLocalSkills_MissingUniversalRootIsNotAnError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeTestLocalSkill(t, filepath.Join(home, ".claude", "skills"), "only-provider", map[string]string{
		"SKILL.md": "---\nname: Only Provider\n---\n",
	})

	skills, supported, err := listRuntimeLocalSkills("runtime-c")
	if err != nil {
		t.Fatalf("listRuntimeLocalSkills: %v", err)
	}
	if !supported {
		t.Fatal("claude should be supported")
	}
	if len(skills) != 1 || skills[0].Key != "only-provider" {
		t.Fatalf("expected only-provider, got %v", skills)
	}
}

func TestListRuntimeLocalSkills_BothRootsMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	skills, supported, err := listRuntimeLocalSkills("runtime-c")
	if err != nil {
		t.Fatalf("listRuntimeLocalSkills: %v", err)
	}
	if !supported {
		t.Fatal("claude should be supported")
	}
	if len(skills) != 0 {
		t.Fatalf("expected empty, got %v", skills)
	}
}

func TestListRuntimeLocalSkills_NestedSkillInUniversalRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeTestLocalSkill(t, filepath.Join(home, ".agents", "skills"), "release/reporter", map[string]string{
		"SKILL.md": "---\nname: Release Reporter\n---\n",
	})

	skills, _, err := listRuntimeLocalSkills("runtime-m")
	if err != nil {
		t.Fatalf("listRuntimeLocalSkills: %v", err)
	}
	if len(skills) != 1 || skills[0].Key != "release/reporter" {
		t.Fatalf("expected release/reporter, got %v", skills)
	}
	if skills[0].Root != localSkillRootUniversal {
		t.Fatalf("root = %q, want %q", skills[0].Root, localSkillRootUniversal)
	}

	пакет, _, err := loadRuntimeLocalSkillBundle("runtime-m", "release/reporter")
	if err != nil {
		t.Fatalf("loadRuntimeLocalSkillBundle: %v", err)
	}
	if пакет.Name != "Release Reporter" {
		t.Fatalf("bundle name = %q", пакет.Name)
	}
}

func TestLoadRuntimeLocalSkillBundle_FallsThroughToUniversalOnNotExist(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeTestLocalSkill(t, filepath.Join(home, ".claude", "skills"), "something-else", map[string]string{
		"SKILL.md": "---\nname: Something Else\n---\n",
	})
	writeTestLocalSkill(t, filepath.Join(home, ".agents", "skills"), "only-universal", map[string]string{
		"SKILL.md": "---\nname: Only Universal\n---\n# only universal\n",
	})

	пакет, _, err := loadRuntimeLocalSkillBundle("runtime-c", "only-universal")
	if err != nil {
		t.Fatalf("loadRuntimeLocalSkillBundle: %v", err)
	}
	if пакет.Name != "Only Universal" {
		t.Fatalf("name = %q, want Only Universal", пакет.Name)
	}
	if пакет.SourcePath != "~/.agents/skills/only-universal" {
		t.Fatalf("source_path = %q", пакет.SourcePath)
	}
}

func TestLoadRuntimeLocalSkillBundle_DoesNotMaskReadErrorWithUniversalFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	clashDir := filepath.Join(home, ".claude", "skills", "clash")
	if err := os.MkdirAll(filepath.Join(clashDir, "SKILL.md"), 0o755); err != nil {
		t.Fatalf("mkdir SKILL.md-as-dir: %v", err)
	}

	writeTestLocalSkill(t, filepath.Join(home, ".agents", "skills"), "clash", map[string]string{
		"SKILL.md": "---\nname: Universal Clash\n---\n",
	})

	пакет, _, err := loadRuntimeLocalSkillBundle("runtime-c", "clash")
	if err == nil {
		t.Fatalf("expected an error, got bundle %+v", пакет)
	}
	if пакет != nil {
		t.Fatalf("expected nil bundle on error, got %+v", пакет)
	}
}

func TestListRuntimeLocalSkills_PerRootVisitedAllowsCrossRootSymlinkAlias(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	target := writeTestLocalSkill(t, filepath.Join(home, ".agents", "skills"), "foo", map[string]string{
		"SKILL.md": "---\nname: Foo\n---\n",
	})

	claudeRoot := filepath.Join(home, ".claude", "skills")
	if err := os.MkdirAll(claudeRoot, 0o755); err != nil {
		t.Fatalf("mkdir claude root: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(claudeRoot, "bar")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	skills, _, err := listRuntimeLocalSkills("runtime-c")
	if err != nil {
		t.Fatalf("listRuntimeLocalSkills: %v", err)
	}
	keys := make([]string, 0, len(skills))
	roots := make(map[string]string)
	for _, s := range skills {
		keys = append(keys, s.Key)
		roots[s.Key] = s.Root
	}
	wantKeys := []string{"bar", "foo"}
	if !reflect.DeepEqual(keys, wantKeys) {
		t.Fatalf("keys = %v, want %v (per-root visited must not collapse the alias)", keys, wantKeys)
	}
	if roots["bar"] != localSkillRootProvider {
		t.Fatalf("bar root = %q, want provider", roots["bar"])
	}
	if roots["foo"] != localSkillRootUniversal {
		t.Fatalf("foo root = %q, want universal", roots["foo"])
	}
}

func TestLoadRuntimeLocalSkillBundle_ProviderDirWithoutSkillMdFallsThrough(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeTestLocalSkill(t, filepath.Join(home, ".claude", "skills"), "shadowed", map[string]string{
		"notes.md": "not a skill — no SKILL.md here",
	})

	writeTestLocalSkill(t, filepath.Join(home, ".agents", "skills"), "shadowed", map[string]string{
		"SKILL.md":     "---\nname: Real Shadowed\ndescription: The valid one\n---\n# Real Shadowed\n",
		"docs/info.md": "info",
	})

	skills, _, err := listRuntimeLocalSkills("runtime-c")
	if err != nil {
		t.Fatalf("listRuntimeLocalSkills: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d (%v)", len(skills), skills)
	}
	if skills[0].Key != "shadowed" || skills[0].Root != localSkillRootUniversal {
		t.Fatalf("list surfaced %+v, want key=shadowed root=universal", skills[0])
	}
	if skills[0].SourcePath != "~/.agents/skills/shadowed" {
		t.Fatalf("list source_path = %q, want ~/.agents/skills/shadowed", skills[0].SourcePath)
	}

	пакет, _, err := loadRuntimeLocalSkillBundle("runtime-c", "shadowed")
	if err != nil {
		t.Fatalf("loadRuntimeLocalSkillBundle: %v (load disagreed with list)", err)
	}
	if пакет.Name != "Real Shadowed" {
		t.Fatalf("bundle name = %q, want Real Shadowed", пакет.Name)
	}
	if пакет.SourcePath != "~/.agents/skills/shadowed" {
		t.Fatalf("bundle source_path = %q, want ~/.agents/skills/shadowed", пакет.SourcePath)
	}
}

func TestLoadRuntimeLocalSkillBundle_ProviderNonDirFallsThrough(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	claudeRoot := filepath.Join(home, ".claude", "skills")
	if err := os.MkdirAll(claudeRoot, 0o755); err != nil {
		t.Fatalf("mkdir claude root: %v", err)
	}

	if err := os.WriteFile(filepath.Join(claudeRoot, "filish"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	writeTestLocalSkill(t, filepath.Join(home, ".agents", "skills"), "filish", map[string]string{
		"SKILL.md": "---\nname: Filish\n---\n# Filish\n",
	})

	пакет, _, err := loadRuntimeLocalSkillBundle("runtime-c", "filish")
	if err != nil {
		t.Fatalf("loadRuntimeLocalSkillBundle: %v", err)
	}
	if пакет.Name != "Filish" || пакет.SourcePath != "~/.agents/skills/filish" {
		t.Fatalf("bundle = %+v, want universal Filish", пакет)
	}
}
