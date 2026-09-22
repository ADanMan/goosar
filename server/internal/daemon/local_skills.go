package daemon

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/adanman/goosar/server/internal/skill"
)

const (
	maxLocalSkillFileSize   int64 = 1 << 20
	maxLocalSkillBundleSize int64 = 8 << 20

	maxLocalSkillFileCount = 256

	maxLocalSkillDirDepth = 4
)

type runtimeLocalSkillSummary struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	SourcePath  string `json:"source_path"`
	Provider    string `json:"provider"`

	Root       string `json:"root,omitempty"`
	Plugin     string `json:"plugin,omitempty"`
	CanDisable bool   `json:"can_disable,omitempty"`
	FileCount  int    `json:"file_count"`
}

type runtimeLocalSkillBundle struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Content     string          `json:"content"`
	SourcePath  string          `json:"source_path"`
	Provider    string          `json:"provider"`
	Files       []SkillFileData `json:"files,omitempty"`
}

type localSkillRoot struct {
	path      string
	kind      string
	keyPrefix string
	plugin    string
}

const (
	localSkillRootProvider = "provider"

	localSkillRootUniversal = "universal"

	localSkillRootPlugin = "plugin"
)

func localSkillRootsForProvider(provider string) ([]localSkillRoot, bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, false, fmt.Errorf("resolve user home: %w", err)
	}

	descriptor, ok := runtimeDescriptor(provider)
	if !ok {
		return nil, false, nil
	}
	providerRoot, ok := descriptor.SkillsRoot(home, os.Getenv)
	if !ok {
		return nil, false, nil
	}

	roots := []localSkillRoot{
		{path: providerRoot, kind: localSkillRootProvider},
		{path: filepath.Join(home, ".agents", "skills"), kind: localSkillRootUniversal},
	}

	if pluginRegistry, ok := resolveRuntimeCPluginRegistry(provider, home, true); ok {
		for _, plugin := range pluginRegistry.listEnabledRuntimeCPlugins() {
			manifest, _ := pluginRegistry.readRuntimeCPluginManifest(plugin.InstallPath)
			for _, path := range pluginRegistry.skillRoots(plugin, manifest) {
				roots = append(roots, localSkillRoot{
					path:      path,
					kind:      localSkillRootPlugin,
					keyPrefix: plugin.Name + ":",
					plugin:    plugin.ID,
				})
			}
		}
	}
	return roots, true, nil
}

func runtimeLocalSkillsCanDisable(provider string) bool {
	d, ok := runtimeDescriptor(provider)
	return ok && d.SkillsCanDisable
}

func isIgnoredLocalSkillEntry(name string) bool {
	if name == "" {
		return true
	}
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch strings.ToLower(name) {
	case "license", "license.md", "license.txt":
		return true
	default:
		return false
	}
}

func normalizeLocalSkillKey(key string) (string, error) {
	if strings.TrimSpace(key) == "" {
		return "", fmt.Errorf("skill key is required")
	}
	cleaned := filepath.Clean(filepath.FromSlash(strings.TrimSpace(key)))
	if cleaned == "." || filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "..") {
		return "", fmt.Errorf("invalid skill key")
	}
	return filepath.ToSlash(cleaned), nil
}

func relativizeHomePath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.ToSlash(path)
	}
	if path == home {
		return "~"
	}
	prefix := home + string(filepath.Separator)
	if strings.HasPrefix(path, prefix) {
		return filepath.ToSlash("~" + string(filepath.Separator) + strings.TrimPrefix(path, prefix))
	}
	return filepath.ToSlash(path)
}

