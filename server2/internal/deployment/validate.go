package deployment

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
)

// mcpNameRe — «буквы/цифры/дефис/подчёркивание» (contract §7 "Валидация
// (deployment)" и §11 "Валидация (workspace-mcp-servers)": то же правило у
// имени сервера деплоя и сервера воркспейса).
var mcpNameRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// credentialKeyRe — McpCredentialField.key: 1-128, буквы/цифры/подчёркивание,
// не начинается с цифры.
var credentialKeyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

// packageIdentRe — provisioning package_name/version (contract §9
// "Валидация (provisioning)").
var packageIdentRe = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?$`)

// secretLikeRe — «любое поле, похожее на секрет по имени» (contract §7:
// key/token/secret/password в подстроке, на любой глубине) — используется
// валидацией DeploymentPolicyBody.
var secretLikeRe = regexp.MustCompile(`(?i)key|token|secret|password`)

// maskMarker — служебный маркер маски, который config MCP-сервера не должен
// содержать (contract: «не может содержать... служебный маркер маски»,
// текст контракта не называет буквальное значение — решение этой сессии,
// см. server2/docs/decisions.md, раздел T-029: тот же маркер, что
// возвращается вызывающему как маскированное значение env-переменной).
const maskMarker = "***"

const (
	maxPolicyBytes    = 64 * 1024
	maxConfigDocBytes = 64 * 1024
	maxLLMBaseURLLen  = 2048
	maxLLMModelLen    = 256
	maxLLMAPIKeyLen   = 4096
	maxCredValueLen   = 8192
	maxCredFields     = 32
	maxCredLabelLen   = 500
)

func validMcpName(name string) bool {
	return name != "" && mcpNameRe.MatchString(name)
}

// transportSupportsEnv — контракт (§7/§11): «если задана credential_schema —
// транспорт сервера обязан поддерживать переменные окружения (http/sse
// отклоняются 400, там нет env-канала)». Схема транспортов сервера этой
// сессии — stdio/http/unknown (011_governance.up.sql,
// platform_mcp_servers_pmcp_transport_check); "sse" контракт упоминает как
// пример отклоняемого транспорта, не заводя его в перечислимый тип — решение:
// принимать его as текстовое значение config["transport"], не хранимое
// поле (текущая колонка допускает только stdio/http/unknown), но не
// проверять его отдельно здесь, так как хранимый transport этой версии
// всегда один из трёх допустимых значений (см. requestTransport).
func transportSupportsEnv(transport string) bool {
	return transport == "stdio" || transport == "unknown"
}

// requestTransport — вытаскивает "transport" из config-объекта запроса
// (contract не заводит отдельное поле transport в *McpServerRequest, только
// внутри config — то же решение уже принято декларацией колонки
// pmcp_transport/wmcp_transport по умолчанию 'unknown', см. 011_governance/
// 002_workspace). Отсутствующее/непонятное значение — 'unknown'.
func requestTransport(config map[string]any) string {
	if v, ok := config["transport"].(string); ok {
		switch v {
		case "stdio", "http", "unknown":
			return v
		}
	}
	return "unknown"
}

// validateMcpConfig — общая проверка config для серверов деплоя и воркспейса
// (contract §7/§11): без enabled/disabled на верхнем уровне, без маркера
// маски нигде в значениях, и (если задана credential_schema) транспорт
// поддерживает env.
func validateMcpConfig(config map[string]any, hasCredentialSchema bool) (msg string, ok bool) {
	if _, has := config["enabled"]; has {
		return "config не может содержать ключ enabled", false
	}
	if _, has := config["disabled"]; has {
		return "config не может содержать ключ disabled", false
	}
	if containsMaskMarker(config) {
		return "config не может содержать служебный маркер маски", false
	}
	if hasCredentialSchema && !transportSupportsEnv(requestTransport(config)) {
		return "транспорт сервера не поддерживает переменные окружения для credential_schema", false
	}
	return "", true
}

func containsMaskMarker(v any) bool {
	switch t := v.(type) {
	case string:
		return t == maskMarker
	case map[string]any:
		for _, vv := range t {
			if containsMaskMarker(vv) {
				return true
			}
		}
	case []any:
		for _, vv := range t {
			if containsMaskMarker(vv) {
				return true
			}
		}
	}
	return false
}

func validateCredentialSchema(fields []McpCredentialField) (msg string, ok bool) {
	if len(fields) > maxCredFields {
		return "credential_schema: не более 32 полей", false
	}
	seen := make(map[string]bool, len(fields))
	for _, f := range fields {
		if !credentialKeyRe.MatchString(f.Key) {
			return "credential_schema: недопустимый key", false
		}
		if seen[f.Key] {
			return "credential_schema: key должен быть уникален", false
		}
		seen[f.Key] = true
		if len(f.Label) > maxCredLabelLen || len(f.Hint) > maxCredLabelLen {
			return "credential_schema: label/hint слишком длинные", false
		}
	}
	return "", true
}

// validatePolicyBody — contract §7 "Валидация (deployment)": только объект,
// только ключи llm/mcp/session, ни одно поле (на любой глубине) не похоже на
// секрет по имени, запись "*" в mcp — только каноническая форма.
func validatePolicyBody(raw json.RawMessage) (msg string, ok bool) {
	if len(raw) > maxPolicyBytes {
		return "policy: документ превышает 64 КБ", false
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return "policy: должен быть JSON-объектом", false
	}
	for k := range m {
		if k != "llm" && k != "mcp" && k != "session" {
			return "policy: недопустимый ключ верхнего уровня " + k, false
		}
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return "policy: должен быть JSON-объектом", false
	}
	if msg, ok := checkSecretLikeKeys(generic); !ok {
		return msg, false
	}
	if mcpRaw, has := m["mcp"]; has {
		var mcp map[string]map[string]any
		if err := json.Unmarshal(mcpRaw, &mcp); err != nil {
			return "policy.mcp: должен быть объектом объектов", false
		}
		if star, has := mcp["*"]; has {
			enabled, hasEnabled := star["enabled"]
			locked, hasLocked := star["locked"]
			if len(star) != 2 || !hasEnabled || !hasLocked || enabled != false || locked != true {
				return `policy.mcp["*"]: допускается только {"enabled": false, "locked": true}`, false
			}
		}
	}
	return "", true
}

func checkSecretLikeKeys(v any) (msg string, ok bool) {
	switch t := v.(type) {
	case map[string]any:
		for k, vv := range t {
			if secretLikeRe.MatchString(k) {
				return "policy: поле похоже на секрет по имени: " + k, false
			}
			if msg, ok := checkSecretLikeKeys(vv); !ok {
				return msg, false
			}
		}
	case []any:
		for _, vv := range t {
			if msg, ok := checkSecretLikeKeys(vv); !ok {
				return msg, false
			}
		}
	}
	return "", true
}

func isAbsoluteHTTPURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil || !u.IsAbs() {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

func validPackageIdent(s string) bool {
	return s != "" && packageIdentRe.MatchString(s)
}

var validPlatforms = map[string]bool{
	"*": true, "darwin-arm64": true, "darwin-x64": true,
	"win-x64": true, "linux-x64": true, "linux-arm64": true,
}

func validPackageType(t string) bool {
	switch t {
	case "skill", "mcp-server", "runtime":
		return true
	}
	return false
}

// jsonSize — приблизительный размер документа в байтах, для лимитов
// "документ ограничен N КБ" (mcp_defaults/mcp_overrides).
func jsonSize(v any) int {
	b, err := json.Marshal(v)
	if err != nil {
		return 0
	}
	return len(b)
}

func trimNonEmpty(s string) string { return strings.TrimSpace(s) }
