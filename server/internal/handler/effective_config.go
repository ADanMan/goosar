// Резолвер итоговой конфигурации: собирает конфиг рабочей станции из трёх
// слоёв — политика деплоя, конфиг пространства, персональный override
// пользователя, — каждый следующий слой перекрывает предыдущий, кроме
// признака locked, который может выставить только политика. Секреты (ключ
// LLM, MCP-документы) хранятся запечатанными и расшифровываются здесь для
// доставки демону; в логи попадают только счётчики, не значения.
// Отсутствие всех слоёв даёт пустой конфиг, а не ошибку.
package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/util/secretbox"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

const EffectiveConfigSchemaVersion = 1

const (
	ConfigOriginPolicy       = "policy"
	ConfigOriginWorkspace    = "workspace"
	ConfigOriginUserOverride = "user_override"
	ConfigOriginMachine      = "machine"
)

type EffectiveConfig struct {
	SchemaVersion int `json:"schema_version"`

	LLM *EffectiveLLM `json:"llm,omitempty"`

	MCP map[string]EffectiveMCPServer `json:"mcp,omitempty"`

	RevokedPackages []string `json:"revoked_packages"`
}

type EffectiveLLM struct {
	BaseURL string `json:"base_url,omitempty"`
	Model   string `json:"model,omitempty"`
	APIKey  string `json:"api_key,omitempty"`
	Origin  string `json:"origin"`
	Locked  bool   `json:"locked"`
}

type EffectiveMCPServer struct {
	Enabled bool              `json:"enabled"`
	Env     map[string]string `json:"env,omitempty"`
	Origin  string            `json:"origin"`
	Locked  bool              `json:"locked,omitempty"`
}

