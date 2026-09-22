package handler

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

const (
	statusOK            = "ok"
	statusDegraded      = "degraded"
	statusNone          = "none"
	statusUnknown       = "unknown"
	statusPending       = "pending"
	statusConfigured    = "configured"
	statusNotConfigured = "not_configured"
)

type GoosarStatusResponse struct {
	GeneratedAt  string                   `json:"generated_at"`
	Workspace    GoosarStatusWorkspace    `json:"workspace"`
	Caller       GoosarStatusCaller       `json:"caller"`
	Runtimes     GoosarStatusRuntimes     `json:"runtimes"`
	Provisioning GoosarStatusProvisioning `json:"provisioning"`
	MCP          GoosarStatusMCP          `json:"mcp"`
	Perimeter    GoosarStatusPerimeter    `json:"perimeter"`
	LLM          GoosarStatusLLM          `json:"llm"`
}

type GoosarStatusWorkspace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type GoosarStatusCaller struct {
	Actor         string `json:"actor"`
	AgentID       string `json:"agent_id,omitempty"`
	AgentName     string `json:"agent_name,omitempty"`
	RuntimeID     string `json:"runtime_id,omitempty"`
	RuntimeStatus string `json:"runtime_status,omitempty"`
	Note          string `json:"note,omitempty"`
}

type GoosarStatusRuntimes struct {
	State  string                    `json:"state"`
	Total  int                       `json:"total"`
	Online int                       `json:"online"`
	Items  []GoosarStatusRuntimeItem `json:"items"`
	Note   string                    `json:"note,omitempty"`
}

type GoosarStatusRuntimeItem struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Provider   string `json:"provider"`
	Status     string `json:"status"`
	LastSeenAt string `json:"last_seen_at,omitempty"`
}

type GoosarStatusProvisioning struct {
	State          string `json:"state"`
	PinnedPackages int    `json:"pinned_packages"`

	DeliveredPacks  int    `json:"delivered_packages"`
	LastDeliveredAt string `json:"last_delivered_at,omitempty"`
	Note            string `json:"note,omitempty"`
}

type GoosarStatusMCP struct {
	State            string `json:"state"`
	WorkspaceServers int    `json:"workspace_servers"`

	ToolsVerified string                  `json:"tools_verified"`
	Assigned      []GoosarStatusMCPServer `json:"assigned"`
	Note          string                  `json:"note,omitempty"`
}

type GoosarStatusMCPServer struct {
	Name      string `json:"name"`
	Transport string `json:"transport"`
	Enabled   bool   `json:"enabled"`
}

type GoosarStatusPerimeter struct {
	DeliveryProfile string `json:"delivery_profile"`

	DeploymentProfile string `json:"deployment_profile"`
	MemberAccess      string `json:"member_access"`
	Kerberos          string `json:"kerberos"`
	Note              string `json:"note"`
}

type GoosarStatusLLM struct {
	State     string `json:"state"`
	BaseURL   string `json:"base_url,omitempty"`
	Model     string `json:"model,omitempty"`
	HasAPIKey bool   `json:"has_api_key"`
	Origin    string `json:"origin,omitempty"`
	Locked    bool   `json:"locked"`
	Note      string `json:"note,omitempty"`
}

func (h *Handler) GetGoosarStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	resp := GoosarStatusResponse{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Workspace:   GoosarStatusWorkspace{ID: workspaceID},
	}
	if ws, err := h.Queries.GetWorkspace(ctx, wsUUID); err == nil {
		resp.Workspace.Name = ws.Name
		resp.Workspace.Slug = ws.Slug
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	resp.Caller = h.goosarStatusCaller(ctx, actorType, actorID)
	resp.Runtimes = h.goosarStatusRuntimes(ctx, wsUUID)
	resp.Provisioning = h.goosarStatusProvisioning(ctx, wsUUID)
	resp.MCP = h.goosarStatusMCP(ctx, wsUUID, userID, actorType, resp.Caller.AgentID)
	resp.Perimeter = h.goosarStatusPerimeter(ctx, userID, workspaceID)
	resp.LLM = h.goosarStatusLLM(ctx, wsUUID, userID)

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) goosarStatusCaller(ctx context.Context, actorType, actorID string) GoosarStatusCaller {
	caller := GoosarStatusCaller{Actor: actorType}
	if actorType != "agent" || actorID == "" {
		return caller
	}
	caller.AgentID = actorID
	agentUUID, err := util.ParseUUID(actorID)
	if err != nil {
		caller.Note = "the request carried an agent id the server could not parse"
		return caller
	}
	agent, err := h.Queries.GetAgent(ctx, agentUUID)
	if err != nil {
		caller.Note = "the calling agent could not be loaded; its binding is unknown"
		return caller
	}
	caller.AgentName = agent.Name
	if !agent.RuntimeID.Valid {
		caller.Note = "this agent is bound to no runtime, so nothing can execute for it"
		return caller
	}
	caller.RuntimeID = uuidToString(agent.RuntimeID)
	rt, err := h.Queries.GetAgentRuntime(ctx, agent.RuntimeID)
	if err != nil {
		caller.RuntimeStatus = statusUnknown
		caller.Note = "the bound runtime row could not be read"
		return caller
	}
	caller.RuntimeStatus = rt.Status
	return caller
}

