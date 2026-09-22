package cli

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

const CLIConfigSchemaVersion = 1

var cliConfigMigrations = []func(raw map[string]json.RawMessage) error{

	func(raw map[string]json.RawMessage) error { return nil },
}

func errFutureCLIConfig(path string, version int) error {
	return fmt.Errorf(
		"CLI config %s has schema_version %d, but this binary supports up to %d — it was written by a newer version of goosar; upgrade the CLI instead of editing the config with this one",
		path, version, CLIConfigSchemaVersion,
	)
}

func rawSchemaVersion(raw map[string]json.RawMessage) (int, error) {
	v, ok := raw["schema_version"]
	if !ok {
		return 0, nil
	}
	var version int
	if err := json.Unmarshal(v, &version); err != nil {
		return 0, fmt.Errorf("parse schema_version: %w", err)
	}
	if version < 0 {
		return 0, fmt.Errorf("schema_version must be non-negative, got %d", version)
	}
	return version, nil
}

func migrateCLIConfigDocument(raw map[string]json.RawMessage, from int) error {
	for v := from; v < CLIConfigSchemaVersion; v++ {
		if err := cliConfigMigrations[v](raw); err != nil {
			return fmt.Errorf("migrate CLI config v%d->v%d: %w", v, v+1, err)
		}
	}
	return nil
}

func knownJSONKeys(t reflect.Type) map[string]bool {
	keys := make(map[string]bool, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if name == "" {
			name = f.Name
		}
		keys[name] = true
	}
	return keys
}

func unknownJSONFields(data []byte, t reflect.Type) map[string]json.RawMessage {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	known := knownJSONKeys(t)
	for k := range raw {
		if known[k] {
			delete(raw, k)
		}
	}
	if len(raw) == 0 {
		return nil
	}
	return raw
}

func mergeUnknownJSONFields(known []byte, unknown map[string]json.RawMessage) ([]byte, error) {
	if len(unknown) == 0 {
		return known, nil
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(known, &doc); err != nil {
		return nil, err
	}
	for k, v := range unknown {
		if _, exists := doc[k]; !exists {
			doc[k] = v
		}
	}
	return json.Marshal(doc)
}
