// Единственный канал, которым управляемый desktop-рантайм получает ключ LLM
// деплоя и адреса библиотеки MCP вне активной задачи (чат-помощник,
// самопроверка при установке). Доступен только управляемым инсталляциям —
// проверяется по заголовку X-Goosar-Launched-By: desktop. Персональные
// MCP-креды сюда не попадают — только конфигурация уровня деплоя.
package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/adanman/goosar/server/pkg/db/generated"
)

const adminAuditActionDeploymentClientSecretsIssued = "deployment.client_secrets.issued"

const ErrCodeClientSecretsNotManaged = "client_secrets_not_managed"

type DeploymentClientSecretsLLM struct {
	APIBase string `json:"api_base"`
	Model   string `json:"model"`
	APIKey  string `json:"api_key"`
}

type DeploymentClientSecretsIntegration struct {
	URL string `json:"url"`
}

type DeploymentClientSecretsResponse struct {
	LLM          *DeploymentClientSecretsLLM                   `json:"llm"`
	Integrations map[string]DeploymentClientSecretsIntegration `json:"integrations"`
}

var deploymentAddressKeyByName = map[string][]string{
	"jira":       {"env", "JIRA_URL"},
	"confluence": {"env", "CONFLUENCE_URL"},
	"ews":        {"env", "EWS_SERVER_URL"},
	"bitrix24":   {"env", "B24_WEBHOOK_URL"},
}

func extractDeploymentMcpAddress(name string, config map[string]any) (string, bool) {
	path, known := deploymentAddressKeyByName[name]
	if !known {
		return "", false
	}
	var cur any = config
	for _, key := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return "", false
		}
		cur, ok = m[key]
		if !ok {
			return "", false
		}
	}
	s, ok := cur.(string)
	if !ok || s == "" {
		return "", false
	}
	return s, true
}

func (h *Handler) GetDeploymentClientSecrets(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	if r.Header.Get("X-Goosar-Launched-By") != "desktop" {
		writeErrorCode(w, http.StatusForbidden, ErrCodeClientSecretsNotManaged,
			"deployment secrets are only issued to a runtime the application itself installed")
		return
	}
	userUUID, ok := parseUUIDOrBadRequest(w, userID, "user_id")
	if !ok {
		return
	}

	resp := DeploymentClientSecretsResponse{
		Integrations: map[string]DeploymentClientSecretsIntegration{},
	}
	if h.cfg.LLMAPIKey != "" || h.cfg.LLMBaseURL != "" {
		resp.LLM = &DeploymentClientSecretsLLM{
			APIBase: h.cfg.LLMBaseURL,
			Model:   h.cfg.LLMDefaultModel,
			APIKey:  h.cfg.LLMAPIKey,
		}
	}

	rows, qErr := h.Queries.ListEnabledDeploymentMcpServerConfigsForUser(r.Context(), userUUID)
	if qErr != nil {
		slog.Error("deployment client secrets: list addresses failed", "error", qErr)
		writeError(w, http.StatusInternalServerError, "failed to resolve deployment integrations")
		return
	}
	issuedFields := []string{}
	if resp.LLM != nil {
		issuedFields = append(issuedFields, "llm.api_base", "llm.model", "llm.api_key")
	}
	for _, row := range rows {
		if h.MCPSecretBox == nil {
			break
		}
		plain, openErr := OpenConfigDocumentWithBox(h.MCPSecretBox, row.Config)
		if openErr != nil {
			slog.Warn("deployment client secrets: could not open config", "name", row.Name, "error", openErr)
			continue
		}
		var doc map[string]any
		if err := json.Unmarshal(plain, &doc); err != nil {
			continue
		}
		url, ok := extractDeploymentMcpAddress(row.Name, doc)
		if !ok {
			continue
		}
		resp.Integrations[row.Name] = DeploymentClientSecretsIntegration{URL: url}
		issuedFields = append(issuedFields, "integrations."+row.Name+".url")
	}

	writeJSON(w, http.StatusOK, resp)

	sort.Strings(issuedFields)
	if _, auditErr := h.Queries.InsertAdminAudit(r.Context(), db.InsertAdminAuditParams{
		ActorUserID: userUUID,
		Action:      adminAuditActionDeploymentClientSecretsIssued,
		TargetType:  "deployment_client_secrets",
		TargetID:    pgtype.Text{String: strings.Join(issuedFields, ","), Valid: true},
		RequestID:   adminAuditRequestID(r),
	}); auditErr != nil {
		slog.Error("deployment client secrets: audit insert failed", "error", auditErr)
	}
}
