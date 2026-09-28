package skill

import "strings"

// parseFrontmatter читает минимальный YAML-подобный фронтматтер SKILL.md
// (--- \n key: value \n ... \n ---), достаточный для имени/описания навыка.
// Не полноценный YAML-парсер (кавычки/вложенность не поддержаны) —
// оправданное упрощение: единственные два поля, которые здесь нужны, — это
// простые однострочные значения, как их описывает экосистема SKILL.md.
func parseFrontmatter(content string) (name, description string, body string) {
	body = content
	trimmed := strings.TrimLeft(content, "\n")
	if !strings.HasPrefix(trimmed, "---") {
		return "", "", content
	}
	rest := trimmed[3:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", "", content
	}
	block := rest[:end]
	afterIdx := strings.Index(rest[end+1:], "\n")
	after := ""
	if afterIdx >= 0 {
		after = rest[end+1+afterIdx+1:]
	}
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		switch strings.ToLower(k) {
		case "name":
			name = v
		case "description":
			description = v
		}
	}
	return name, description, strings.TrimLeft(after, "\n")
}
