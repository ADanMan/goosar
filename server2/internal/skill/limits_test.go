package skill

import "testing"

func TestIsBinaryPath(t *testing.T) {
	cases := map[string]bool{
		"SKILL.md":        false,
		"README.md":       false,
		"logo.png":        true,
		"assets/icon.SVG": true,
		"archive.zip":     true,
		"noext":           false,
	}
	for path, want := range cases {
		if got := isBinaryPath(path); got != want {
			t.Errorf("isBinaryPath(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestIsLicensePath(t *testing.T) {
	cases := map[string]bool{
		"LICENSE":         true,
		"license.md":      true,
		"LICENSE.txt":     true,
		"sub/dir/LICENSE": true,
		"LICENSED.md":     false,
		"SKILL.md":        false,
	}
	for path, want := range cases {
		if got := isLicensePath(path); got != want {
			t.Errorf("isLicensePath(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestValidatePath(t *testing.T) {
	valid := []string{"a.md", "sub/dir/file.md", "./x.md"}
	invalid := []string{"", "/abs/path.md", "../escape.md", "sub/../../escape.md"}
	for _, p := range valid {
		if err := ValidatePath(p); err != nil {
			t.Errorf("ValidatePath(%q) = %v, want nil", p, err)
		}
	}
	for _, p := range invalid {
		if err := ValidatePath(p); err == nil {
			t.Errorf("ValidatePath(%q) = nil, want error", p)
		}
	}
}
