package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

const (
	mcpCredentialFieldsLimit = 32
	mcpCredentialKeyMaxLen   = 128
	mcpCredentialTextMaxLen  = 500
	mcpCredentialValueMaxLen = 8192
)

type McpCredentialField struct {
	Key string `json:"key"`

	Label string `json:"label,omitempty"`

	Hint string `json:"hint,omitempty"`

	Required bool `json:"required"`
}

func parseMcpCredentialSchema(stored []byte) []McpCredentialField {
	if len(stored) == 0 {
		return nil
	}
	var fields []McpCredentialField
	if err := json.Unmarshal(stored, &fields); err != nil {
		return nil
	}
	return fields
}

func validateMcpCredentialSchema(fields []McpCredentialField) error {
	if len(fields) > mcpCredentialFieldsLimit {
		return fmt.Errorf("credential_schema may declare at most %d fields", mcpCredentialFieldsLimit)
	}
	seen := make(map[string]struct{}, len(fields))
	for i := range fields {
		key := strings.TrimSpace(fields[i].Key)
		if err := validateMcpCredentialKey(key); err != nil {
			return err
		}
		if _, dup := seen[key]; dup {
			return fmt.Errorf("credential_schema declares %q twice", key)
		}
		seen[key] = struct{}{}
		if len(fields[i].Label) > mcpCredentialTextMaxLen || len(fields[i].Hint) > mcpCredentialTextMaxLen {
			return fmt.Errorf("credential_schema field %q: label and hint must be at most %d characters", key, mcpCredentialTextMaxLen)
		}
	}
	return nil
}

func validateMcpCredentialKey(key string) error {
	if key == "" {
		return errors.New("credential field key is required")
	}
	if len(key) > mcpCredentialKeyMaxLen {
		return fmt.Errorf("credential field key must be at most %d characters", mcpCredentialKeyMaxLen)
	}
	if key[0] >= '0' && key[0] <= '9' {
		return fmt.Errorf("credential field key %q must not start with a digit", key)
	}
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
		default:
			return fmt.Errorf("credential field key %q may only contain letters, digits, and underscores", key)
		}
	}
	return nil
}

func normalizeMcpCredentialSchema(fields []McpCredentialField) []McpCredentialField {
	out := make([]McpCredentialField, 0, len(fields))
	for _, field := range fields {
		out = append(out, McpCredentialField{
			Key:      strings.TrimSpace(field.Key),
			Label:    strings.TrimSpace(field.Label),
			Hint:     strings.TrimSpace(field.Hint),
			Required: field.Required,
		})
	}
	return out
}

func mcpCredentialTransportSupported(transport string) bool {
	switch transport {
	case "http", "sse":
		return false
	}
	return true
}

func validateMcpCredentialValues(schema []McpCredentialField, values map[string]string) error {
	if len(values) > mcpCredentialFieldsLimit {
		return fmt.Errorf("at most %d credential values may be supplied", mcpCredentialFieldsLimit)
	}
	declared := make(map[string]struct{}, len(schema))
	for _, field := range schema {
		declared[field.Key] = struct{}{}
	}
	for key, value := range values {
		if _, ok := declared[key]; !ok {
			return fmt.Errorf("%q is not a credential field of this MCP server", key)
		}
		if len(value) > mcpCredentialValueMaxLen {
			return fmt.Errorf("the value for %q is too long", key)
		}
	}
	return nil
}

func mergeMcpCredentialValues(schema []McpCredentialField, existing, incoming map[string]string) map[string]string {
	declared := make(map[string]struct{}, len(schema))
	for _, field := range schema {
		declared[field.Key] = struct{}{}
	}
	merged := make(map[string]string, len(existing)+len(incoming))
	for key, value := range existing {
		if _, ok := declared[key]; !ok || strings.TrimSpace(value) == "" {
			continue
		}
		merged[key] = value
	}
	for key, value := range incoming {
		if _, ok := declared[key]; !ok {
			continue
		}
		if strings.TrimSpace(value) == "" {
			delete(merged, key)
			continue
		}
		merged[key] = value
	}
	return merged
}

func mcpCredentialKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func missingMcpCredentials(schema []McpCredentialField, provided []string) []string {
	if len(schema) == 0 {
		return nil
	}
	have := make(map[string]struct{}, len(provided))
	for _, key := range provided {
		have[key] = struct{}{}
	}
	missing := make([]string, 0)
	for _, field := range schema {
		if !field.Required {
			continue
		}
		if _, ok := have[field.Key]; !ok {
			missing = append(missing, field.Key)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return missing
}

func applyMcpCredentialValues(entry json.RawMessage, schema []McpCredentialField, values map[string]string) (json.RawMessage, error) {
	if len(schema) == 0 || len(values) == 0 {
		return entry, nil
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(entry, &doc); err != nil {

		return nil, errors.New("mcp server entry is not a JSON object")
	}
	env := map[string]string{}
	if raw, ok := doc["env"]; ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &env); err != nil {
			return nil, errors.New("mcp server entry has a non-string env map")
		}
	}
	applied := 0
	for _, field := range schema {
		value, ok := values[field.Key]
		if !ok || strings.TrimSpace(value) == "" {
			continue
		}
		env[field.Key] = value
		applied++
	}
	if applied == 0 {
		return entry, nil
	}
	envBytes, err := json.Marshal(env)
	if err != nil {
		return nil, errors.New("failed to encode mcp server env")
	}
	doc["env"] = envBytes
	out, err := json.Marshal(doc)
	if err != nil {
		return nil, errors.New("failed to encode mcp server entry")
	}
	return out, nil
}

func (h *Handler) layerOwnerMcpCredentials(credentialSchema, sealedValues, entry []byte) (json.RawMessage, []string, error) {
	schema := parseMcpCredentialSchema(credentialSchema)
	if len(schema) == 0 {
		return entry, nil, nil
	}
	values := map[string]string{}
	if len(sealedValues) > 0 {
		opened, err := h.openConfigDocument(sealedValues)
		if err != nil {
			return nil, nil, errors.New("open mcp user credentials")
		}
		if err := json.Unmarshal(opened, &values); err != nil {
			return nil, nil, errors.New("mcp user credentials are not a string map")
		}
	}
	if missing := missingMcpCredentials(schema, mcpCredentialKeys(values)); len(missing) > 0 {
		return nil, missing, nil
	}
	layered, err := applyMcpCredentialValues(entry, schema, values)
	if err != nil {
		return nil, nil, err
	}
	return layered, nil, nil
}
