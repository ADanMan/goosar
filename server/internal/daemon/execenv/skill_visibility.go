package execenv

import (
	"strings"

	"gopkg.in/yaml.v3"
)

const disableModelInvocationKey = "disable-model-invocation"

func modelVisibleSkills(skills []SkillContextForEnv) []SkillContextForEnv {
	if len(skills) == 0 {
		return nil
	}
	visible := make([]SkillContextForEnv, 0, len(skills))
	for _, skill := range skills {
		if skillModelInvocationVisible(skill) {
			visible = append(visible, skill)
		}
	}
	return visible
}

func skillModelInvocationVisible(skill SkillContextForEnv) bool {
	return !frontmatterOptsOutOfModelInvocation(skill.Content)
}

func frontmatterOptsOutOfModelInvocation(content string) bool {
	fmBody, _, ok := frontmatterParts(content)
	if !ok || strings.TrimSpace(fmBody) == "" {
		return false
	}

	var meta map[string]any
	if err := yaml.Unmarshal([]byte(fmBody), &meta); err != nil {
		return false
	}

	raw, present := meta[disableModelInvocationKey]
	if !present {
		return false
	}

	switch flag := raw.(type) {
	case bool:
		return flag
	case string:
		return strings.EqualFold(strings.TrimSpace(flag), "true")
	default:
		return false
	}
}