type configMCPLayerEntry struct {
	Enabled *bool             `json:"enabled,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Locked  bool              `json:"locked,omitempty"`
}

func (e configMCPLayerEntry) enabled() bool {
	if e.Enabled == nil {
		return true
	}
	return *e.Enabled
}

type deploymentPolicyLLM struct {
	BaseURL string `json:"base_url,omitempty"`
	Model   string `json:"model,omitempty"`
	Locked  bool   `json:"locked,omitempty"`
}

type deploymentPolicyDoc struct {
	LLM *deploymentPolicyLLM           `json:"llm,omitempty"`
	MCP map[string]configMCPLayerEntry `json:"mcp,omitempty"`
}

var errConfigSecretKeyUnset = errors.New("config secrets require GOOSAR_MCP_SECRET_KEY to be set")

var errConfigSealedKeyUnset = errors.New("sealed config value present but GOOSAR_MCP_SECRET_KEY is not set")

func (h *Handler) sealConfigSecret(plaintext string) ([]byte, error) {
	if plaintext == "" {
		return nil, nil
	}
	if h.MCPSecretBox == nil {
		return nil, errConfigSecretKeyUnset
	}
	sealed, err := h.MCPSecretBox.Seal([]byte(plaintext))
	if err != nil {
		return nil, fmt.Errorf("seal config secret: %w", err)
	}
	return sealed, nil
}

func (h *Handler) openConfigSecret(sealed []byte) (string, error) {
	if len(sealed) == 0 {
		return "", nil
	}
	if h.MCPSecretBox == nil {
		return "", errConfigSealedKeyUnset
	}
	plaintext, err := h.MCPSecretBox.Open(sealed)
	if err != nil {
		return "", fmt.Errorf("open config secret: %w", err)
	}
	return string(plaintext), nil
}

func (h *Handler) sealConfigDocument(doc []byte) ([]byte, error) {
	return SealConfigDocumentWithBox(h.MCPSecretBox, doc)
}

func SealConfigDocumentWithBox(box *secretbox.Box, doc []byte) ([]byte, error) {
	if len(doc) == 0 {
		return nil, nil
	}
	if box == nil {
		return nil, errConfigSecretKeyUnset
	}
	sealed, err := box.Seal(doc)
	if err != nil {
		return nil, fmt.Errorf("seal config document: %w", err)
	}
	env, err := json.Marshal(mcpSealedEnvelope{Sealed: base64.StdEncoding.EncodeToString(sealed)})
	if err != nil {
		return nil, fmt.Errorf("seal config document: marshal envelope: %w", err)
	}
	return env, nil
}

func (h *Handler) openConfigDocument(stored []byte) ([]byte, error) {
	return OpenConfigDocumentWithBox(h.MCPSecretBox, stored)
}

func OpenConfigDocumentWithBox(box *secretbox.Box, stored []byte) ([]byte, error) {
	if len(stored) == 0 {
		return nil, nil
	}
	if !isSealedMcpConfig(stored) {
		return stored, nil
	}
	if box == nil {
		return nil, errConfigSealedKeyUnset
	}
	var env mcpSealedEnvelope
	if err := json.Unmarshal(stored, &env); err != nil {
		return nil, fmt.Errorf("open config document: parse envelope: %w", err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(env.Sealed)
	if err != nil {
		return nil, fmt.Errorf("open config document: decode envelope: %w", err)
	}
	plaintext, err := box.Open(ciphertext)
	if err != nil {
		return nil, fmt.Errorf("open config document: %w", err)
	}
	return plaintext, nil
}

type llmLayerValues struct {
	baseURL string
	model   string
	apiKey  string
}

func (v llmLayerValues) empty() bool {
	return v.baseURL == "" && v.model == "" && v.apiKey == ""
}

type llmFieldPins struct {
	baseURL bool
	model   bool
}

func applyLLMLayer(current *EffectiveLLM, vals llmLayerValues, origin string, pins llmFieldPins) *EffectiveLLM {
	if pins.baseURL {
		vals.baseURL = ""
	}
	if pins.model {
		vals.model = ""
	}
	if vals.empty() {
		return current
	}
	next := &EffectiveLLM{Origin: origin}
	if current != nil {
		next.BaseURL = current.BaseURL
		next.Model = current.Model
		next.APIKey = current.APIKey
		next.Locked = current.Locked
	}
	if vals.baseURL != "" {
		next.BaseURL = vals.baseURL
	}
	if vals.model != "" {
		next.Model = vals.model
	}
	if vals.apiKey != "" {
		next.APIKey = vals.apiKey
	}
	return next
}

const mcpPolicyWildcard = "*"

func applyMCPLayer(current map[string]EffectiveMCPServer, entries map[string]configMCPLayerEntry, origin string, allowLocks bool, wildcardLocked bool) map[string]EffectiveMCPServer {
	if len(entries) == 0 {
		return current
	}
	next := make(map[string]EffectiveMCPServer, len(current)+len(entries))
	for name, entry := range current {
		next[name] = entry
	}
	for name, entry := range entries {
		if name == mcpPolicyWildcard {
			continue
		}
		if wildcardLocked {
			next[name] = EffectiveMCPServer{
				Enabled: false,
				Origin:  ConfigOriginPolicy,
				Locked:  true,
			}
			continue
		}
		if existing, ok := next[name]; ok && existing.Locked {
			continue
		}
		resolved := EffectiveMCPServer{
			Enabled: entry.enabled(),
			Env:     entry.Env,
			Origin:  origin,
		}
		if allowLocks {
			resolved.Locked = entry.Locked
		}
		next[name] = resolved
	}
	return next
}

func parseMCPLayerDoc(raw []byte) (map[string]configMCPLayerEntry, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var entries map[string]configMCPLayerEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("parse mcp layer document: %w", err)
	}
	return entries, nil
}

func (h *Handler) ResolveEffectiveConfig(ctx context.Context, workspaceID, userID pgtype.UUID) (*EffectiveConfig, error) {
	var llm *EffectiveLLM
	var llmPins llmFieldPins
	mcp := map[string]EffectiveMCPServer{}
	mcpWildcardLocked := false

	policyRow, err := h.Queries.GetDeploymentPolicy(ctx)
	switch {
	case err == nil:
		var doc deploymentPolicyDoc
		if len(policyRow.Policy) > 0 {
			if err := json.Unmarshal(policyRow.Policy, &doc); err != nil {
				return nil, fmt.Errorf("effective config: parse deployment policy: %w", err)
			}
		}
		if doc.LLM != nil {
			llm = &EffectiveLLM{
				BaseURL: doc.LLM.BaseURL,
				Model:   doc.LLM.Model,
				Origin:  ConfigOriginPolicy,
				Locked:  doc.LLM.Locked,
			}
			if doc.LLM.Locked {

				llmPins = llmFieldPins{
					baseURL: doc.LLM.BaseURL != "",
					model:   doc.LLM.Model != "",
				}
			}
		}

		_, mcpWildcardLocked = doc.MCP[mcpPolicyWildcard]
		mcp = applyMCPLayer(mcp, doc.MCP, ConfigOriginPolicy, true, mcpWildcardLocked)
	case errors.Is(err, pgx.ErrNoRows):

	default:
		return nil, fmt.Errorf("effective config: load deployment policy: %w", err)
	}

	wc, err := h.Queries.GetWorkspaceConfig(ctx, workspaceID)
	switch {
	case err == nil:
		apiKey, err := h.openConfigSecret(wc.LlmApiKey)
		if err != nil {
			return nil, fmt.Errorf("effective config: workspace llm api key: %w", err)
		}
		llm = applyLLMLayer(llm, llmLayerValues{
			baseURL: wc.LlmBaseUrl.String,
			model:   wc.LlmModel.String,
			apiKey:  apiKey,
		}, ConfigOriginWorkspace, llmPins)

		mcpDoc, err := h.openConfigDocument(wc.McpDefaults)
		if err != nil {
			return nil, fmt.Errorf("effective config: workspace mcp defaults: %w", err)
		}
		entries, err := parseMCPLayerDoc(mcpDoc)
		if err != nil {
			return nil, fmt.Errorf("effective config: workspace mcp defaults: %w", err)
		}
		mcp = applyMCPLayer(mcp, entries, ConfigOriginWorkspace, false, mcpWildcardLocked)
	case errors.Is(err, pgx.ErrNoRows):

	default:
		return nil, fmt.Errorf("effective config: load workspace config: %w", err)
	}

	uo, err := h.Queries.GetUserConfigOverride(ctx, db.GetUserConfigOverrideParams{
		WorkspaceID: workspaceID,
		UserID:      userID,
	})
	switch {
	case err == nil:
		apiKey, err := h.openConfigSecret(uo.LlmApiKey)
		if err != nil {
			return nil, fmt.Errorf("effective config: override llm api key: %w", err)
		}
		llm = applyLLMLayer(llm, llmLayerValues{
			baseURL: uo.LlmBaseUrl.String,
			model:   uo.LlmModel.String,
			apiKey:  apiKey,
		}, ConfigOriginUserOverride, llmPins)

		mcpDoc, err := h.openConfigDocument(uo.McpOverrides)
		if err != nil {
			return nil, fmt.Errorf("effective config: override mcp document: %w", err)
		}
		entries, err := parseMCPLayerDoc(mcpDoc)
		if err != nil {
			return nil, fmt.Errorf("effective config: override mcp document: %w", err)
		}
		mcp = applyMCPLayer(mcp, entries, ConfigOriginUserOverride, false, mcpWildcardLocked)
	case errors.Is(err, pgx.ErrNoRows):

	default:
		return nil, fmt.Errorf("effective config: load user override: %w", err)
	}

	eff := &EffectiveConfig{
		SchemaVersion: EffectiveConfigSchemaVersion,

		RevokedPackages: h.revokedPackagesForEffectiveConfig(ctx, workspaceID),
	}
	if llm != nil {
		eff.LLM = llm
	}
	if len(mcp) > 0 {
		eff.MCP = mcp
	}
	return eff, nil
}
