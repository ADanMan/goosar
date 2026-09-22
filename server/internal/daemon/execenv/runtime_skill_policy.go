package execenv

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const claudeRuntimeSkillSettingsFile = "claude-runtime-skill-settings.json"

type RuntimeSkillRefForEnv struct {
	Root   string
	Key    string
	Name   string
	Plugin string
}

func cleanRuntimeSkillKey(key string) (string, bool) {
	cleaned := filepath.Clean(filepath.FromSlash(strings.TrimSpace(key)))
	switch {
	case cleaned == ".", filepath.IsAbs(cleaned), cleaned == "..":
		return "", false
	case strings.HasPrefix(cleaned, ".."+string(filepath.Separator)):
		return "", false
	default:
		return filepath.ToSlash(cleaned), true
	}
}

type denyRuleSet struct {
	rules []string
	seen  map[string]struct{}
}

func (d *denyRuleSet) addSkill(invocationName string) {
	for _, rule := range []string{"Skill(" + invocationName + ")", "Skill(" + invocationName + " *)"} {
		if _, exists := d.seen[rule]; exists {
			continue
		}
		if d.seen == nil {
			d.seen = make(map[string]struct{})
		}
		d.seen[rule] = struct{}{}
		d.rules = append(d.rules, rule)
	}
}

func prepareClaudeSkillSettings(envRoot string, disabled []RuntimeSkillRefForEnv, workspaceSkills []SkillContextForEnv) (string, error) {
	path := filepath.Join(envRoot, claudeRuntimeSkillSettingsFile)

	overrides := make(map[string]string)
	var deny denyRuleSet
	for _, skill := range disabled {
		key, ok := cleanRuntimeSkillKey(skill.Key)
		if !ok {
			continue
		}
		invocationName := strings.TrimSpace(skill.Name)
		if invocationName == "" {
			invocationName = filepath.Base(filepath.FromSlash(key))
		}
		if workspaceClaimsRuntimeSkill(invocationName, workspaceSkills) {
			continue
		}

		if skill.Root == "plugin" {
			invocationName = key
		} else {
			overrides[invocationName] = "off"
		}
		deny.addSkill(invocationName)
	}

	if len(overrides) == 0 && len(deny.rules) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return "", err
		}
		return "", nil
	}

	data, err := json.Marshal(map[string]any{
		"skillOverrides": overrides,
		"permissions": map[string]any{
			"deny": deny.rules,
		},
	})
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func resolveDisabledCodexSkillPath(skill RuntimeSkillRefForEnv, key, codexHome string, workspaceSkills []SkillContextForEnv) (path string, ok bool, err error) {
	switch skill.Root {
	case "provider":
		firstKeyPart := strings.SplitN(key, "/", 2)[0]
		if workspaceClaimsRuntimeSkill(firstKeyPart, workspaceSkills) {
			return "", false, nil
		}
		return filepath.Join(codexHome, "skills", filepath.FromSlash(key), "SKILL.md"), true, nil
	case "universal":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false, fmt.Errorf("resolve user home for disabled Codex skills: %w", err)
		}
		return filepath.Join(home, ".agents", "skills", filepath.FromSlash(key), "SKILL.md"), true, nil
	default:
		return "", false, nil
	}
}

func ensureCodexDisabledSkillsConfig(configPath, codexHome string, disabled []RuntimeSkillRefForEnv, workspaceSkills []SkillContextForEnv) error {
	if len(disabled) == 0 {
		return nil
	}

	seenPaths := make(map[string]struct{}, len(disabled))
	var paths []string
	for _, skill := range disabled {
		key, ok := cleanRuntimeSkillKey(skill.Key)
		if !ok {
			continue
		}
		skillPath, ok, err := resolveDisabledCodexSkillPath(skill, key, codexHome, workspaceSkills)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		if _, dup := seenPaths[skillPath]; dup {
			continue
		}
		seenPaths[skillPath] = struct{}{}
		paths = append(paths, skillPath)
	}
	if len(paths) == 0 {
		return nil
	}

	file, err := os.OpenFile(configPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()

	for _, path := range paths {
		entry := fmt.Sprintf("\n[[skills.config]]\npath = %s\nenabled = false\n", strconv.Quote(filepath.ToSlash(path)))
		if _, err := file.WriteString(entry); err != nil {
			return err
		}
	}
	return nil
}

func workspaceClaimsRuntimeSkill(name string, workspaceSkills []SkillContextForEnv) bool {
	claim := sanitizeSkillName(name)
	for _, skill := range workspaceSkills {
		if sanitizeSkillName(skill.Name) == claim {
			return true
		}
	}
	return false
}
