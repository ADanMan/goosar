package agenttmpl

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestLoad_RealTemplates(t *testing.T) {

	reg, err := Load()
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if len(reg.List()) == 0 {
		t.Fatal("expected at least one bundled template, got none")
	}
}

func TestLoadFromFS_Valid(t *testing.T) {
	fsys := fstest.MapFS{
		"templates/alpha.json": &fstest.MapFile{Data: []byte(`{
			"slug": "alpha",
			"name": "Alpha",
			"description": "first",
			"instructions": "do alpha",
			"skills": [{"source_url": "https://github.com/x/y/tree/main/skills/z"}]
		}`)},
		"templates/beta.json": &fstest.MapFile{Data: []byte(`{
			"slug": "beta",
			"name": "Beta",
			"description": "second",
			"instructions": "do beta",
			"skills": [{"source_url": "https://github.com/x/y/tree/main/skills/q"}]
		}`)},
	}

	reg, err := loadFromFS(fsys, "templates")
	if err != nil {
		t.Fatalf("loadFromFS: %v", err)
	}
	if got, want := len(reg.List()), 2; got != want {
		t.Fatalf("List() len = %d, want %d", got, want)
	}

	if reg.List()[0].Slug != "alpha" {
		t.Errorf("List()[0].Slug = %q, want alpha", reg.List()[0].Slug)
	}
	if _, ok := reg.Get("alpha"); !ok {
		t.Errorf("Get(alpha) = false, want true")
	}
	if _, ok := reg.Get("nope"); ok {
		t.Errorf("Get(nope) = true, want false")
	}
}

func TestLoadFromFS_Invalid(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr string
	}{
		{
			name:    "bad json",
			content: `{not json`,
			wantErr: "parse",
		},
		{
			name:    "missing slug",
			content: `{"name": "X", "instructions": "do", "skills": [{"source_url":"u"}]}`,
			wantErr: "missing slug",
		},
		{
			name:    "slug mismatches filename",
			content: `{"slug":"other","name":"X","instructions":"do","skills":[{"source_url":"u"}]}`,
			wantErr: "does not match filename",
		},
		{
			name:    "bad slug",
			content: `{"slug":"Bad_Slug","name":"X","instructions":"do","skills":[{"source_url":"u"}]}`,
			wantErr: "kebab-case",
		},
		{
			name:    "missing name",
			content: `{"slug":"x","instructions":"do","skills":[{"source_url":"u"}]}`,
			wantErr: "missing name",
		},
		{
			name:    "missing instructions",
			content: `{"slug":"x","name":"X","skills":[{"source_url":"u"}]}`,
			wantErr: "missing instructions",
		},
		{
			name:    "skill missing url",
			content: `{"slug":"x","name":"X","instructions":"do","skills":[{}]}`,
			wantErr: "missing source_url",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			filename := "x.json"
			if tc.name == "slug mismatches filename" {
				filename = "x.json"
			}
			fsys := fstest.MapFS{
				"templates/" + filename: &fstest.MapFile{Data: []byte(tc.content)},
			}
			_, err := loadFromFS(fsys, "templates")
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %v, want substring %q", err, tc.wantErr)
			}
		})
	}
}

func TestLoadFromFS_PromptOnlyTemplate(t *testing.T) {

	fsys := fstest.MapFS{
		"templates/prompt-only.json": &fstest.MapFile{Data: []byte(`{
			"slug":"prompt-only",
			"name":"Prompt Only",
			"description":"no skills here",
			"instructions":"just write good prose",
			"skills":[]
		}`)},
	}
	reg, err := loadFromFS(fsys, "templates")
	if err != nil {
		t.Fatalf("loadFromFS: %v", err)
	}
	tmpl, ok := reg.Get("prompt-only")
	if !ok {
		t.Fatal("Get(prompt-only) = false, want true")
	}
	if len(tmpl.Skills) != 0 {
		t.Errorf("len(Skills) = %d, want 0", len(tmpl.Skills))
	}
}

func TestLoadFromFS_DuplicateSlug(t *testing.T) {

	fsys := fstest.MapFS{
		"templates/a.json": &fstest.MapFile{Data: []byte(`{
			"slug":"a","name":"A","instructions":"do","skills":[{"source_url":"u"}]
		}`)},
		"templates/b.json": &fstest.MapFile{Data: []byte(`{
			"slug":"a","name":"A2","instructions":"do","skills":[{"source_url":"u"}]
		}`)},
	}
	_, err := loadFromFS(fsys, "templates")
	if err == nil || !strings.Contains(err.Error(), "duplicate slug") {

		if err == nil || !strings.Contains(err.Error(), "does not match filename") {
			t.Errorf("expected duplicate slug or filename mismatch, got %v", err)
		}
	}
}
