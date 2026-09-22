package handler

import (
	"context"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

type AgentInvocationTargetDTO struct {
	TargetType string  `json:"target_type"`
	TargetID   *string `json:"target_id"`
}

const (
	permissionModePrivate  = "private"
	permissionModePublicTo = "public_to"

	invocationTargetWorkspace = "workspace"
	invocationTargetMember    = "member"
	invocationTargetTeam      = "team"
)

func deriveLegacyVisibility(permissionMode string, targets []db.AgentInvocationTarget) string {
	if permissionMode == permissionModePublicTo {
		for _, t := range targets {
			if t.TargetType == invocationTargetWorkspace {
				return "workspace"
			}
		}
	}
	return "private"
}

func applyInvocationTargetsToResponse(resp *AgentResponse, targets []db.AgentInvocationTarget) {
	dto := make([]AgentInvocationTargetDTO, 0, len(targets))
	for _, t := range targets {
		var idPtr *string
		if t.TargetID.Valid {
			s := uuidToString(t.TargetID)
			idPtr = &s
		}
		dto = append(dto, AgentInvocationTargetDTO{TargetType: t.TargetType, TargetID: idPtr})
	}
	resp.InvocationTargets = dto
	resp.Visibility = deriveLegacyVisibility(resp.PermissionMode, targets)
}

type resolvedPermission struct {
	mode    string
	targets []targetSpec
}

type targetSpec struct {
	targetType string
	targetID   pgtype.UUID
}

func (p resolvedPermission) legacyVisibility() string {
	if p.mode == permissionModePublicTo {
		for _, t := range p.targets {
			if t.targetType == invocationTargetWorkspace {
				return "workspace"
			}
		}
	}
	return "private"
}

func parsePermissionInput(workspaceID pgtype.UUID, permissionMode *string, targets []AgentInvocationTargetDTO, hasPermissionMode, hasTargets bool, legacyVisibility *string) (resolvedPermission, bool, error) {
	if !hasPermissionMode && legacyVisibility == nil {
		return resolvedPermission{}, false, nil
	}

	if !hasPermissionMode {
		switch *legacyVisibility {
		case "workspace":
			return resolvedPermission{
				mode:    permissionModePublicTo,
				targets: []targetSpec{{targetType: invocationTargetWorkspace, targetID: workspaceID}},
			}, true, nil
		case "private", "":
			return resolvedPermission{mode: permissionModePrivate}, true, nil
		default:
			return resolvedPermission{}, false, fmt.Errorf("visibility must be 'private' or 'workspace'")
		}
	}

	mode := permissionModePrivate
	if permissionMode != nil && *permissionMode != "" {
		mode = *permissionMode
	}
	if mode != permissionModePrivate && mode != permissionModePublicTo {
		return resolvedPermission{}, false, fmt.Errorf("permission_mode must be 'private' or 'public_to'")
	}

	res := resolvedPermission{mode: mode}
	if mode == permissionModePrivate {

		return res, true, nil
	}

	if hasTargets {
		seen := map[string]struct{}{}
		for _, t := range targets {
			switch t.TargetType {
			case invocationTargetWorkspace:
				key := "workspace"
				if _, dup := seen[key]; dup {
					continue
				}
				seen[key] = struct{}{}
				res.targets = append(res.targets, targetSpec{targetType: invocationTargetWorkspace, targetID: workspaceID})
			case invocationTargetMember:
				if t.TargetID == nil || *t.TargetID == "" {
					return resolvedPermission{}, false, fmt.Errorf("member invocation target requires target_id")
				}
				uid, err := util.ParseUUID(*t.TargetID)
				if err != nil {
					return resolvedPermission{}, false, fmt.Errorf("member invocation target_id is not a valid uuid")
				}
				key := "member:" + *t.TargetID
				if _, dup := seen[key]; dup {
					continue
				}
				seen[key] = struct{}{}
				res.targets = append(res.targets, targetSpec{targetType: invocationTargetMember, targetID: uid})
			case invocationTargetTeam:
				if t.TargetID == nil || *t.TargetID == "" {
					return resolvedPermission{}, false, fmt.Errorf("team invocation target requires target_id")
				}
				tid, err := util.ParseUUID(*t.TargetID)
				if err != nil {
					return resolvedPermission{}, false, fmt.Errorf("team invocation target_id is not a valid uuid")
				}
				key := "team:" + *t.TargetID
				if _, dup := seen[key]; dup {
					continue
				}
				seen[key] = struct{}{}
				res.targets = append(res.targets, targetSpec{targetType: invocationTargetTeam, targetID: tid})
			default:
				return resolvedPermission{}, false, fmt.Errorf("invocation target_type must be 'workspace', 'member', or 'team'")
			}
		}
	}

	if len(res.targets) == 0 {
		res.targets = append(res.targets, targetSpec{targetType: invocationTargetWorkspace, targetID: workspaceID})
	}
	return res, true, nil
}

func (h *Handler) replaceInvocationTargets(ctx context.Context, agentID pgtype.UUID, createdBy pgtype.UUID, targets []targetSpec) error {
	return replaceInvocationTargetsWithQueries(ctx, h.Queries, agentID, createdBy, targets)
}

func replaceInvocationTargetsWithQueries(ctx context.Context, q *db.Queries, agentID pgtype.UUID, createdBy pgtype.UUID, targets []targetSpec) error {
	if err := q.DeleteAgentInvocationTargets(ctx, agentID); err != nil {
		return err
	}
	for _, t := range targets {
		if err := q.CreateAgentInvocationTarget(ctx, db.CreateAgentInvocationTargetParams{
			AgentID:    agentID,
			TargetType: t.targetType,
			TargetID:   t.targetID,
			CreatedBy:  createdBy,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) permissionInputChangesAgent(ctx context.Context, existing db.Agent, req UpdateAgentRequest, hasPermissionMode, hasTargets bool) (bool, error) {

	if !hasPermissionMode && !hasTargets {
		if req.Visibility == nil {
			return false, nil
		}
		current, err := h.Queries.ListAgentInvocationTargets(ctx, existing.ID)
		if err != nil {
			return true, err
		}
		submitted := "private"
		if *req.Visibility == "workspace" {
			submitted = "workspace"
		}
		return submitted != deriveLegacyVisibility(existing.PermissionMode, current), nil
	}

	var targetsDTO []AgentInvocationTargetDTO
	if req.InvocationTargets != nil {
		targetsDTO = *req.InvocationTargets
	}
	perm, ok, err := parsePermissionInput(existing.WorkspaceID, req.PermissionMode, targetsDTO, hasPermissionMode, hasTargets, req.Visibility)
	if err != nil || !ok {

		return false, nil
	}
	if perm.mode != existing.PermissionMode {
		return true, nil
	}
	current, err := h.Queries.ListAgentInvocationTargets(ctx, existing.ID)
	if err != nil {
		return true, err
	}
	want := make(map[string]struct{}, len(perm.targets))
	for _, tgt := range perm.targets {
		want[tgt.targetType+":"+uuidToString(tgt.targetID)] = struct{}{}
	}
	have := make(map[string]struct{}, len(current))
	for _, row := range current {
		have[row.TargetType+":"+uuidToString(row.TargetID)] = struct{}{}
	}
	if len(want) != len(have) {
		return true, nil
	}
	for k := range want {
		if _, ok := have[k]; !ok {
			return true, nil
		}
	}
	return false, nil
}

func (h *Handler) enrichAgentResponseWithTargets(ctx context.Context, resp *AgentResponse, agentID pgtype.UUID) error {
	targets, err := h.Queries.ListAgentInvocationTargets(ctx, agentID)
	if err != nil {
		return err
	}
	applyInvocationTargetsToResponse(resp, targets)
	return nil
}

func (h *Handler) enrichAgentResponseWithTargetsHTTP(w http.ResponseWriter, r *http.Request, resp *AgentResponse, agentID pgtype.UUID) bool {
	if err := h.enrichAgentResponseWithTargets(r.Context(), resp, agentID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load agent invocation targets")
		return false
	}
	return true
}
