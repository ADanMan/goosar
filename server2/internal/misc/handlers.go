package misc

import (
	"net/http"
	"strings"
	"time"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// --- POST /api/feedback --------------------------------------------------------

type feedbackRequest struct {
	Message     string `json:"message"`
	URL         string `json:"url"`
	Kind        string `json:"kind"`
	WorkspaceID string `json:"workspace_id"`
}

func (d *Deps) handleCreateFeedback(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return
	}
	var req feedbackRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil || strings.TrimSpace(req.Message) == "" {
		httpapi.BadRequest(w, "message is required")
		return
	}
	if len(req.Message) > 10000 {
		httpapi.BadRequest(w, "message must be at most 10000 characters")
		return
	}
	if req.Kind == "" {
		req.Kind = "general"
	}
	if !d.FeedbackLimiter.Allow(actor.UserID) {
		httpapi.WriteError(w, http.StatusTooManyRequests, "too many feedback submissions this hour", "rate_limited")
		return
	}
	clientMeta := map[string]any{
		"client_platform": r.Header.Get("X-Client-Platform"),
		"client_version":  r.Header.Get("X-Client-Version"),
		"client_os":       r.Header.Get("X-Client-OS"),
	}
	id, createdAt, err := d.Store.CreateFeedback(r.Context(), actor.UserID, req.WorkspaceID, req.Message, req.URL, req.Kind, clientMeta)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusCreated, map[string]any{"id": id, "created_at": createdAt})
}

// --- POST /api/contact-sales ----------------------------------------------------

var freeMailDomains = map[string]bool{
	"gmail.com": true, "yahoo.com": true, "outlook.com": true, "hotmail.com": true,
	"icloud.com": true, "aol.com": true, "mail.com": true, "protonmail.com": true,
	"yandex.ru": true, "mail.ru": true, "live.com": true, "msn.com": true,
	"qq.com": true, "163.com": true, "gmx.com": true,
}

var validCompanySizes = map[string]bool{"1-10": true, "11-50": true, "51-200": true, "201-500": true, "501-1000": true, "1000+": true}
var validUseCases = map[string]bool{"evaluate": true, "adopt_team": true, "self_host": true, "integrate": true, "partner": true, "other": true}

func (d *Deps) handleContactSales(w http.ResponseWriter, r *http.Request) {
	var req ContactSalesRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	if !validContactSalesRequest(req) {
		httpapi.BadRequest(w, "invalid or missing fields")
		return
	}
	if isFreeMailDomain(req.BusinessEmail) {
		httpapi.BadRequest(w, "business_email must not be a free-mail domain")
		return
	}
	ip := httpapi.ClientIP(r)
	if !d.ContactSalesIPLimiter.Allow(ip) {
		httpapi.WriteError(w, http.StatusTooManyRequests, "IP rate limit exceeded", "rate_limited")
		return
	}
	count, err := d.Store.ContactLeadCountLastHourByEmail(r.Context(), req.BusinessEmail)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if count >= contactSalesEmailPerHour {
		httpapi.WriteError(w, http.StatusTooManyRequests, "too many recent inquiries from this email", "rate_limited")
		return
	}
	id, createdAt, err := d.Store.CreateContactLead(r.Context(), req)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusCreated, map[string]any{"id": id, "created_at": createdAt})
}

func validContactSalesRequest(req ContactSalesRequest) bool {
	if req.FirstName == "" || len(req.FirstName) > 80 || req.LastName == "" || len(req.LastName) > 80 {
		return false
	}
	if req.BusinessEmail == "" || len(req.BusinessEmail) > 254 || !strings.Contains(req.BusinessEmail, "@") {
		return false
	}
	if req.CompanyName == "" || len(req.CompanyName) > 200 {
		return false
	}
	if !validCompanySizes[req.CompanySize] {
		return false
	}
	if req.CountryRegion == "" || len(req.CountryRegion) > 80 {
		return false
	}
	if !validUseCases[req.UseCase] {
		return false
	}
	if len(req.Goals) > 2000 {
		return false
	}
	return true
}

