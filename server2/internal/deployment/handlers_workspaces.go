package deployment

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/realtime"
)

func (d *Deps) handleListDeploymentWorkspaces(w http.ResponseWriter, r *http.Request) {
	if _, ok := d.requireDeploymentAdmin(w, r); !ok {
		return
	}
	rows, err := d.DB.Pool.Query(r.Context(), `
		SELECT s.id, s.ws_title, s.ws_slug, (SELECT count(*) FROM space_members m WHERE m.workspace_id = s.id)
		FROM spaces s ORDER BY s.created_at`)
	if checkErr(w, err) {
		return
	}
	defer rows.Close()
	out := []DeploymentWorkspace{}
	for rows.Next() {
		var ws DeploymentWorkspace
		if err := rows.Scan(&ws.ID, &ws.Name, &ws.Slug, &ws.MemberCount); err != nil {
			internalError(w)
			return
		}
		out = append(out, ws)
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
}

func (d *Deps) handleListDeploymentWorkspaceMembers(w http.ResponseWriter, r *http.Request) {
	if _, ok := d.requireDeploymentAdmin(w, r); !ok {
		return
	}
	workspaceID := r.PathValue("workspaceId")
	if _, err := d.Workspace.GetWorkspace(r.Context(), workspaceID); err != nil {
		httpapi.NotFound(w, "")
		return
	}
	members, err := d.Workspace.ListMembers(r.Context(), workspaceID)
	if checkErr(w, err) {
		return
	}
	out := make([]WorkspaceMember, 0, len(members))
	for _, m := range members {
		deactivated, _ := d.accountDeactivated(r.Context(), m.UserID)
		out = append(out, WorkspaceMember{UserID: m.UserID, Name: m.Name, Email: m.Email, Role: m.Role, Deactivated: deactivated})
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
}

func (d *Deps) accountDeactivated(ctx context.Context, accountID string) (bool, error) {
	var deactivated bool
	err := d.DB.Pool.QueryRow(ctx, `SELECT acct_deactivated_at IS NOT NULL FROM accounts WHERE id = $1`, accountID).Scan(&deactivated)
	return deactivated, err
}

func (d *Deps) handleSetWorkspaceOpenJoin(w http.ResponseWriter, r *http.Request) {
	actor, ok := d.requireDeploymentAdmin(w, r)
	if !ok {
		return
	}
	workspaceID := r.PathValue("workspaceId")
	var templateKey *string
	err := d.DB.Pool.QueryRow(r.Context(), `SELECT ws_template_key FROM spaces WHERE id = $1`, workspaceID).Scan(&templateKey)
	if errors.Is(err, pgx.ErrNoRows) {
		httpapi.NotFound(w, "")
		return
	}
	if checkErr(w, err) {
		return
	}
	if templateKey == nil || *templateKey == "" {
		httpapi.BadRequest(w, "workspace was not created from a role template")
		return
	}
	var body struct {
		OpenJoin *bool `json:"open_join"`
	}
	if err := httpapi.DecodeJSON(r, &body); err != nil || body.OpenJoin == nil {
		httpapi.BadRequest(w, "open_join is required")
		return
	}
	var out struct {
		ID   string
		Name string
		Slug string
	}
	if err := d.DB.Pool.QueryRow(r.Context(), `
		UPDATE spaces SET ws_open_join = $2, updated_at = now() WHERE id = $1
		RETURNING id, ws_title, ws_slug`, workspaceID, *body.OpenJoin,
	).Scan(&out.ID, &out.Name, &out.Slug); err != nil {
		internalError(w)
		return
	}
	audit := httpAudit(r, actor, "workspace.open_join.set")
	audit.WorkspaceID, audit.TargetType, audit.TargetID = ptr(workspaceID), ptr("workspace"), ptr(workspaceID)
	_ = WriteAudit(r.Context(), d.DB, audit)
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{
		"id": out.ID, "name": out.Name, "slug": out.Slug, "open_join": *body.OpenJoin,
	})
}

func (d *Deps) handleListJoinTargets(w http.ResponseWriter, r *http.Request) {
	if _, ok := httpapi.RequireActor(w, r); !ok {
		return
	}
	rows, err := d.DB.Pool.Query(r.Context(), `
		SELECT s.id, s.ws_slug, s.ws_title, COALESCE(s.ws_summary, ''), COALESCE(s.ws_template_key, ''),
			(SELECT count(*) FROM space_members m WHERE m.workspace_id = s.id)
		FROM spaces s WHERE s.ws_open_join ORDER BY s.ws_title`)
	if checkErr(w, err) {
		return
	}
	defer rows.Close()
	out := []JoinTarget{}
	for rows.Next() {
		var t JoinTarget
		if err := rows.Scan(&t.ID, &t.Slug, &t.Name, &t.Description, &t.TemplateKey, &t.MemberCount); err != nil {
			internalError(w)
			return
		}
		out = append(out, t)
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
}

var joinLimiter = httpapi.NewLimiter(20, time.Hour)

func (d *Deps) handleJoinTargetWorkspace(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return
	}
	key := actor.UserID
	if key == "" {
		key = httpapi.ClientIP(r)
	}
	if !joinLimiter.Allow(key) {
		httpapi.TooManyRequests(w, "")
		return
	}
	workspaceID := r.PathValue("workspaceId")
	var name, slug string
	var openJoin bool
	err := d.DB.Pool.QueryRow(r.Context(), `SELECT ws_title, ws_slug, ws_open_join FROM spaces WHERE id = $1`, workspaceID).
		Scan(&name, &slug, &openJoin)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !openJoin) {
		httpapi.NotFound(w, "workspace is not open for self-join")
		return
	}
	if checkErr(w, err) {
		return
	}
	if _, memberErr := d.Workspace.GetMemberByUser(r.Context(), workspaceID, actor.UserID); memberErr == nil {
		httpapi.WriteJSON(w, http.StatusOK, JoinTargetResult{ID: workspaceID, Slug: slug, Name: name, AlreadyMember: true})
		return
	}
	if _, err := d.Workspace.AddMember(r.Context(), workspaceID, actor.UserID, "member"); err != nil {
		internalError(w)
		return
	}
	d.Hub.Publish(workspaceID, realtime.Event{Type: "member.added", Payload: map[string]any{"user_id": actor.UserID}})
	httpapi.WriteJSON(w, http.StatusOK, JoinTargetResult{ID: workspaceID, Slug: slug, Name: name, AlreadyMember: false})
}

