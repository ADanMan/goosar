package agenttmpl

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	skillpkg "github.com/adanman/goosar/server/internal/skill"
)

//go:embed vendored/manifest.json all:vendored/skills
var vendoredFS embed.FS

type VendoredFile struct {
	Path    string
	Content string
}

type VendoredSkill struct {
	SourceURL   string
	Owner       string
	Repo        string
	Ref         string
	Path        string
	Commit      string
	Name        string
	Description string
	Content     string
	Files       []VendoredFile
}

type vendoredManifest struct {
	Skills []vendoredManifestEntry `json:"skills"`
}

type vendoredManifestEntry struct {
	SourceURL string `json:"source_url"`
	Dir       string `json:"dir"`
	Owner     string `json:"owner"`
	Repo      string `json:"repo"`
	Ref       string `json:"ref"`
	Path      string `json:"path"`
	Commit    string `json:"commit"`
}

type VendoredRegistry struct {
	byURL map[string]*VendoredSkill
}

func LoadVendored() (*VendoredRegistry, error) {
	return loadVendoredFromFS(vendoredFS, "vendored")
}

func loadVendoredFromFS(fsys fs.FS, dir string) (*VendoredRegistry, error) {
	raw, err := fs.ReadFile(fsys, path.Join(dir, "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("agenttmpl: read vendored manifest: %w", err)
	}
	var manifest vendoredManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, fmt.Errorf("agenttmpl: parse vendored manifest: %w", err)
	}

	reg := &VendoredRegistry{byURL: make(map[string]*VendoredSkill, len(manifest.Skills))}
	for _, entry := range manifest.Skills {
		if entry.SourceURL == "" || entry.Dir == "" {
			return nil, fmt.Errorf("agenttmpl: vendored manifest entry missing source_url or dir: %+v", entry)
		}
		key := normalizeVendoredSourceURL(entry.SourceURL)
		if _, dup := reg.byURL[key]; dup {
			return nil, fmt.Errorf("agenttmpl: duplicate vendored source_url %q", entry.SourceURL)
		}
		skill, err := loadVendoredSkill(fsys, path.Join(dir, "skills", entry.Dir), entry)
		if err != nil {
			return nil, err
		}
		reg.byURL[key] = skill
	}
	return reg, nil
}

func loadVendoredSkill(fsys fs.FS, skillDir string, entry vendoredManifestEntry) (*VendoredSkill, error) {
	skillMd, err := fs.ReadFile(fsys, path.Join(skillDir, "SKILL.md"))
	if err != nil {
		return nil, fmt.Errorf("agenttmpl: vendored skill %s: read SKILL.md: %w", entry.Dir, err)
	}
	if len(strings.TrimSpace(string(skillMd))) == 0 {
		return nil, fmt.Errorf("agenttmpl: vendored skill %s: SKILL.md is empty", entry.Dir)
	}

	name, description := skillpkg.ParseSkillFrontmatter(string(skillMd))
	if name == "" {

		name = path.Base(entry.Path)
	}

	skill := &VendoredSkill{
		SourceURL:   entry.SourceURL,
		Owner:       entry.Owner,
		Repo:        entry.Repo,
		Ref:         entry.Ref,
		Path:        entry.Path,
		Commit:      entry.Commit,
		Name:        name,
		Description: description,
		Content:     string(skillMd),
	}

	err = fs.WalkDir(fsys, skillDir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel := strings.TrimPrefix(p, skillDir+"/")
		if rel == "SKILL.md" {
			return nil
		}
		if isLicenseFileName(path.Base(rel)) {

			return nil
		}
		content, err := fs.ReadFile(fsys, p)
		if err != nil {
			return fmt.Errorf("read %s: %w", p, err)
		}
		skill.Files = append(skill.Files, VendoredFile{Path: rel, Content: string(content)})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("agenttmpl: vendored skill %s: %w", entry.Dir, err)
	}

	sort.Slice(skill.Files, func(i, j int) bool { return skill.Files[i].Path < skill.Files[j].Path })
	return skill, nil
}

func isLicenseFileName(base string) bool {
	lower := strings.ToLower(base)
	for _, stem := range []string{"license", "licence", "notice", "copying"} {
		if lower == stem || strings.HasPrefix(lower, stem+".") {
			return true
		}
	}
	return false
}

func (r *VendoredRegistry) ForURL(sourceURL string) (*VendoredSkill, bool) {
	skill, ok := r.byURL[normalizeVendoredSourceURL(sourceURL)]
	return skill, ok
}

func normalizeVendoredSourceURL(u string) string {
	u = strings.TrimRight(strings.TrimSpace(u), "/")
	for _, prefix := range []string{"https://github.com/", "https://www.github.com/", "http://github.com/", "github.com/"} {
		if len(u) >= len(prefix) && strings.EqualFold(u[:len(prefix)], prefix) {
			return "https://github.com/" + u[len(prefix):]
		}
	}
	return u
}