func isFreeMailDomain(email string) bool {
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 {
		return false
	}
	return freeMailDomains[strings.ToLower(parts[1])]
}

// --- POST /api/client-usage ------------------------------------------------------

type clientUsageRequest struct {
	InstallID string `json:"install_id"`
	Runtime   *struct {
		ProbeResult     string         `json:"probe_result"`
		RuntimeCount    *int           `json:"runtime_count"`
		ProviderSummary map[string]int `json:"provider_summary"`
		OnlineCount     *int           `json:"online_count"`
		OfflineCount    *int           `json:"offline_count"`
	} `json:"runtime"`
}

func (d *Deps) handleClientUsage(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireHuman(w, r)
	if !ok {
		return
	}
	var req clientUsageRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil || req.InstallID == "" {
		httpapi.BadRequest(w, "install_id is required")
		return
	}
	// T-029 доводка: контракт (docs/50-api-contract.yaml,
	// meUpsertClientUsage) не заводит заголовок/поле для платформы вовсе —
	// только требует install_id и описывает runtime как "desktop-only;
	// rejected for web clients". Раньше здесь читался несуществующий по
	// контракту заголовок X-Client-Platform, из-за чего запрос,
	// соответствующий контракту буквально, всегда получал 400 (заголовок
	// отсутствует → "invalid client platform"). Платформа теперь выводится
	// из самого признака, который контракт и называет решающим: наличие
	// поля runtime — desktop, отсутствие — web; "web клиент с runtime"
	// поэтому больше не отдельная проверка, а просто недостижимая ветка.
	platform := "web"
	if req.Runtime != nil {
		platform = "desktop"
		if req.Runtime.ProbeResult != "success" && req.Runtime.ProbeResult != "error" {
			httpapi.BadRequest(w, "invalid runtime.probe_result")
			return
		}
		if req.Runtime.OnlineCount != nil && req.Runtime.OfflineCount != nil && req.Runtime.RuntimeCount != nil {
			if *req.Runtime.OnlineCount+*req.Runtime.OfflineCount != *req.Runtime.RuntimeCount {
				httpapi.BadRequest(w, "inconsistent runtime counts")
				return
			}
		}
	}
	in := ClientUsageInput{AccountID: actor.UserID, InstallID: req.InstallID, Platform: platform,
		ClientVersion: r.Header.Get("X-Client-Version"), ClientOS: r.Header.Get("X-Client-OS")}
	if req.Runtime != nil {
		in.RuntimeProbe = map[string]any{
			"probe_result": req.Runtime.ProbeResult, "runtime_count": req.Runtime.RuntimeCount,
			"provider_summary": req.Runtime.ProviderSummary, "online_count": req.Runtime.OnlineCount,
			"offline_count": req.Runtime.OfflineCount,
		}
	}
	if err := d.Store.UpsertClientUsage(r.Context(), in); err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- GET /api/status --------------------------------------------------------------

func (d *Deps) handleWorkspaceStatus(w http.ResponseWriter, r *http.Request) {
	member, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}
	actor, _ := httpapi.ActorFrom(r.Context())

	ws, err := d.Store.WorkspaceHeader(r.Context(), member.WorkspaceID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	runtimes, err := d.Store.RuntimeSummary(r.Context(), member.WorkspaceID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	pins, err := d.Store.ProvisioningPinCount(r.Context(), member.WorkspaceID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	mcpServers, err := d.Store.WorkspaceMcpServers(r.Context(), member.WorkspaceID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	llm, err := d.Store.WorkspaceLLMConfig(r.Context(), member.WorkspaceID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}

	callerActorType := "human"
	if actor != nil && !actor.IsHuman {
		callerActorType = "agent"
	}

	runtimeItems := make([]map[string]any, 0, len(runtimes.Items))
	for _, it := range runtimes.Items {
		item := map[string]any{"id": it.ID, "name": it.Name, "provider": it.Provider, "status": it.Status}
		// last_seen_at — contract типизирует его как non-nullable string;
		// executors.ex_last_seen_at NULL (рантайм ни разу не пинговал) —
		// решение T-029: не включать ключ вовсе, а не отдавать null.
		if it.LastSeenAt != nil {
			item["last_seen_at"] = *it.LastSeenAt
		}
		runtimeItems = append(runtimeItems, item)
	}
	mcpAssigned := make([]map[string]any, 0, len(mcpServers))
	for _, m := range mcpServers {
		mcpAssigned = append(mcpAssigned, map[string]any{"name": m.Name, "transport": m.Transport, "enabled": m.Enabled})
	}

	httpapi.WriteJSON(w, http.StatusOK, map[string]any{
		"generated_at": time.Now().UTC(),
		"workspace":    map[string]any{"id": ws.ID, "name": ws.Name, "slug": ws.Slug},
		"caller": map[string]any{
			"actor": callerActorType,
			"note":  "детали агента (agent_id/runtime_id) не резолвятся в этой версии — см. server2/docs/decisions.md, T-029",
		},
		"runtimes": map[string]any{
			"state": runtimeState(runtimes.Total), "total": runtimes.Total, "online": runtimes.Online, "items": runtimeItems,
		},
		"provisioning": map[string]any{
			"state": "ok", "pinned_packages": pins, "delivered_packages": 0,
			"note": "delivered_packages/last_delivered_at не отслеживаются в этой версии",
		},
		"mcp": map[string]any{
			"state": "ok", "workspace_servers": len(mcpServers), "tools_verified": "unknown", "assigned": mcpAssigned,
		},
		"perimeter": d.perimeterStatus(),
		"llm":       llmStatus(llm),
	})
}

func runtimeState(total int) string {
	if total == 0 {
		return "empty"
	}
	return "ok"
}

// perimeterStatus — GOOSAR_DELIVERY_PROFILE/GOOSAR_DEPLOYMENT_PROFILE
// (contract, группа "Деплой/политика") — раньше T-026 читал только первую и
// зеркалил её во второе поле; обе переменные теперь читаются по отдельности
// (см. server2/docs/env-parity.md). member_access/kerberos контракт нигде не
// расшифровывает (WorkspaceStatus.perimeter — спорное место без ссылки на
// конкретный источник) — решение T-029 в server2/docs/decisions.md:
// member_access — нет отдельного источника кроме открытого самостоятельного
// вступления пространства (не путать с открытием периметра — оставлено как
// "unknown" с пояснением), kerberos — заведомо "not_configured" (LDAP/
// Kerberos — если и настроен через GOOSAR_LDAP_*, это не то же самое, что
// Kerberos SSO, отдельной переменной под который контракт не заводит).
func (d *Deps) perimeterStatus() map[string]any {
	delivery := d.DeliveryProfile
	if delivery == "" {
		delivery = "cloud"
	}
	deployment := d.DeploymentProfile
	if deployment == "" {
		deployment = "perimeter"
	}
	return map[string]any{
		"delivery_profile":   delivery,
		"deployment_profile": deployment,
		"member_access":      "unknown",
		"kerberos":           "not_configured",
		"note":               "member_access/kerberos — пробел спецификации, см. server2/docs/decisions.md, T-029",
	}
}

func llmStatus(cfg LLMConfig) map[string]any {
	state := "not_configured"
	if cfg.Origin == "workspace" {
		state = "ok"
	}
	// base_url/model — contract §WorkspaceStatus.llm типизирует их простым
	// (не nullable) string, в отличие от большинства других опциональных
	// полей контракта, которые явно допускают null — решение T-029: "", а
	// не nil, когда LLM не настроен на пространстве.
	return map[string]any{
		"state": state, "base_url": stringOrEmpty(cfg.BaseURL), "model": stringOrEmpty(cfg.Model),
		"has_api_key": cfg.HasKey, "origin": cfg.Origin, "locked": false,
	}
}

func stringOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
