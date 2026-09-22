// Пакет skill — общие утилиты для работы с файлами SKILL.md.
package skill

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

var frontmatterPattern = regexp.MustCompile(`(?s)\A---\r?\n(.*?\r?\n)---`)

func ParseSkillFrontmatter(content string) (name, description string) {
	if !strings.HasPrefix(content, "---") {
		return "", ""
	}
	match := frontmatterPattern.FindStringSubmatch(content)
	if match == nil {
		return "", ""
	}

	var fm map[string]any
	if err := yaml.Unmarshal([]byte(match[1]), &fm); err != nil {
		return "", ""
	}
	return coerceFrontmatterValue(fm["name"]), coerceFrontmatterValue(fm["description"])
}

func coerceFrontmatterValue(v any) string {
	switch val := v.(type) {
	case nil:
		return ""
	case string:
		return val
	case bool:
		return strconv.FormatBool(val)
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	case uint64:
		return strconv.FormatUint(val, 10)
	case float64:
		return strconv.FormatFloat(val, 'g', -1, 64)
	default:
		encoded, err := json.Marshal(val)
		if err != nil {
			return ""
		}
		return string(encoded)
	}
}
