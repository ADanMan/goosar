package agent

import (
	"encoding/json"

	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/seal"
)

// Шифрование секретных JSON-полей агента (runtime_config/mcp_config/custom_env,
// колонки op_*_sealed в 003_agents.up.sql). Контракт (docs/50-api-contract.md
// §1.9, "MFA") прямо называет механизм: "GOOSAR_MCP_SECRET_KEY (тот же ключ
// шифрует и MCP-конфиги агентов, и TOTP-секреты; без него — ... сохранённые
// MCP-конфиги пишутся в БД plaintext), GOOSAR_MCP_SECRET_KEY_PREVIOUS (ротация
// ключа)". До доводки T-029 отдельный файл crypto.go реализовывал AES-256-GCM
// сам, не делясь кодом с internal/autopilot/internal/seal — три независимые
// копии одного алгоритма с чуть разными форматами. Доводка T-029 сводит все
// три к internal/seal (sealJSON/openJSON — тонкие обёртки над
// seal.SealJSONOptional/seal.OpenJSONOptional: единый формат, включая маркер
// и чтение старого/нового формата — см. internal/seal, пакет умеет открыть и
// то, что этот код писал до доводки, байт-в-байт совместимо, так что перевод
// не требует миграции уже сохранённых данных); функции перенесены сюда же
// (не отдельным файлом), потому что после сведения к обёрткам над
// internal/seal отдельный файл на пару строк не нёс архитектурной ценности.

// sealJSON сериализует v и, если key задан, шифрует результат; при пустом key
// возвращает JSON как есть (contract: "без ключа — plaintext"). encrypted
// сообщает, что фактически произошло — для op_mcp_config_encrypted и
// подобных полей ответа.
func sealJSON(key string, v any) (sealed []byte, encrypted bool, err error) {
	return seal.SealJSONOptional(key, v)
}

// openJSON — обратная операция: расшифровывает sealed (пробуя key, затем
// prevKey при ротации, contract GOOSAR_MCP_SECRET_KEY_PREVIOUS) и разбирает
// JSON в out. ok=false — значение зашифровано, но ни один из ключей не
// подошёл (mcp_config_redacted у вызывающего кода).
func openJSON(key, prevKey string, sealedVal []byte, out any) (ok bool) {
	_, ok = seal.OpenJSONOptional(key, prevKey, sealedVal, out)
	return ok
}

// viewCtx — то, что нужно, чтобы решить, какие секреты агента показывать
// вызывающему (contract §10.1-10.3).
type viewCtx struct {
	ViewerID        string
	IsHuman         bool
	Role            httpapi.Role
	AlwaysReveal    bool // ws_settings.always_reveal_agent_secrets
	ComposioEnabled bool
}

func (v viewCtx) isOwnerOrAdmin() bool {
	return httpapi.RoleAtLeast(v.Role, httpapi.RoleOwner, httpapi.RoleAdmin)
}

func (v viewCtx) isOwner(a Agent) bool {
	return v.IsHuman && a.OwnerAccountID != nil && *a.OwnerAccountID == v.ViewerID
}

// canSeeSecrets — владелец агента, либо владелец/админ воркспейса при
// включённой настройке "всегда раскрывать секреты" (contract §10.3).
func (v viewCtx) canSeeSecrets(a Agent) bool {
	return v.isOwner(a) || (v.isOwnerOrAdmin() && v.AlwaysReveal)
}

// canManage — canManageAgent (contract §10.2): владелец агента, либо
// владелец/админ воркспейса; агент-актор никогда (не человек).
func (v viewCtx) canManage(a Agent) bool {
	if !v.IsHuman {
		return false
	}
	return v.isOwner(a) || v.isOwnerOrAdmin()
}

// visibilityFromPermission — устаревшее плоское поле visibility, выводимое
// из permission_mode (contract §10.1: "устаревший плоский вид этой же модели").
func visibilityFromPermission(permissionMode string) string {
	if permissionMode == "public_to" {
		return "workspace"
	}
	return "private"
}

