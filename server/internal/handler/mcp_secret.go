// Шифрование agent.mcp_config при хранении: в это поле часто попадают
// сторонние креды (корпоративные токены Jira/Confluence, bearer-токены
// шлюзов), так что сырой JSON не должен оказываться в дампе БД в открытом
// виде. Используется тот же примитив secretbox, что и для VCS/Slack,
// со своим ключом GOOSAR_MCP_SECRET_KEY. Чтение поддерживает и запечатанные,
// и старые открытые строки, так что данные не теряются при включении ключа
// позже; без ключа сервер сохраняет прежнее открытое поведение и пишет
// предупреждение при старте.
package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	db "github.com/adanman/goosar/server/pkg/db/generated"
)

const mcpSealedKey = "__goosar_sealed__"

var errMcpKeyUnset = errors.New("sealed mcp_config present but GOOSAR_MCP_SECRET_KEY is not set")

type mcpSealedEnvelope struct {
	Sealed string `json:"__goosar_sealed__"`
}

func isSealedMcpConfig(raw []byte) bool {
	if len(raw) == 0 {
		return false
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return false
	}
	if len(obj) != 1 {
		return false
	}
	val, ok := obj[mcpSealedKey]
	if !ok {
		return false
	}
	var s string
	return json.Unmarshal(val, &s) == nil
}

func (h *Handler) sealMcpConfig(plaintext []byte) ([]byte, error) {
	if len(plaintext) == 0 {
		return plaintext, nil
	}
	if h.MCPSecretBox == nil {
		return plaintext, nil
	}
	sealed, err := h.MCPSecretBox.Seal(plaintext)
	if err != nil {
		return nil, fmt.Errorf("seal mcp_config: %w", err)
	}
	env, err := json.Marshal(mcpSealedEnvelope{Sealed: base64.StdEncoding.EncodeToString(sealed)})
	if err != nil {
		return nil, fmt.Errorf("seal mcp_config: marshal envelope: %w", err)
	}
	return env, nil
}

func (h *Handler) openMcpConfig(stored []byte) (json.RawMessage, error) {
	if len(stored) == 0 {
		return nil, nil
	}
	if !isSealedMcpConfig(stored) {
		return json.RawMessage(stored), nil
	}
	if h.MCPSecretBox == nil {
		return nil, errMcpKeyUnset
	}
	var env mcpSealedEnvelope
	if err := json.Unmarshal(stored, &env); err != nil {
		return nil, fmt.Errorf("open mcp_config: parse envelope: %w", err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(env.Sealed)
	if err != nil {
		return nil, fmt.Errorf("open mcp_config: decode envelope: %w", err)
	}
	plaintext, err := h.MCPSecretBox.Open(ciphertext)
	if err != nil {
		return nil, fmt.Errorf("open mcp_config: %w", err)
	}
	return json.RawMessage(plaintext), nil
}

func (h *Handler) prepareMcpConfigForStore(raw []byte) ([]byte, error) {
	if len(raw) == 0 {
		return raw, nil
	}
	if !json.Valid(raw) {
		return nil, errors.New("mcp_config must be valid JSON")
	}
	if h.MCPSecretBox == nil && isSealedMcpConfig(raw) {
		return nil, fmt.Errorf("mcp_config must not be an object with only the reserved %q key", mcpSealedKey)
	}
	return h.sealMcpConfig(raw)
}

func (h *Handler) warnPlaintextMcpConfigWrite(agentID string) {
	if h.MCPSecretBox != nil {
		return
	}
	slog.Warn("agent mcp_config stored in PLAINTEXT (GOOSAR_MCP_SECRET_KEY not set)", "agent_id", agentID)
}

func (h *Handler) BackfillSealedMcpConfigs(ctx context.Context) (int, error) {
	rows, err := h.Queries.ListAgentMcpConfigsForBackfill(ctx)
	if err != nil {
		return 0, fmt.Errorf("mcp_config backfill: list agents: %w", err)
	}
	sealedCount := 0
	for _, row := range rows {
		if isSealedMcpConfig(row.McpConfig) {
			continue
		}
		sealed, err := h.sealMcpConfig(row.McpConfig)
		if err != nil {
			slog.Error("mcp_config backfill: seal failed", "agent_id", uuidToString(row.ID), "error", err)
			continue
		}
		n, err := h.Queries.SealAgentMcpConfigForBackfill(ctx, db.SealAgentMcpConfigForBackfillParams{
			ID:       row.ID,
			Sealed:   sealed,
			Previous: row.McpConfig,
		})
		if err != nil {
			slog.Error("mcp_config backfill: update failed", "agent_id", uuidToString(row.ID), "error", err)
			continue
		}
		sealedCount += int(n)
	}
	if sealedCount > 0 {
		slog.Info("mcp_config backfill: sealed legacy plaintext rows", "sealed", sealedCount, "scanned", len(rows))
	}
	return sealedCount, nil
}
