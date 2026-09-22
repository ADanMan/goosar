package mcplibrary

import (
	"encoding/json"
	"strings"

	"github.com/adanman/goosar/server/internal/handler"
)

const addressPlaceholder = "\x00address\x00"

func normalizeForCompare(config map[string]any, addressKeys []string) map[string]any {
	out := deepCopyMap(config)
	for _, path := range addressKeys {
		segments := strings.SplitN(path, ".", 2)
		if len(segments) == 1 {
			if _, ok := out[segments[0]]; ok {
				out[segments[0]] = addressPlaceholder
			}
			continue
		}
		nested, ok := out[segments[0]].(map[string]any)
		if !ok {
			continue
		}
		if _, ok := nested[segments[1]]; ok {
			nested[segments[1]] = addressPlaceholder
		}
	}
	return out
}

func deepCopyMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		if nested, ok := v.(map[string]any); ok {
			out[k] = deepCopyMap(nested)
			continue
		}
		out[k] = v
	}
	return out
}

func configMatchesModuloAddress(spec Spec, storedConfig []byte) bool {
	var stored map[string]any
	if err := json.Unmarshal(storedConfig, &stored); err != nil {
		return false
	}
	wantJSON, err := json.Marshal(normalizeForCompare(spec.Config, spec.AddressKeys))
	if err != nil {
		return false
	}
	gotJSON, err := json.Marshal(normalizeForCompare(stored, spec.AddressKeys))
	if err != nil {
		return false
	}
	return string(wantJSON) == string(gotJSON)
}

func credentialSchemaMatches(spec Spec, storedSchema []byte) bool {
	want, err := json.Marshal(normalizeCredentialSchema(spec.CredentialSchema))
	if err != nil {
		return false
	}
	var stored []handler.McpCredentialField
	if len(storedSchema) > 0 {
		if err := json.Unmarshal(storedSchema, &stored); err != nil {
			return false
		}
	}
	got, err := json.Marshal(normalizeCredentialSchema(stored))
	if err != nil {
		return false
	}
	return string(want) == string(got)
}

func normalizeCredentialSchema(fields []handler.McpCredentialField) []handler.McpCredentialField {
	if fields == nil {
		return []handler.McpCredentialField{}
	}
	return fields
}