// --- fleet ---------------------------------------------------------------------

const fleetMachineLimit = 200

func (d *Deps) handleListFleet(w http.ResponseWriter, r *http.Request) {
	if _, ok := d.requireDeploymentAdmin(w, r); !ok {
		return
	}
	fleet, err := d.buildFleet(r.Context())
	if checkErr(w, err) {
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, fleet)
}

func (d *Deps) buildFleet(ctx context.Context) (Fleet, error) {
	rows, err := d.DB.Pool.Query(ctx, `
		SELECT e.ex_daemon_id, e.workspace_id, s.ws_title, s.ws_slug,
			COALESCE(a.acct_email, ''), COALESCE(a.acct_full_name, ''),
			e.ex_device_info, e.ex_status = 'online', e.ex_last_seen_at, e.id, e.ex_custom_title, e.ex_title,
			e.ex_provider, e.ex_visibility
		FROM executors e
		JOIN spaces s ON s.id = e.workspace_id
		LEFT JOIN accounts a ON a.id = e.ex_owner_account_id
		WHERE e.ex_daemon_id IS NOT NULL
		ORDER BY e.ex_last_seen_at DESC NULLS LAST`)
	if err != nil {
		return Fleet{}, fmt.Errorf("deployment: чтение fleet: %w", err)
	}
	defer rows.Close()

	byDaemon := map[string]*FleetMachine{}
	order := []string{}
	for rows.Next() {
		var daemonID, workspaceID, wsName, wsSlug, ownerEmail, ownerName, deviceInfo string
		var online bool
		var lastSeen *time.Time
		var runtimeID, customTitle, title, provider, visibility string
		if err := rows.Scan(&daemonID, &workspaceID, &wsName, &wsSlug, &ownerEmail, &ownerName,
			&deviceInfo, &online, &lastSeen, &runtimeID, &customTitle, &title, &provider, &visibility); err != nil {
			return Fleet{}, err
		}
		m, has := byDaemon[daemonID]
		if !has {
			m = &FleetMachine{DaemonID: daemonID, WorkspaceID: workspaceID, WorkspaceName: wsName, WorkspaceSlug: wsSlug,
				OwnerEmail: ownerEmail, OwnerName: ownerName, DeviceInfo: deviceInfo, Online: online, LastHeartbeatAt: lastSeen}
			byDaemon[daemonID] = m
			order = append(order, daemonID)
		}
		name := customTitle
		if name == "" {
			name = title
		}
		status := "offline"
		if online {
			status = "online"
		}
		m.Runtimes = append(m.Runtimes, FleetRuntime{ID: runtimeID, Name: name, Provider: provider, Visibility: visibility, Status: status, Online: online, LastSeenAt: lastSeen})
	}
	if err := rows.Err(); err != nil {
		return Fleet{}, err
	}

	machines := make([]FleetMachine, 0, len(order))
	for _, id := range order {
		machines = append(machines, *byDaemon[id])
	}

	summary := FleetSummary{MachinesTotal: len(machines)}
	for _, m := range machines {
		if m.Online {
			summary.MachinesOnline++
		} else {
			summary.MachinesOffline++
		}
	}
	truncated := false
	if len(machines) > fleetMachineLimit {
		machines = machines[:fleetMachineLimit]
		truncated = true
	}
	return Fleet{Machines: machines, Summary: summary, Total: summary.MachinesTotal, Truncated: truncated}, nil
}
