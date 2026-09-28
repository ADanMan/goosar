package workspace

import "testing"

// slugCase — валидность одного значения slug плюс человекочитаемая причина,
// печатаемая при провале (табличная форма вместо списков valid/invalid).
type slugCase struct {
	value string
	valid bool
}

func TestWorkspaceSlugValidation(t *testing.T) {
	table := []slugCase{
		{"acme", true},
		{"acme-workspace", true},
		{"a1-b2-c3", true},
		{"", false},
		{"Acme", false},
		{"acme_workspace", false},
		{"-acme", false},
		{"acme-", false},
		{"acme--workspace", false},
		{"acme workspace", false},
	}
	for _, c := range table {
		if got := slugRe.MatchString(c.value); got != c.valid {
			t.Errorf("slugRe.MatchString(%q) = %v, want %v", c.value, got, c.valid)
		}
	}
}

func TestIssuePrefixDerivation(t *testing.T) {
	type namePrefix struct{ name, prefix string }
	fixtures := []namePrefix{
		{"Acme Corp", "AC"},
		{"Acme Corp Widgets X", "ACW"},
		{"solo", "S"},
		{"   ", "TSK"},
	}
	for _, f := range fixtures {
		if got := issuePrefixFromName(f.name); got != f.prefix {
			t.Errorf("issuePrefixFromName(%q): got %q, want %q", f.name, got, f.prefix)
		}
	}
}

func TestReservedSlugList(t *testing.T) {
	mustBeReserved := []string{"api", "admin", "ws"}
	for _, s := range mustBeReserved {
		if !reservedSlugs[s] {
			t.Errorf("%q должен входить в reservedSlugs", s)
		}
	}
	if reservedSlugs["acme"] || reservedSlugs["contract-test-workspace"] {
		t.Error("обычные пользовательские slug не должны попадать в reservedSlugs")
	}
}
