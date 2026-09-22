package execenv

import (
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

func seedUserCodexSkills(codexHome string, workspaceSkills []SkillContextForEnv, logger *slog.Logger) error {
	sharedSkillsDir := filepath.Join(resolveSharedCodexHome(), "skills")

	switch info, err := os.Stat(sharedSkillsDir); {
	case err != nil && os.IsNotExist(err):
		return nil
	case err != nil:
		return fmt.Errorf("stat shared skills dir: %w", err)
	case !info.IsDir():
		return nil
	}

	entries, err := os.ReadDir(sharedSkillsDir)
	if err != nil {
		return fmt.Errorf("read shared skills dir: %w", err)
	}

	reservedNames := reservedSkillNameSet(workspaceSkills)
	targetSkillsDir := filepath.Join(codexHome, "skills")
	for _, entry := range entries {
		seedOneUserCodexSkill(sharedSkillsDir, targetSkillsDir, entry.Name(), reservedNames, logger)
	}
	return nil
}

func reservedSkillNameSet(workspaceSkills []SkillContextForEnv) map[string]struct{} {
	reserved := make(map[string]struct{}, len(workspaceSkills))
	for _, skill := range workspaceSkills {
		reserved[sanitizeSkillName(skill.Name)] = struct{}{}
	}
	return reserved
}

func seedOneUserCodexSkill(sharedSkillsDir, targetSkillsDir, name string, reservedNames map[string]struct{}, logger *slog.Logger) {
	if name == "" || strings.HasPrefix(name, ".") {
		return
	}
	if _, claimed := reservedNames[sanitizeSkillName(name)]; claimed {
		logger.Info("execenv: codex user-skill yields to workspace skill", "name", name)
		return
	}

	src := filepath.Join(sharedSkillsDir, name)
	resolved, err := filepath.EvalSymlinks(src)
	if err != nil {
		logger.Warn("execenv: codex user-skill resolve failed", "name", name, "error", err)
		return
	}
	if fi, err := os.Stat(resolved); err != nil || !fi.IsDir() {
		return
	}

	dst := filepath.Join(targetSkillsDir, name)
	if err := os.RemoveAll(dst); err != nil {
		logger.Warn("execenv: codex user-skill clean dst failed", "name", name, "error", err)
		return
	}
	if err := copyDirTree(resolved, dst); err != nil {
		logger.Warn("execenv: codex user-skill copy failed", "name", name, "error", err)
	}
}

func copyDirTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return copyFile(path, target)
	})
}
