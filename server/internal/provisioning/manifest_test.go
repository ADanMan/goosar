package provisioning

import (
	"encoding/json"
	"strings"
	"testing"
)

func validManifestJSON() string {
	return `{
		"schemaVersion": 1,
		"name": "office-docx",
		"version": "1.4.0",
		"type": "skill",
		"platform": "darwin-arm64",
		"sha256": "` + strings.Repeat("a", 64) + `",
		"size": 123,
		"requires": ["runtime:playwright-browsers@2.0.0"]
	}`
}

func TestParsePackageManifest_Valid(t *testing.T) {
	m, err := ParsePackageManifest([]byte(validManifestJSON()))
	if err != nil {
		t.Fatalf("ParsePackageManifest() error = %v", err)
	}
	if m.SchemaVersion != 1 || m.Name != "office-docx" || m.Version != "1.4.0" ||
		m.Type != "skill" || m.Platform != "darwin-arm64" || m.Size != 123 {
		t.Fatalf("unexpected manifest: %+v", m)
	}
	if len(m.Requires) != 1 || m.Requires[0] != "runtime:playwright-browsers@2.0.0" {
		t.Fatalf("unexpected requires: %+v", m.Requires)
	}
}

func TestParsePackageManifest_Malformed(t *testing.T) {
	cases := []struct {
		name string
		json string
	}{
		{"not json", `not json at all`},
		{"empty body", ``},
		{"json null", `null`},
		{"json array", `[1,2,3]`},
		{"wrong schema version", withField(validManifestJSON(), "schemaVersion", "2")},
		{"missing schema version", `{"name":"x","version":"1.0.0","type":"skill","platform":"*","sha256":"` + strings.Repeat("a", 64) + `","size":1}`},
		{"empty name", withStringField(validManifestJSON(), "name", "")},
		{"empty version", withStringField(validManifestJSON(), "version", "")},
		{"invalid type", withStringField(validManifestJSON(), "type", "malware")},
		{"invalid platform", withStringField(validManifestJSON(), "platform", "amiga-68k")},
		{"short sha256", withStringField(validManifestJSON(), "sha256", "abc123")},
		{"non-hex sha256", withStringField(validManifestJSON(), "sha256", strings.Repeat("z", 64))},
		{"uppercase sha256 rejected", withStringField(validManifestJSON(), "sha256", strings.Repeat("A", 64))},
		{"zero size", withField(validManifestJSON(), "size", "0")},
		{"negative size", withField(validManifestJSON(), "size", "-5")},
		{"malformed requires entry", withRequires(validManifestJSON(), `["not-a-valid-ref"]`)},
		{"requires missing version", withRequires(validManifestJSON(), `["skill:foo"]`)},
		{"requires unknown type", withRequires(validManifestJSON(), `["exploit:foo@1.0.0"]`)},
		{"path traversal in name", withStringField(validManifestJSON(), "name", "../../etc/passwd")},
		{"path traversal in version", withStringField(validManifestJSON(), "version", "../1.0.0")},
		{"slash in name", withStringField(validManifestJSON(), "name", "a/b")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParsePackageManifest([]byte(tc.json)); err == nil {
				t.Fatalf("ParsePackageManifest(%q) expected error, got nil", tc.json)
			}
		})
	}
}

func withField(json, field, rawValue string) string {
	return replaceJSONField(json, field, rawValue)
}

func withStringField(json, field, value string) string {
	return replaceJSONField(json, field, `"`+value+`"`)
}

func withRequires(json, rawArray string) string {
	return replaceJSONField(json, "requires", rawArray)
}

