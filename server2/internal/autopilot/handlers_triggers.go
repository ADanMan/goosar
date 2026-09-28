package autopilot

import (
	"encoding/json"
	"net/http"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

var validTriggerKinds = map[string]bool{"schedule": true, "webhook": true, "api": true}
var validProviders = map[string]bool{"generic": true, "github": true}

type createTriggerRequest struct {
	Kind           string          `json:"kind"`
	CronExpression *string         `json:"cron_expression"`
	Timezone       *string         `json:"timezone"`
	Label          *string         `json:"label"`
	Provider       *string         `json:"provider"`
	EventFilters   json.RawMessage `json:"event_filters"`
}

func (d *Deps) handleCreateTrigger(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return
	}
	member, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}
	autopilotID := r.PathValue("id")
	if !d.requireWriteAccess(w, r, autopilotID, actor.UserID, member.Role) {
		return
	}
	var req createTriggerRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	if !validTriggerKinds[req.Kind] {
		httpapi.BadRequest(w, "kind must be schedule, webhook or api")
		return
	}
	if req.Kind == "schedule" {
		if req.CronExpression == nil || *req.CronExpression == "" {
			httpapi.BadRequest(w, "cron_expression is required for kind=schedule")
			return
		}
		if _, err := ParseCron(*req.CronExpression); err != nil {
			httpapi.BadRequest(w, err.Error())
			return
		}
		if req.Timezone != nil {
			if _, err := LoadTimezone(*req.Timezone); err != nil {
				httpapi.BadRequest(w, err.Error())
				return
			}
		}
	}
	if req.Kind == "webhook" && req.Provider != nil && !validProviders[*req.Provider] {
		httpapi.BadRequest(w, "provider must be generic or github")
		return
	}

	t, err := d.Store.CreateTrigger(r.Context(), autopilotID, CreateTriggerParams{
		Kind: req.Kind, CronExpression: req.CronExpression, Timezone: req.Timezone,
		Label: req.Label, Provider: req.Provider, EventFilters: req.EventFilters,
	})
	if _, invalid := err.(*ErrInvalidCron); invalid {
		httpapi.BadRequest(w, err.Error())
		return
	}
	if _, invalid := err.(*ErrInvalidTimezone); invalid {
		httpapi.BadRequest(w, err.Error())
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.publish(member.WorkspaceID, "autopilot:updated", map[string]any{
		"autopilot_id": autopilotID, "trigger": t.Sanitized(),
	})
	httpapi.WriteJSON(w, http.StatusCreated, t.ToJSON(d.baseURL(r)))
}

type updateTriggerRequest struct {
	Enabled      *bool           `json:"enabled"`
	CronExpr     *string         `json:"cron_expression"`
	Timezone     *string         `json:"timezone"`
	Label        *string         `json:"label"`
	EventFilters json.RawMessage `json:"event_filters"`
}

func (d *Deps) handleUpdateTrigger(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return
	}
	member, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}
	autopilotID, triggerID := r.PathValue("id"), r.PathValue("triggerId")
	if !d.requireWriteAccess(w, r, autopilotID, actor.UserID, member.Role) {
		return
	}
	var req updateTriggerRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	if req.CronExpr != nil {
		if _, err := ParseCron(*req.CronExpr); err != nil {
			httpapi.BadRequest(w, err.Error())
			return
		}
	}
	if req.Timezone != nil {
		if _, err := LoadTimezone(*req.Timezone); err != nil {
			httpapi.BadRequest(w, err.Error())
			return
		}
	}
	t, err := d.Store.UpdateTrigger(r.Context(), autopilotID, triggerID, TriggerUpdatePatch{
		Enabled: req.Enabled, CronExpr: req.CronExpr, Timezone: req.Timezone, Label: req.Label,
		Filters: req.EventFilters,
	})
	if err == ErrTriggerNotFound {
		httpapi.NotFound(w, "trigger not found")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.publish(member.WorkspaceID, "autopilot:updated", map[string]any{
		"autopilot_id": autopilotID, "trigger": t.Sanitized(),
	})
	httpapi.WriteJSON(w, http.StatusOK, t.ToJSON(d.baseURL(r)))
}

func (d *Deps) handleDeleteTrigger(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return
	}
	member, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}
	autopilotID, triggerID := r.PathValue("id"), r.PathValue("triggerId")
	if !d.requireWriteAccess(w, r, autopilotID, actor.UserID, member.Role) {
		return
	}
	if err := d.Store.DeleteTrigger(r.Context(), autopilotID, triggerID); err == ErrTriggerNotFound {
		httpapi.NotFound(w, "trigger not found")
		return
	} else if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.publish(member.WorkspaceID, "autopilot:updated", map[string]any{"autopilot_id": autopilotID, "trigger_id": triggerID})
	w.WriteHeader(http.StatusNoContent)
}

func (d *Deps) handleRotateWebhookToken(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return
	}
	member, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}
	autopilotID, triggerID := r.PathValue("id"), r.PathValue("triggerId")
	if !d.requireWriteAccess(w, r, autopilotID, actor.UserID, member.Role) {
		return
	}
	t, err := d.Store.RotateWebhookToken(r.Context(), autopilotID, triggerID)
	if err == ErrTriggerNotFound {
		httpapi.NotFound(w, "trigger not found")
		return
	}
	if err == ErrNotWebhookTrigger {
		httpapi.BadRequest(w, "trigger is not a webhook trigger")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.publish(member.WorkspaceID, "autopilot:updated", map[string]any{
		"autopilot_id": autopilotID, "trigger": t.Sanitized(),
	})
	httpapi.WriteJSON(w, http.StatusOK, t.ToJSON(d.baseURL(r)))
}

type setSigningSecretRequest struct {
	SigningSecret string `json:"signing_secret"`
}

func (d *Deps) handleSetSigningSecret(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return
	}
	member, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}
	autopilotID, triggerID := r.PathValue("id"), r.PathValue("triggerId")
	if !d.requireWriteAccess(w, r, autopilotID, actor.UserID, member.Role) {
		return
	}
	var req setSigningSecretRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	if req.SigningSecret != "" && len(req.SigningSecret) < 16 {
		httpapi.BadRequest(w, "signing_secret must be empty or at least 16 characters")
		return
	}
	t, err := d.Store.SetSigningSecret(r.Context(), autopilotID, triggerID, d.McpSecretKey, req.SigningSecret)
	if err == ErrTriggerNotFound {
		httpapi.NotFound(w, "trigger not found")
		return
	}
	if err == ErrNotWebhookTrigger {
		httpapi.BadRequest(w, "trigger is not a webhook trigger")
		return
	}
	if err == ErrEncryptionUnavailable {
		httpapi.WriteError(w, http.StatusServiceUnavailable, "encryption key not configured", "mfa_unavailable")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.publish(member.WorkspaceID, "autopilot:updated", map[string]any{
		"autopilot_id": autopilotID, "trigger": t.Sanitized(),
	})
	httpapi.WriteJSON(w, http.StatusOK, t.ToJSON(d.baseURL(r)))
}
