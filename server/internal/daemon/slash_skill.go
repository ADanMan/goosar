package daemon

import (
	"regexp"
	"strings"
)

var slashSkillLinkPattern = regexp.MustCompile(`\[/((?:[^\]\\]|\\.)+)\]\(slash://skill/([^)]+)\)`)

type SlashSkillRef struct {
	Label string
	ID    string
}

func ExtractSlashSkills(md string) []SlashSkillRef {
	var refs []SlashSkillRef
	seen := map[string]bool{}

	for _, match := range slashSkillLinkPattern.FindAllStringSubmatch(md, -1) {
		id := match[2]
		if seen[id] {
			continue
		}
		seen[id] = true
		refs = append(refs, SlashSkillRef{Label: unescapeSlashLabel(match[1]), ID: id})
	}

	return refs
}

func unescapeSlashLabel(raw string) string {
	var out strings.Builder
	out.Grow(len(raw))

	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if c == '\\' && i+1 < len(raw) && (raw[i+1] == '[' || raw[i+1] == ']') {
			i++
			c = raw[i]
		}
		out.WriteByte(c)
	}

	return out.String()
}
