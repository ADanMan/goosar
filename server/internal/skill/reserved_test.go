package skill

import "testing"

func TestIsReservedContentPath(t *testing.T) {
	reserved := []string{
		"SKILL.md",
		"skill.md",
		"SKILL.MD",
		"./SKILL.md",
		"foo/../SKILL.md",
	}
	for _, p := range reserved {
		if !IsReservedContentPath(p) {
			t.Errorf("IsReservedContentPath(%q) = false, want true", p)
		}
	}

	notReserved := []string{
		"README.md",
		"docs/SKILL.md",
		"skills/SKILL.md",
		"SKILL.md.bak",
		"",
	}
	for _, p := range notReserved {
		if IsReservedContentPath(p) {
			t.Errorf("IsReservedContentPath(%q) = true, want false", p)
		}
	}
}
