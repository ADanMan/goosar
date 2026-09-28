// Package agent реализует тег Agents/AgentSkills/AgentMcpServers контракта
// (`/api/agents/**`): CRUD агента, архив/восстановление, создание из шаблона
// (`agenttemplate`), avatar, права вызова (permission_mode/invocation_targets),
// runtime-привязка и модель, инструкции, навыки агента (пользовательские —
// таблица capabilities пакета skill, и встроенные в рантайм —
// disabled_runtime_skills), MCP-серверы агента (привязка общих серверов
// воркспейса), env/custom_env (с шифрованием и аудитом), метки агента и
// история его запусков.
//
// Таблицы — 003_agents.up.sql (operatives, operative_targets,
// operative_mcp_links, operative_capabilities, operative_disabled_local_skills)
// плюс общие tags/operative_tag_links (005_tasks.up.sql). Постановка/отмена
// задач агента — через server2/internal/dispatch, не напрямую.
package agent

import (
	"encoding/json"
	"time"
)

// Agent — строка operatives в форме, достаточной для сборки ответа
// components/schemas/Agent (маскирование секретов — на уровне view.go, не
// здесь: это чистые данные).
type Agent struct {
	ID                 string
	WorkspaceID        string
	ExecutorID         string
	Title              string
	Summary            string
	Instructions       string
	AvatarURI          *string
	RuntimeMode        string
	RuntimeConfigSeal  []byte
	CustomArgs         json.RawMessage
	McpConfigSeal      []byte
	McpConfigEncrypted bool
	CustomEnvSeal      []byte
	PermissionMode     string
	Status             string
	MaxConcurrentTasks int
	Model              string
	ThinkingLevel      string
	ServiceTier        string
	ComposioAllowlist  json.RawMessage // nullable jsonb array
	OwnerAccountID     *string
	Kind               string
	SystemKey          *string
	ArchivedAt         *time.Time
	ArchivedBy         *string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// InvocationTarget — components/schemas/AgentInvocationTarget.
type InvocationTarget struct {
	TargetType string  `json:"target_type"`
	TargetID   *string `json:"target_id"`
}

// SkillSummary — components/schemas/AgentSkillSummary (skill воркспейса,
// привязанный к агенту, с флагом enabled этой привязки).
type SkillSummary struct {
	ID          string          `json:"id"`
	WorkspaceID string          `json:"workspace_id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Config      json.RawMessage `json:"config"`
	Enabled     bool            `json:"enabled"`
	CreatedBy   *string         `json:"created_by"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// DisabledRuntimeSkill — components/schemas/DisabledRuntimeSkill.
type DisabledRuntimeSkill struct {
	RuntimeID string  `json:"runtime_id"`
	Provider  string  `json:"provider"`
	Root      string  `json:"root"`
	Key       string  `json:"key"`
	Name      string  `json:"name,omitempty"`
	Plugin    *string `json:"plugin,omitempty"`
}

// McpServer — components/schemas/AgentMcpServer (общий MCP-сервер
// воркспейса в контексте конкретного агента).
type McpServer struct {
	ID               string          `json:"id"`
	WorkspaceID      string          `json:"workspace_id"`
	Name             string          `json:"name"`
	Transport        string          `json:"transport"`
	Source           string          `json:"source"`
	CredentialSchema json.RawMessage `json:"credential_schema"`
	ProvidedKeys     []string        `json:"provided_keys"`
	Enabled          bool            `json:"enabled"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

func emptyArray() json.RawMessage  { return json.RawMessage(`[]`) }
func emptyObject() json.RawMessage { return json.RawMessage(`{}`) }

func strOr(s *string, def string) string {
	if s == nil {
		return def
	}
	return *s
}

func rawOr(raw json.RawMessage, def json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return def
	}
	return raw
}