func replaceJSONField(json, field, rawValue string) string {
	marker := `"` + field + `":`
	idx := indexOf(json, marker)
	if idx < 0 {

		trimmed := trimRightSpaceAndBrace(json)
		return trimmed + `,"` + field + `":` + rawValue + `}`
	}
	start := idx + len(marker)

	depth := 0
	inString := false
	end := len(json)
	for i := start; i < len(json); i++ {
		c := json[i]
		switch {
		case c == '"' && (i == 0 || json[i-1] != '\\'):
			inString = !inString
		case inString:
			continue
		case c == '[' || c == '{':
			depth++
		case c == ']' || c == '}':
			if depth == 0 {
				end = i
				i = len(json)
				continue
			}
			depth--
		case c == ',' && depth == 0:
			end = i
			i = len(json)
		}
	}
	return json[:start] + rawValue + json[end:]
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func trimRightSpaceAndBrace(s string) string {
	i := len(s) - 1
	for i >= 0 && (s[i] == ' ' || s[i] == '\n' || s[i] == '\t') {
		i--
	}
	if i >= 0 && s[i] == '}' {
		return s[:i]
	}
	return s
}

func TestPackageManifest_RequireKeyAndBlobFilename(t *testing.T) {
	m := PackageManifest{Type: "skill", Name: "office-docx", Version: "1.4.0", Platform: "darwin-arm64"}
	if got, want := m.RequireKey(), "skill:office-docx@1.4.0"; got != want {
		t.Fatalf("RequireKey() = %q, want %q", got, want)
	}
	if got, want := m.BlobFilename(), "office-docx-1.4.0-darwin-arm64.tar.zst"; got != want {
		t.Fatalf("BlobFilename() = %q, want %q", got, want)
	}
}

func TestParseRequireRef(t *testing.T) {
	cases := []struct {
		ref         string
		wantType    string
		wantName    string
		wantVersion string
		wantErr     bool
	}{
		{"runtime:playwright-browsers@2.0.0", "runtime", "playwright-browsers", "2.0.0", false},
		{"skill:office-docx@1.4.0", "skill", "office-docx", "1.4.0", false},
		{"mcp-server:office@1.0.0", "mcp-server", "office", "1.0.0", false},
		{"badtype:foo@1.0.0", "", "", "", true},
		{"skill:foo", "", "", "", true},
		{"skill:@1.0.0", "", "", "", true},
		{"skill:foo@", "", "", "", true},
		{"", "", "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.ref, func(t *testing.T) {
			gotType, gotName, gotVersion, err := ParseRequireRef(tc.ref)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseRequireRef(%q) expected error, got nil", tc.ref)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseRequireRef(%q) unexpected error: %v", tc.ref, err)
			}
			if gotType != tc.wantType || gotName != tc.wantName || gotVersion != tc.wantVersion {
				t.Fatalf("ParseRequireRef(%q) = (%q,%q,%q), want (%q,%q,%q)",
					tc.ref, gotType, gotName, gotVersion, tc.wantType, tc.wantName, tc.wantVersion)
			}
		})
	}
}

func TestPackageManifest_MarshalJSON_RequiresAlwaysArray(t *testing.T) {
	cases := []struct {
		name     string
		manifest PackageManifest
	}{
		{"nil requires (zero value struct literal)", PackageManifest{
			SchemaVersion: 1, Name: "web-search", Version: "2.0.1",
			Type: "skill", Platform: "*", SHA256: strings.Repeat("a", 64), Size: 10,
		}},
		{"explicit empty requires slice", PackageManifest{
			SchemaVersion: 1, Name: "web-search", Version: "2.0.1",
			Type: "skill", Platform: "*", SHA256: strings.Repeat("a", 64), Size: 10,
			Requires: []string{},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.manifest)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}
			if !strings.Contains(string(data), `"requires":[]`) {
				t.Fatalf("json.Marshal() = %s, want it to contain \"requires\":[]", data)
			}
		})
	}
}

func TestPackageManifest_MarshalJSON_RequiresRoundTrip(t *testing.T) {
	raw := `{"schemaVersion":1,"name":"web-search","version":"2.0.1","type":"skill","platform":"*","sha256":"` +
		strings.Repeat("a", 64) + `","size":10}`
	m, err := ParsePackageManifest([]byte(raw))
	if err != nil {
		t.Fatalf("ParsePackageManifest() error = %v", err)
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if !strings.Contains(string(data), `"requires":[]`) {
		t.Fatalf("json.Marshal() = %s, want it to contain \"requires\":[]", data)
	}
	if strings.Contains(string(data), `"requires":null`) {
		t.Fatalf("json.Marshal() = %s, must never serialize requires as null", data)
	}
}

func TestPackageManifest_MarshalJSON_RequiresPreservesValues(t *testing.T) {
	m := PackageManifest{
		SchemaVersion: 1, Name: "office-docx", Version: "1.4.0",
		Type: "skill", Platform: "darwin-arm64", SHA256: strings.Repeat("a", 64), Size: 10,
		Requires: []string{"runtime:playwright-browsers@2.0.0"},
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if !strings.Contains(string(data), `"requires":["runtime:playwright-browsers@2.0.0"]`) {
		t.Fatalf("json.Marshal() = %s, want it to preserve the requires entries", data)
	}
}

func TestValidPlatform(t *testing.T) {
	for _, p := range []string{"*", "darwin-arm64", "darwin-x64", "win-x64", "linux-x64"} {
		if !ValidPlatform(p) {
			t.Errorf("ValidPlatform(%q) = false, want true", p)
		}
	}
	for _, p := range []string{"", "amiga-68k", "DARWIN-ARM64", "darwin_arm64"} {
		if ValidPlatform(p) {
			t.Errorf("ValidPlatform(%q) = true, want false", p)
		}
	}
}
