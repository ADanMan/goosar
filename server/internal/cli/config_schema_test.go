package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, tmp, body string) string {
	t.Helper()
	cfgDir := filepath.Join(tmp, ".goosar")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cfgDir, "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCLIConfigSchema_V0LoadsIntoCurrentShape(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	writeConfig(t, tmp, `{"server_url": "https://goosar.ru", "token": "gsl_x", "device_name": "box"}`)

	конфиг, err := LoadCLIConfig()
	if err != nil {
		t.Fatalf("LoadCLIConfig on v0 file: %v", err)
	}
	if конфиг.ServerURL != "https://goosar.ru" || конфиг.Token != "gsl_x" || конфиг.DeviceName != "box" {
		t.Errorf("v0 settings lost on load: %+v", конфиг)
	}
	if конфиг.SchemaVersion != CLIConfigSchemaVersion {
		t.Errorf("SchemaVersion after migration: got %d, want %d", конфиг.SchemaVersion, CLIConfigSchemaVersion)
	}
}

func TestCLIConfigSchema_SaveStampsCurrentVersion(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	if err := SaveCLIConfig(CLIConfig{ServerURL: "https://goosar.ru"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(tmp, ".goosar", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	version, err := rawSchemaVersion(raw)
	if err != nil {
		t.Fatal(err)
	}
	if version != CLIConfigSchemaVersion {
		t.Errorf("saved schema_version: got %d, want %d", version, CLIConfigSchemaVersion)
	}
}

func TestCLIConfigSchema_FutureVersionRefusedOnLoad(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	writeConfig(t, tmp, `{"schema_version": 99, "server_url": "https://goosar.ru"}`)

	_, err := LoadCLIConfig()
	if err == nil {
		t.Fatal("expected error loading future-version config")
	}
	if !strings.Contains(err.Error(), "newer version") {
		t.Errorf("error should name the cause, got: %v", err)
	}
}

func TestCLIConfigSchema_FutureVersionRefusedOnSave(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	future := `{"schema_version": 99, "server_url": "https://goosar.ru", "new_field": "keep"}`
	path := writeConfig(t, tmp, future)

	конфиг, _ := LoadCLIConfig()
	if err := SaveCLIConfig(конфиг); err == nil {
		t.Fatal("expected Save to refuse overwriting a future-version config")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != future {
		t.Errorf("future-version config was modified on disk:\n%s", data)
	}
}

func TestCLIConfigSchema_InvalidVersionRejected(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	writeConfig(t, tmp, `{"schema_version": -1}`)
	if _, err := LoadCLIConfig(); err == nil {
		t.Fatal("expected error for negative schema_version")
	}
}

func TestCLIConfigSchema_UnknownTopLevelFieldRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	writeConfig(t, tmp, `{"server_url": "https://goosar.ru", "future_knob": {"a": 1}}`)

	конфиг, err := LoadCLIConfig()
	if err != nil {
		t.Fatal(err)
	}
	конфиг.DeviceName = "renamed"
	if err := SaveCLIConfig(конфиг); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(tmp, ".goosar", "config.json"))
	if !strings.Contains(string(data), `"future_knob"`) {
		t.Errorf("unknown field dropped on round-trip:\n%s", data)
	}
	if !strings.Contains(string(data), `"renamed"`) {
		t.Errorf("edit lost on save:\n%s", data)
	}
}