func readLocalSkillMainFile(skillDir string) (string, error) {
	mainPath := filepath.Join(skillDir, "SKILL.md")
	info, err := os.Stat(mainPath)
	if err != nil {
		return "", err
	}
	if info.Size() > maxLocalSkillFileSize {
		return "", fmt.Errorf("SKILL.md exceeds %d bytes", maxLocalSkillFileSize)
	}
	content, err := os.ReadFile(mainPath)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

func collectLocalSkillFiles(skillDir string, includeContent bool) ([]SkillFileData, error) {
	files := make([]SkillFileData, 0)
	var totalSize int64

	walkRoot := skillDir
	if resolved, err := filepath.EvalSymlinks(skillDir); err == nil {
		walkRoot = resolved
	}

	err := filepath.WalkDir(walkRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if path == walkRoot {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			if isIgnoredLocalSkillEntry(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if isIgnoredLocalSkillEntry(entry.Name()) || strings.EqualFold(entry.Name(), "SKILL.md") {
			return nil
		}

		rel, err := filepath.Rel(walkRoot, path)
		if err != nil {
			return nil
		}
		rel = filepath.Clean(rel)
		if rel == "." || filepath.IsAbs(rel) || strings.HasPrefix(rel, "..") {
			return nil
		}

		info, err := entry.Info()
		if err != nil || info.Size() > maxLocalSkillFileSize {
			return nil
		}
		if len(files) >= maxLocalSkillFileCount {
			return fmt.Errorf("local skill exceeds %d files", maxLocalSkillFileCount)
		}
		totalSize += info.Size()
		if totalSize > maxLocalSkillBundleSize {
			return fmt.Errorf("local skill exceeds %d bytes in total", maxLocalSkillBundleSize)
		}

		file := SkillFileData{Path: filepath.ToSlash(rel)}
		if includeContent {
			content, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			file.Content = string(content)
		}
		files = append(files, file)
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})
	return files, nil
}

func listRuntimeLocalSkills(provider string) ([]runtimeLocalSkillSummary, bool, error) {
	roots, supported, err := localSkillRootsForProvider(provider)
	if err != nil || !supported {
		return nil, supported, err
	}

	skills := make([]runtimeLocalSkillSummary, 0)

	seenKeys := make(map[string]bool)
	for _, root := range roots {
		if _, err := os.Stat(root.path); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, true, err
		}

		rootSkills := make([]runtimeLocalSkillSummary, 0)
		visited := make(map[string]bool)
		enumerateLocalSkills(provider, root, root.path, root.path, 0, visited, &rootSkills)

		for _, s := range rootSkills {
			if seenKeys[s.Key] {
				continue
			}
			seenKeys[s.Key] = true
			skills = append(skills, s)
		}
	}

	sort.Slice(skills, func(i, j int) bool {
		return skills[i].Key < skills[j].Key
	})
	return skills, true, nil
}

func enumerateLocalSkills(
	provider string,
	root localSkillRoot,
	walkRoot, currentDir string,
	depth int,
	visited map[string]bool,
	skills *[]runtimeLocalSkillSummary,
) {
	if depth > maxLocalSkillDirDepth {
		return
	}
	resolved, err := filepath.EvalSymlinks(currentDir)
	if err != nil {
		return
	}
	if visited[resolved] {
		return
	}
	visited[resolved] = true

	entries, err := os.ReadDir(currentDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		name := entry.Name()
		if isIgnoredLocalSkillEntry(name) {
			continue
		}
		path := filepath.Join(currentDir, name)
		info, statErr := os.Stat(path)
		if statErr != nil || !info.IsDir() {
			continue
		}

		mainPath := filepath.Join(path, "SKILL.md")
		if _, err := os.Stat(mainPath); err == nil {
			rel, err := filepath.Rel(walkRoot, path)
			if err != nil {
				continue
			}
			key, err := normalizeLocalSkillKey(rel)
			if err != nil {
				continue
			}
			key = root.keyPrefix + key

			content, err := readLocalSkillMainFile(path)
			if err != nil {
				continue
			}
			skillName, description := skill.ParseSkillFrontmatter(content)
			if root.plugin != "" {
				skillName = key
			} else if skillName == "" {
				skillName = filepath.Base(path)
			}

			files, err := collectLocalSkillFiles(path, false)
			if err != nil {
				continue
			}

			*skills = append(*skills, runtimeLocalSkillSummary{
				Key:         key,
				Name:        skillName,
				Description: description,
				SourcePath:  relativizeHomePath(path),
				Provider:    provider,
				Root:        root.kind,
				Plugin:      root.plugin,
				CanDisable:  runtimeLocalSkillsCanDisable(provider),

				FileCount: len(files) + 1,
			})
			continue
		}

		enumerateLocalSkills(provider, root, walkRoot, path, depth+1, visited, skills)
	}
}

func loadRuntimeLocalSkillBundle(provider, skillKey string) (*runtimeLocalSkillBundle, bool, error) {
	roots, supported, err := localSkillRootsForProvider(provider)
	if err != nil || !supported {
		return nil, supported, err
	}

	key, err := normalizeLocalSkillKey(skillKey)
	if err != nil {
		return nil, true, err
	}

	for _, root := range roots {
		rootKey := key
		if root.keyPrefix != "" {
			if !strings.HasPrefix(key, root.keyPrefix) {
				continue
			}
			rootKey = strings.TrimPrefix(key, root.keyPrefix)
		}
		skillDir := filepath.Join(root.path, filepath.FromSlash(rootKey))
		info, err := os.Stat(skillDir)
		if err != nil {

			if os.IsNotExist(err) {
				continue
			}
			return nil, true, err
		}
		if !info.IsDir() {

			continue
		}

		mainPath := filepath.Join(skillDir, "SKILL.md")
		if _, err := os.Stat(mainPath); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, true, err
		}

		content, err := readLocalSkillMainFile(skillDir)
		if err != nil {
			return nil, true, err
		}
		name, description := skill.ParseSkillFrontmatter(content)
		if root.plugin != "" {
			name = key
		} else if name == "" {
			name = filepath.Base(skillDir)
		}

		files, err := collectLocalSkillFiles(skillDir, true)
		if err != nil {
			return nil, true, err
		}

		return &runtimeLocalSkillBundle{
			Name:        name,
			Description: description,
			Content:     content,
			SourcePath:  relativizeHomePath(skillDir),
			Provider:    provider,
			Files:       files,
		}, true, nil
	}

	return nil, true, fmt.Errorf("local skill not found")
}