func (h *Handler) goosarStatusRuntimes(ctx context.Context, wsUUID pgtype.UUID) GoosarStatusRuntimes {
	runtimes, err := h.Queries.ListAgentRuntimes(ctx, wsUUID)
	if err != nil {
		slog.Warn("goosar status: list runtimes failed", "error", err)
		return GoosarStatusRuntimes{
			State: statusUnknown, Items: []GoosarStatusRuntimeItem{},
			Note: "the runtime list could not be read, so reachability is unknown",
		}
	}

	out := GoosarStatusRuntimes{Items: make([]GoosarStatusRuntimeItem, 0, len(runtimes)), Total: len(runtimes)}
	for _, rt := range runtimes {
		if rt.Status == "online" {
			out.Online++
		}
		item := GoosarStatusRuntimeItem{
			ID:       uuidToString(rt.ID),
			Name:     rt.Name,
			Provider: rt.Provider,
			Status:   rt.Status,
		}
		if ts := timestampToPtr(rt.LastSeenAt); ts != nil {
			item.LastSeenAt = *ts
		}
		out.Items = append(out.Items, item)
	}

	switch {
	case out.Total == 0:
		out.State = statusNone
		out.Note = "no runtime is registered in this workspace; nothing can execute here yet"
	case out.Online == 0:
		out.State = statusDegraded
		out.Note = "every registered runtime is offline by its last heartbeat"
	default:
		out.State = statusOK
	}
	return out
}

func (h *Handler) goosarStatusProvisioning(ctx context.Context, wsUUID pgtype.UUID) GoosarStatusProvisioning {
	if h.ProvisioningStore == nil {
		return GoosarStatusProvisioning{
			State: statusNotConfigured,
			Note:  "this deployment serves no provisioning packages (GOOSAR_PROVISIONING_STORE unset or misconfigured)",
		}
	}
	pins, err := h.Queries.ListEnabledProvisioningPins(ctx, wsUUID)
	if err != nil {
		slog.Warn("goosar status: list provisioning pins failed", "error", err)
		return GoosarStatusProvisioning{State: statusUnknown, Note: "the provisioning lockfile could not be read"}
	}
	delivered, err := h.Queries.ListDeliveredProvisioningPackages(ctx, wsUUID)
	if err != nil {
		slog.Warn("goosar status: list delivered packages failed", "error", err)
		return GoosarStatusProvisioning{
			State: statusUnknown, PinnedPackages: len(pins),
			Note: "the delivery record could not be read",
		}
	}

	type packageKey struct{ pkgType, name string }
	pinned := make(map[packageKey]struct{}, len(pins))
	for _, p := range pins {
		pinned[packageKey{p.PackageType, p.PackageName}] = struct{}{}
	}

	out := GoosarStatusProvisioning{PinnedPackages: len(pins)}
	var last time.Time
	for _, d := range delivered {
		if _, ok := pinned[packageKey{d.PackageType, d.PackageName}]; !ok {
			continue
		}
		out.DeliveredPacks++
		if d.LastDeliveredAt.Valid && d.LastDeliveredAt.Time.After(last) {
			last = d.LastDeliveredAt.Time
		}
	}
	if !last.IsZero() {
		out.LastDeliveredAt = last.UTC().Format(time.RFC3339)
	}

	switch {
	case out.PinnedPackages == 0:
		out.State = statusNone
		out.Note = "the workspace pins no packages, so nothing is provisioned to machines"
	case out.DeliveredPacks == 0:
		out.State = statusPending
		out.Note = "packages are pinned but no machine has fetched the current manifest yet"
	default:
		out.State = statusOK
		out.Note = "at least one machine has been served the pinned set; " +
			"this is a delivery record, not a check that the packages still work"
	}
	return out
}

