package agenttmpl

import (
	"io/fs"
	"path"
	"strings"
	"testing"
	"testing/fstest"
)

func TestVendoredCoversEveryTemplateSkill(t *testing.T) {
	templates, err := Load()
	if err != nil {
		t.Fatalf("load templates: %v", err)
	}
	vendored, err := LoadVendored()
	if err != nil {
		t.Fatalf("load vendored skills: %v", err)
	}

	for _, tmpl := range templates.List() {
		for _, ref := range tmpl.Skills {
			if _, ok := vendored.ForURL(ref.SourceURL); !ok {
				t.Errorf("template %q: skill %s has no vendored copy — run scripts/sync-vendored-template-skills.sh", tmpl.Slug, ref.SourceURL)
			}
		}
	}
}

func TestLoadVendoredBundlesAreComplete(t *testing.T) {
	vendored, err := LoadVendored()
	if err != nil {
		t.Fatalf("load vendored skills: %v", err)
	}
	if len(vendored.byURL) == 0 {
		t.Fatal("vendored registry is empty")
	}
	for url, skill := range vendored.byURL {
		if strings.TrimSpace(skill.Content) == "" {
			t.Errorf("%s: empty SKILL.md content", url)
		}
		if skill.Name == "" {
			t.Errorf("%s: empty resolved name", url)
		}
		if skill.Owner == "" || skill.Repo == "" || skill.Commit == "" {
			t.Errorf("%s: incomplete provenance (owner=%q repo=%q commit=%q)", url, skill.Owner, skill.Repo, skill.Commit)
		}
		for _, f := range skill.Files {
			if f.Path == "" || strings.HasPrefix(f.Path, "/") || strings.Contains(f.Path, "..") {
				t.Errorf("%s: unsafe vendored file path %q", url, f.Path)
			}
			if strings.EqualFold(f.Path, "SKILL.md") {
				t.Errorf("%s: SKILL.md must be the primary content, not a supporting file", url)
			}
		}
	}
}

func TestVendoredForURLNormalizesTrivialVariants(t *testing.T) {
	vendored, err := LoadVendored()
	if err != nil {
		t.Fatalf("load vendored skills: %v", err)
	}
	canonical := "https://github.com/anthropics/skills/tree/main/skills/webapp-testing"
	base, ok := vendored.ForURL(canonical)
	if !ok {
		t.Fatalf("expected vendored copy for %s", canonical)
	}
	variants := []string{
		canonical + "/",
		"  " + canonical + "  ",
		"github.com/anthropics/skills/tree/main/skills/webapp-testing",
		"https://GitHub.com/anthropics/skills/tree/main/skills/webapp-testing",
	}
	for _, v := range variants {
		got, ok := vendored.ForURL(v)
		if !ok || got != base {
			t.Errorf("ForURL(%q): expected the same vendored skill, ok=%v", v, ok)
		}
	}

	if _, ok := vendored.ForURL("https://github.com/anthropics/skills/tree/main/skills/Webapp-Testing"); ok {
		t.Error("ForURL must not case-fold the repository path")
	}
}

func TestLoadVendoredShipsLicensesOutsideSkillContent(t *testing.T) {
	fsys := fstest.MapFS{
		"vendored/manifest.json":                     &fstest.MapFile{Data: []byte(`{"skills":[{"source_url":"https://github.com/o/r/tree/main/skills/demo","dir":"demo","owner":"o","repo":"r","ref":"main","path":"skills/demo","commit":"abc123"}]}`)},
		"vendored/skills/demo/SKILL.md":              &fstest.MapFile{Data: []byte("---\nname: demo\n---\nbody\n")},
		"vendored/skills/demo/LICENSE":               &fstest.MapFile{Data: []byte("Apache License 2.0\n")},
		"vendored/skills/demo/NOTICE":                &fstest.MapFile{Data: []byte("notice\n")},
		"vendored/skills/demo/references/LICENSE.md": &fstest.MapFile{Data: []byte("MIT\n")},
		"vendored/skills/demo/references/guide.md":   &fstest.MapFile{Data: []byte("guide\n")},
	}

	reg, err := loadVendoredFromFS(fsys, "vendored")
	if err != nil {
		t.Fatalf("load vendored: %v", err)
	}
	skill, ok := reg.ForURL("https://github.com/o/r/tree/main/skills/demo")
	if !ok {
		t.Fatal("fixture skill not found")
	}

	var paths []string
	for _, f := range skill.Files {
		paths = append(paths, f.Path)
	}
	if len(paths) != 1 || paths[0] != "references/guide.md" {
		t.Errorf("imported files = %v, want only references/guide.md", paths)
	}

	for _, p := range []string{"vendored/skills/demo/LICENSE", "vendored/skills/demo/NOTICE"} {
		if _, err := fs.ReadFile(fsys, p); err != nil {
			t.Errorf("%s must stay in the vendored tree: %v", p, err)
		}
	}
}

func TestVendoredSkillFilesCarryNoLicenseText(t *testing.T) {
	vendored, err := LoadVendored()
	if err != nil {
		t.Fatalf("load vendored skills: %v", err)
	}
	for url, skill := range vendored.byURL {
		for _, f := range skill.Files {
			if isLicenseFileName(path.Base(f.Path)) {
				t.Errorf("%s: licence file %q imported as skill content", url, f.Path)
			}
		}
	}
}
