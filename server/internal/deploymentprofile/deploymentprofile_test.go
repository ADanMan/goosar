package deploymentprofile

import "testing"

func TestParseAcceptsTheFourProfiles(t *testing.T) {
	for raw, want := range map[string]Profile{
		"perimeter": Perimeter,
		"  Demo  ":  Demo,
		"DEV":       Dev,
		"local":     Local,
	} {
		got, err := Parse(raw)
		if err != nil {
			t.Fatalf("Parse(%q) error: %v", raw, err)
		}
		if got != want {
			t.Fatalf("Parse(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestParseEmptyIsPerimeter(t *testing.T) {
	got, err := Parse("")
	if err != nil {
		t.Fatalf("Parse(\"\") error: %v", err)
	}
	if got != Perimeter {
		t.Fatalf("Parse(\"\") = %q, want %q", got, Perimeter)
	}
}

func TestParseRejectsUnknown(t *testing.T) {
	if _, err := Parse("prod"); err == nil {
		t.Fatal("Parse(\"prod\") = nil error; want a rejection")
	}
}

func TestFromEnvReadsTheVariable(t *testing.T) {
	t.Setenv(EnvVar, "demo")
	got, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv error: %v", err)
	}
	if got != Demo {
		t.Fatalf("FromEnv = %q, want %q", got, Demo)
	}
}
