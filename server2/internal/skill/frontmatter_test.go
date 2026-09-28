package skill

import "testing"

func TestParseFrontmatter(t *testing.T) {
	content := "---\nname: My Skill\ndescription: Does things\n---\n# Body\n\nHello.\n"
	name, desc, body := parseFrontmatter(content)
	if name != "My Skill" {
		t.Errorf("name = %q", name)
	}
	if desc != "Does things" {
		t.Errorf("description = %q", desc)
	}
	if body != "# Body\n\nHello.\n" {
		t.Errorf("body = %q", body)
	}
}

func TestParseFrontmatter_NoFrontmatter(t *testing.T) {
	content := "# Just a body\n"
	name, desc, body := parseFrontmatter(content)
	if name != "" || desc != "" {
		t.Errorf("expected empty name/description, got %q/%q", name, desc)
	}
	if body != content {
		t.Errorf("body should be returned unchanged, got %q", body)
	}
}

func TestParseFrontmatter_QuotedValues(t *testing.T) {
	content := "---\nname: \"Quoted Name\"\ndescription: 'Single quoted'\n---\nBody text\n"
	name, desc, body := parseFrontmatter(content)
	if name != "Quoted Name" {
		t.Errorf("name = %q, want %q", name, "Quoted Name")
	}
	if desc != "Single quoted" {
		t.Errorf("description = %q, want %q", desc, "Single quoted")
	}
	if body != "Body text\n" {
		t.Errorf("body = %q", body)
	}
}

func TestParseFrontmatter_MissingClosingFence(t *testing.T) {
	// Незакрытый фронтматтер — весь текст трактуется как обычное тело, не
	// как метаданные (extractBundle/FromZip тогда используют имя из
	// deriveNameFromZip, а не пустую строку из полуразобранного блока).
	content := "---\nname: Broken\nNo closing fence here.\n"
	name, desc, body := parseFrontmatter(content)
	if name != "" || desc != "" {
		t.Errorf("expected empty name/description without a closing fence, got %q/%q", name, desc)
	}
	if body != content {
		t.Errorf("body should be returned unchanged, got %q", body)
	}
}

func TestParseFrontmatter_IgnoresUnknownKeys(t *testing.T) {
	content := "---\nauthor: Someone\nname: Named\nversion: 2\n---\nBody\n"
	name, _, _ := parseFrontmatter(content)
	if name != "Named" {
		t.Errorf("name = %q, want %q (unknown keys should be ignored, not break parsing)", name, "Named")
	}
}
