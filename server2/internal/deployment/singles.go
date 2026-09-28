package deployment

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/seal"
)

// llmHealthCache — «кэш 60 секунд на процесс» (contract §13 "getLlmHealth"),
// общий для всего сервера, не привязан к пространству/пользователю.
type llmHealthCache struct {
	mu      sync.Mutex
	value   LlmHealth
	checked bool
}

func (d *Deps) handleGetLlmHealth(w http.ResponseWriter, r *http.Request) {
	if _, ok := httpapi.RequireHuman(w, r); !ok {
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, d.llmHealthStatus(r))
}

func (d *Deps) llmHealthStatus(r *http.Request) LlmHealth {
	c := d.llmHealth
	c.mu.Lock()
	if c.checked && time.Since(c.value.CheckedAt) < 60*time.Second {
		v := c.value
		c.mu.Unlock()
		return v
	}
	c.mu.Unlock()

	v := d.pingLLM(r)
	c.mu.Lock()
	c.value, c.checked = v, true
	c.mu.Unlock()
	return v
}

func (d *Deps) pingLLM(r *http.Request) LlmHealth {
	now := time.Now()
	if d.Config.DeploymentLLMBaseURL == "" || d.Config.DeploymentLLMAPIKey == "" {
		return LlmHealth{Status: "unconfigured", CheckedAt: now}
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, d.Config.DeploymentLLMBaseURL+"/models", nil)
	if err != nil {
		return LlmHealth{Status: "unreachable", CheckedAt: now}
	}
	req.Header.Set("Authorization", "Bearer "+d.Config.DeploymentLLMAPIKey)
	start := time.Now()
	resp, err := d.httpClient.Do(req)
	latency := int(time.Since(start).Milliseconds())
	if err != nil {
		return LlmHealth{Status: "unreachable", CheckedAt: now, LatencyMs: latency}
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusOK:
		return LlmHealth{Status: "ok", CheckedAt: now, LatencyMs: latency}
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return LlmHealth{Status: "auth_rejected", CheckedAt: now, LatencyMs: latency}
	default:
		return LlmHealth{Status: "degraded", CheckedAt: now, LatencyMs: latency}
	}
}

// --- /api/deployment/client-secrets -------------------------------------------

func (d *Deps) handleGetClientSecrets(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return
	}
	if r.Header.Get("X-Goosar-Launched-By") != "desktop" {
		httpapi.WriteError(w, http.StatusForbidden, "not a managed desktop installation", "client_secrets_not_managed")
		return
	}
	secrets := ClientSecrets{Integrations: map[string]ClientSecretsIntegration{}}
	if d.Config.DeploymentLLMBaseURL != "" {
		secrets.LLM = &ClientSecretsLLM{
			APIBase: d.Config.DeploymentLLMBaseURL,
			Model:   d.Config.DeploymentLLMModel,
			APIKey:  d.Config.DeploymentLLMAPIKey,
		}
	}
	fields := []string{}
	if secrets.LLM != nil {
		fields = append(fields, "llm.api_base", "llm.model", "llm.api_key")
	}
	rows, err := d.DB.Pool.Query(r.Context(), `
		SELECT pmcp_name, pmcp_transport, pmcp_config_sealed FROM platform_mcp_servers WHERE pmcp_transport = 'http'`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var name, transport string
			var sealedConfig []byte
			if err := rows.Scan(&name, &transport, &sealedConfig); err != nil {
				continue
			}
			var cfg map[string]any
			if seal.OpenJSON(d.Config.McpSecretKey, d.Config.McpSecretKeyPrevious, sealedConfig, &cfg) {
				if url, ok := cfg["url"].(string); ok && url != "" {
					secrets.Integrations[name] = ClientSecretsIntegration{URL: url}
					fields = append(fields, "integrations."+name+".url")
				}
			}
		}
	}
	audit := httpAuditAs(r, actor, "deployment.client_secrets.issued", "any-authenticated")
	audit.Reason = ptr(joinCSV(fields))
	_ = WriteAudit(r.Context(), d.DB, audit)
	httpapi.WriteJSON(w, http.StatusOK, secrets)
}

func joinCSV(items []string) string {
	b, _ := json.Marshal(items)
	return string(b)
}