// maskGatewayToken маскирует runtime_config.gateway.token значением ***
// "во всех ответах" (Agent.runtime_config, contract yaml), независимо от
// прав вызывающего — отдельный, более узкий механизм, чем mcp_config/custom_env.
func maskGatewayToken(cfg map[string]any) map[string]any {
	if cfg == nil {
		return map[string]any{}
	}
	if gw, ok := cfg["gateway"].(map[string]any); ok {
		if _, hasToken := gw["token"]; hasToken {
			cloned := make(map[string]any, len(gw))
			for k, v := range gw {
				cloned[k] = v
			}
			cloned["token"] = "***"
			out := make(map[string]any, len(cfg))
			for k, v := range cfg {
				out[k] = v
			}
			out["gateway"] = cloned
			return out
		}
	}
	return cfg
}

// View — сборка components/schemas/Agent как map[string]any (не struct с
// omitempty: часть полей контракта не nullable и обязана присутствовать
// даже пустой — точный контроль через map читается прямее).
func (d *Deps) View(v viewCtx, a Agent, targets []InvocationTarget, skills []SkillSummary, disabledRuntime []DisabledRuntimeSkill) map[string]any {
	var runtimeConfig map[string]any
	openJSON(d.Store.cryptoKey, d.Store.cryptoKeyPrev, a.RuntimeConfigSeal, &runtimeConfig)
	runtimeConfig = maskGatewayToken(runtimeConfig)

	mcpConfig, mcpRedacted := d.viewMcpConfig(v, a)

	composioAllowlist, composioRedacted := d.viewComposio(v, a)

	hasCustomEnv := len(a.CustomEnvSeal) > 0
	keys, _ := d.Store.CustomEnvKeys(a)
	if targets == nil {
		targets = []InvocationTarget{}
	}
	if skills == nil {
		skills = []SkillSummary{}
	}
	if disabledRuntime == nil {
		disabledRuntime = []DisabledRuntimeSkill{}
	}

	out := map[string]any{
		"id":                                  a.ID,
		"workspace_id":                        a.WorkspaceID,
		"runtime_id":                          a.ExecutorID,
		"name":                                a.Title,
		"description":                         a.Summary,
		"instructions":                        a.Instructions,
		"avatar_url":                          a.AvatarURI,
		"runtime_mode":                        a.RuntimeMode,
		"runtime_config":                      runtimeConfig,
		"custom_args":                         rawOrArray(a.CustomArgs),
		"mcp_config":                          mcpConfig,
		"has_custom_env":                      hasCustomEnv,
		"custom_env_key_count":                len(keys),
		"mcp_config_redacted":                 mcpRedacted,
		"mcp_config_encrypted":                a.McpConfigEncrypted,
		"visibility":                          visibilityFromPermission(a.PermissionMode),
		"permission_mode":                     a.PermissionMode,
		"invocation_targets":                  targets,
		"status":                              a.Status,
		"max_concurrent_tasks":                a.MaxConcurrentTasks,
		"model":                               a.Model,
		"thinking_level":                      a.ThinkingLevel,
		"service_tier":                        a.ServiceTier,
		"composio_toolkit_allowlist":          composioAllowlist,
		"composio_toolkit_allowlist_redacted": composioRedacted,
		"owner_id":                            a.OwnerAccountID,
		"skills":                              skills,
		"disabled_runtime_skills":             disabledRuntime,
		"created_at":                          a.CreatedAt,
		"updated_at":                          a.UpdatedAt,
		"archived_at":                         a.ArchivedAt,
		"archived_by":                         a.ArchivedBy,
		"system_key":                          strOr(a.SystemKey, ""),
	}
	return out
}

func (d *Deps) viewMcpConfig(v viewCtx, a Agent) (mcpConfig any, redacted bool) {
	if len(a.McpConfigSeal) == 0 {
		return nil, false
	}
	if !v.canSeeSecrets(a) {
		return nil, true
	}
	var cfg map[string]any
	if !openJSON(d.Store.cryptoKey, d.Store.cryptoKeyPrev, a.McpConfigSeal, &cfg) {
		return nil, true
	}
	return cfg, false
}

func (d *Deps) viewComposio(v viewCtx, a Agent) (allowlist []string, redacted bool) {
	if len(a.ComposioAllowlist) == 0 {
		return []string{}, false
	}
	if !v.ComposioEnabled {
		return []string{}, true
	}
	var list []string
	_ = json.Unmarshal(a.ComposioAllowlist, &list)
	if list == nil {
		list = []string{}
	}
	if v.canSeeSecrets(a) {
		return list, false
	}
	return []string{}, true
}

func rawOrArray(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return []string{}
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil || out == nil {
		return []string{}
	}
	return out
}