func (h *Handler) goosarStatusMCP(ctx context.Context, wsUUID pgtype.UUID, userID, actorType, agentID string) GoosarStatusMCP {

	callerUUID, _ := util.ParseUUID(userID)
	out := GoosarStatusMCP{
		ToolsVerified: statusUnknown,
		Assigned:      []GoosarStatusMCPServer{},
		Note: "declared servers only: nothing records whether a server actually served tools " +
			"at the last runtime start, so tool availability is unknown, not ok and not broken",
	}

	if actorType != "agent" {
		servers, err := h.Queries.ListWorkspaceMcpServers(ctx, db.ListWorkspaceMcpServersParams{
			WorkspaceID: wsUUID,
			UserID:      callerUUID,
		})
		if err != nil {
			slog.Warn("goosar status: list workspace mcp servers failed", "error", err)
			out.State = statusUnknown
			out.Note = "the MCP server library could not be read"
			return out
		}
		out.WorkspaceServers = len(servers)
	}

	if agentID != "" {
		if agentUUID, parseErr := util.ParseUUID(agentID); parseErr == nil {

			assigned, listErr := h.Queries.ListAgentMcpServers(ctx, db.ListAgentMcpServersParams{
				AgentID:     agentUUID,
				UserID:      callerUUID,
				WorkspaceID: wsUUID,
			})
			if listErr != nil {
				slog.Warn("goosar status: list agent mcp servers failed", "error", listErr)
			}
			for _, s := range assigned {
				out.Assigned = append(out.Assigned, GoosarStatusMCPServer{
					Name:      s.Name,
					Transport: s.Transport,
					Enabled:   s.Enabled,
				})
			}
		}
	}

	if out.WorkspaceServers == 0 && len(out.Assigned) == 0 {
		out.State = statusNone
		if actorType == "agent" {

			out.Note = "no MCP server is assigned to you; whether the workspace declares any is not visible to an agent actor"
		} else {
			out.Note = "no MCP server is declared in this workspace"
		}
		return out
	}
	out.State = statusUnknown
	return out
}

func (h *Handler) goosarStatusPerimeter(ctx context.Context, userID, workspaceID string) GoosarStatusPerimeter {
	out := GoosarStatusPerimeter{
		DeliveryProfile:   string(h.cfg.DeliveryProfile),
		DeploymentProfile: string(h.cfg.DeploymentProfile),
		MemberAccess:      statusUnknown,
		Kerberos:          statusUnknown,
		Note: "Kerberos and other machine-local credentials are never visible to the server; " +
			"a live ticket cache still does not prove the target server will accept it",
	}
	if out.DeliveryProfile == "" {
		out.DeliveryProfile = statusUnknown
	}
	if out.DeploymentProfile == "" {
		out.DeploymentProfile = statusUnknown
	}
	member, err := h.getWorkspaceMember(ctx, userID, workspaceID)
	if err != nil {
		return out
	}
	if member.PerimeterAccess {
		out.MemberAccess = "granted"
	} else {
		out.MemberAccess = "not_granted"
	}
	return out
}

func (h *Handler) goosarStatusLLM(ctx context.Context, wsUUID pgtype.UUID, userID string) GoosarStatusLLM {
	userUUID, err := util.ParseUUID(userID)
	if err != nil {
		return GoosarStatusLLM{State: statusUnknown, Note: "the caller's user id could not be parsed"}
	}
	eff, err := h.ResolveEffectiveConfig(ctx, wsUUID, userUUID)
	if err != nil {
		slog.Warn("goosar status: resolve effective config failed", "error", err)
		return GoosarStatusLLM{State: statusUnknown, Note: "the effective configuration could not be resolved"}
	}
	if eff.LLM == nil || (eff.LLM.BaseURL == "" && eff.LLM.APIKey == "") {
		return GoosarStatusLLM{State: statusNotConfigured, Note: "no configuration layer sets an LLM endpoint"}
	}
	return GoosarStatusLLM{
		State:     statusConfigured,
		BaseURL:   eff.LLM.BaseURL,
		Model:     eff.LLM.Model,
		HasAPIKey: eff.LLM.APIKey != "",
		Origin:    eff.LLM.Origin,
		Locked:    eff.LLM.Locked,
		Note:      "configured, not verified: no last-successful-call signal is recorded",
	}
}
