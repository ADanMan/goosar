package execenv

import (
	"fmt"
	"os"
	"strings"
)

const skillsConfigHeader = "[[skills.config]]"

func isTOMLTableHeader(trimmedLine string) bool {
	return strings.HasPrefix(trimmedLine, "[")
}

func stripSkillsConfigEntries(content string) string {
	if !strings.Contains(content, skillsConfigHeader) {
		return content
	}

	var kept []string
	skipping := false
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)

		if isTOMLTableHeader(trimmed) {

			skipping = trimmed == skillsConfigHeader
			if skipping {
				continue
			}
		} else if skipping {
			continue
		}

		kept = append(kept, line)
	}

	result := strings.TrimRight(strings.Join(kept, "\n"), "\n") + "\n"
	if strings.TrimSpace(result) == "" {
		return ""
	}
	return result
}

func sanitizeCopiedCodexConfig(configPath string) error {
	original, err := os.ReadFile(configPath)
	switch {
	case err == nil:

	case os.IsNotExist(err):
		return nil
	default:
		return fmt.Errorf("read config.toml: %w", err)
	}

	cleaned := stripSkillsConfigEntries(string(original))
	if cleaned == string(original) {
		return nil
	}
	if err := os.WriteFile(configPath, []byte(cleaned), 0o644); err != nil {
		return fmt.Errorf("write config.toml: %w", err)
	}
	return nil
}
