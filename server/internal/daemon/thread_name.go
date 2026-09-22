package daemon

import "strings"

const codexThreadNameMaxRunes = 120

func deriveTaskThreadName(task Task) string {
	if name := normalizeThreadName(task.ThreadName, codexThreadNameMaxRunes); name != "" {
		return name
	}
	if name := normalizeThreadName(task.AutopilotTitle, codexThreadNameMaxRunes); name != "" {
		return name
	}
	if name := normalizeThreadName(task.QuickCreatePrompt, codexThreadNameMaxRunes); name != "" {
		return name
	}
	if name := normalizeThreadName(task.ChatMessage, codexThreadNameMaxRunes); name != "" {
		return name
	}
	return normalizeThreadName(task.TriggerCommentContent, codexThreadNameMaxRunes)
}

func normalizeThreadName(s string, maxRunes int) string {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return ""
	}
	return truncateThreadName(strings.Join(fields, " "), maxRunes)
}

func truncateThreadName(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return s
	}

	rs := []rune(s)
	if len(rs) <= maxRunes {
		return s
	}
	if maxRunes <= 3 {
		return string(rs[:maxRunes])
	}
	return string(rs[:maxRunes-3]) + "..."
}
