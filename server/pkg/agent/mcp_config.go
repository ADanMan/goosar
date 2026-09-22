package agent

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"sort"
)

func hasManagedMcpConfig(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return false
	}
	return !bytes.Equal(trimmed, []byte("null"))
}

type mcpFlagPair struct {
	Enabled  json.RawMessage `json:"enabled"`
	Disabled json.RawMessage `json:"disabled"`
}

func mcpEntryDisabled(raw json.RawMessage) (disabled bool, malformed []string) {
	var flags mcpFlagPair
	if err := json.Unmarshal(raw, &flags); err != nil {
		return false, nil
	}

	if off, bad := mcpBoolFlag(flags.Enabled); bad {
		malformed = append(malformed, "enabled")
	} else if off != nil && !*off {
		disabled = true
	}
	if off, bad := mcpBoolFlag(flags.Disabled); bad {
		malformed = append(malformed, "disabled")
	} else if off != nil && *off {
		disabled = true
	}
	return disabled, malformed
}

func mcpBoolFlag(raw json.RawMessage) (value *bool, bad bool) {
	if !hasManagedMcpConfig(raw) {
		return nil, false
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, true
	}
	return &b, false
}

func logMalformedMcpFlags(logger *slog.Logger, name string, malformed []string) {
	if logger == nil {
		return
	}
	for _, field := range malformed {
		logger.Warn("ignoring malformed mcp_config entry flag (treating entry as enabled)",
			"name", name, "field", field)
	}
}

func filterDisabledMcpServers(raw json.RawMessage, logger *slog.Logger) json.RawMessage {
	if !hasManagedMcpConfig(raw) {
		return raw
	}

	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return raw
	}
	serversRaw, ok := top["mcpServers"]
	if !ok {
		return raw
	}
	var servers map[string]json.RawMessage
	if err := json.Unmarshal(serversRaw, &servers); err != nil {
		return raw
	}

	kept, removedAny := dropDisabledMcpEntries(servers, logger)
	if !removedAny {
		return raw
	}

	serversBytes, err := json.Marshal(kept)
	if err != nil {
		return raw
	}
	top["mcpServers"] = serversBytes
	out, err := json.Marshal(top)
	if err != nil {
		return raw
	}
	return out
}

func dropDisabledMcpEntries(servers map[string]json.RawMessage, logger *slog.Logger) (map[string]json.RawMessage, bool) {
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)

	kept := make(map[string]json.RawMessage, len(servers))
	removedAny := false
	for _, name := range names {
		entry := servers[name]
		disabled, malformed := mcpEntryDisabled(entry)
		logMalformedMcpFlags(logger, name, malformed)
		if disabled {
			if logger != nil {
				logger.Info("skipping disabled mcp_config entry", "name", name)
			}
			removedAny = true
			continue
		}
		kept[name] = entry
	}
	return kept, removedAny
}
