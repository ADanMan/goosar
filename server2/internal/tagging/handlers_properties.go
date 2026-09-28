package tagging

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/realtime"
)

const maxPropertyNameLen = 32
const maxPropertyOptions = 50

var propertyTypes = map[string]bool{
	"text": true, "number": true, "select": true, "multi_select": true,
	"date": true, "checkbox": true, "url": true,
}

func validPropertyConfig(propertyType string, cfg PropertyConfig) error {
	needsOptions := propertyType == "select" || propertyType == "multi_select"
	if !needsOptions {
		return nil
	}
	if len(cfg.Options) == 0 {
		return errors.New("config.options is required for select/multi_select")
	}
	if len(cfg.Options) > maxPropertyOptions {
		return fmt.Errorf("config.options must have at most %d entries", maxPropertyOptions)
	}
	names := map[string]bool{}
	ids := map[string]bool{}
	for i := range cfg.Options {
		o := &cfg.Options[i]
		if o.Name == "" {
			return errors.New("every option needs a name")
		}
		if names[o.Name] {
			return errors.New("option names must be unique")
		}
		names[o.Name] = true
		if o.ID == "" {
			o.ID = randomID()
		}
		if ids[o.ID] {
			return errors.New("option ids must be unique")
		}
		ids[o.ID] = true
	}
	return nil
}

func (d *Deps) handleListProperties(w http.ResponseWriter, r *http.Request) {
	member, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}
	includeArchived := r.URL.Query().Get("include_archived") == "true"
	list, err := d.Store.ListProperties(r.Context(), member.WorkspaceID, includeArchived)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if list == nil {
		list = []Property{}
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"properties": list, "total": len(list)})
}

type createPropertyRequest struct {
	Name        string         `json:"name"`
	Type        string         `json:"type"`
	Description string         `json:"description"`
	Icon        string         `json:"icon"`
	Config      PropertyConfig `json:"config"`
}

func (d *Deps) handleCreateProperty(w http.ResponseWriter, r *http.Request) {
	member, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}
	if !httpapi.RoleAtLeast(member.Role, httpapi.RoleOwner, httpapi.RoleAdmin) {
		httpapi.Forbidden(w, "requires owner or admin role")
		return
	}
	var req createPropertyRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > maxPropertyNameLen || hasControlChars(name) {
		httpapi.BadRequest(w, "name must be 1-32 characters without control characters")
		return
	}
	if reservedPropertyNames[name] {
		httpapi.BadRequest(w, "name is a reserved built-in field name")
		return
	}
	if !propertyTypes[req.Type] {
		httpapi.BadRequest(w, "type must be one of text, number, select, multi_select, date, checkbox, url")
		return
	}
	if err := validPropertyConfig(req.Type, req.Config); err != nil {
		httpapi.BadRequest(w, err.Error())
		return
	}
	prop, err := d.Store.CreateProperty(r.Context(), CreatePropertyParams{
		WorkspaceID: member.WorkspaceID, Name: name, Type: req.Type,
		Description: strings.TrimSpace(req.Description), Icon: strings.TrimSpace(req.Icon), Config: req.Config,
	})
	if errors.Is(err, ErrLimitExceeded()) {
		httpapi.WriteError(w, http.StatusBadRequest, "workspace already has 20 active properties", "property_limit_exceeded")
		return
	}
	if errors.Is(err, ErrNameTaken) {
		httpapi.WriteError(w, http.StatusConflict, "a property with this name already exists", "property_name_taken")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if d.Publisher != nil {
		d.Publisher.Publish(member.WorkspaceID, realtime.Event{Type: "property:created", Payload: map[string]any{"property": prop}})
	}
	httpapi.WriteJSON(w, http.StatusCreated, prop)
}

func (d *Deps) handleGetProperty(w http.ResponseWriter, r *http.Request) {
	member, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}
	prop, err := d.Store.GetProperty(r.Context(), member.WorkspaceID, r.PathValue("id"))
	if errors.Is(err, ErrNotFound) {
		httpapi.NotFound(w, "property not found")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, prop)
}

type updatePropertyRequest struct {
	Name        *string         `json:"name"`
	Description *string         `json:"description"`
	Icon        *string         `json:"icon"`
	Config      *PropertyConfig `json:"config"`
	Archived    *bool           `json:"archived"`
}

func (d *Deps) handleUpdateProperty(w http.ResponseWriter, r *http.Request) {
	member, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}
	if !httpapi.RoleAtLeast(member.Role, httpapi.RoleOwner, httpapi.RoleAdmin) {
		httpapi.Forbidden(w, "requires owner or admin role")
		return
	}
	id := r.PathValue("id")
	var req updatePropertyRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	patch := UpdatePropertyParams{Archived: req.Archived}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" || len(name) > maxPropertyNameLen || hasControlChars(name) {
			httpapi.BadRequest(w, "name must be 1-32 characters without control characters")
			return
		}
		if reservedPropertyNames[name] {
			httpapi.BadRequest(w, "name is a reserved built-in field name")
			return
		}
		patch.Name = &name
	}
	if req.Description != nil {
		patch.HasDesc = true
		desc := strings.TrimSpace(*req.Description)
		patch.Description = &desc
	}
	if req.Icon != nil {
		patch.HasIcon = true
		icon := strings.TrimSpace(*req.Icon)
		patch.Icon = &icon
	}
	if req.Config != nil {
		current, err := d.Store.GetProperty(r.Context(), member.WorkspaceID, id)
		if errors.Is(err, ErrNotFound) {
			httpapi.NotFound(w, "property not found")
			return
		}
		if err != nil {
			httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
			return
		}
		if err := validPropertyConfig(current.Type, *req.Config); err != nil {
			httpapi.BadRequest(w, err.Error())
			return
		}
		removed := removedOptionIDs(current.Config, *req.Config)
		if len(removed) > 0 {
			used, err := d.Store.OptionsInUse(r.Context(), member.WorkspaceID, id, removed)
			if err != nil {
				httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
				return
			}
			if len(used) > 0 {
				httpapi.WriteError(w, http.StatusConflict,
					"cannot remove options still in use: "+strings.Join(used, ", "), "property_options_in_use")
				return
			}
		}
		patch.Config = req.Config
	}
	prop, err := d.Store.UpdateProperty(r.Context(), member.WorkspaceID, id, patch)
	if errors.Is(err, ErrNotFound) {
		httpapi.NotFound(w, "property not found")
		return
	}
	if errors.Is(err, ErrNameTaken) {
		httpapi.WriteError(w, http.StatusConflict, "a property with this name already exists", "property_name_taken")
		return
	}
	if errors.Is(err, ErrLimitExceeded()) {
		httpapi.WriteError(w, http.StatusBadRequest, "workspace already has 20 active properties", "property_limit_exceeded")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if d.Publisher != nil {
		d.Publisher.Publish(member.WorkspaceID, realtime.Event{Type: "property:updated", Payload: map[string]any{"property": prop}})
	}
	httpapi.WriteJSON(w, http.StatusOK, prop)
}

func removedOptionIDs(old, next PropertyConfig) []string {
	keep := map[string]bool{}
	for _, o := range next.Options {
		keep[o.ID] = true
	}
	var removed []string
	for _, o := range old.Options {
		if !keep[o.ID] {
			removed = append(removed, o.ID)
		}
	}
	return removed
}

// --- issue <-> property value --------------------------------------------------

type setPropertyValueRequest struct {
	Value json.RawMessage `json:"value"`
}

func (d *Deps) handleSetIssuePropertyValue(w http.ResponseWriter, r *http.Request) {
	member, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}
	issueID, propertyID := r.PathValue("id"), r.PathValue("propertyId")
	prop, err := d.Store.GetProperty(r.Context(), member.WorkspaceID, propertyID)
	if errors.Is(err, ErrNotFound) {
		httpapi.NotFound(w, "property definition not found")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if prop.Archived {
		httpapi.WriteError(w, http.StatusBadRequest, "cannot set a value for an archived property", "property_archived")
		return
	}
	var req setPropertyValueRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil || len(req.Value) == 0 {
		httpapi.BadRequest(w, "value is required")
		return
	}
	normalized, err := validatePropertyValue(prop, req.Value)
	if err != nil {
		httpapi.BadRequest(w, err.Error())
		return
	}
	updated, err := d.Store.SetIssuePropertyValue(r.Context(), member.WorkspaceID, issueID, propertyID, normalized)
	if errors.Is(err, ErrNotFound) {
		httpapi.NotFound(w, "issue not found")
		return
	}
	if errors.Is(err, ErrPropertyBlockTooLarge) {
		httpapi.WriteError(w, http.StatusBadRequest, "the properties block for this issue exceeds 16 KB", "properties_block_too_large")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.publishIssueProperties(member.WorkspaceID, issueID, updated)
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"properties": json.RawMessage(updated)})
}

func (d *Deps) handleDeleteIssuePropertyValue(w http.ResponseWriter, r *http.Request) {
	member, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}
	issueID, propertyID := r.PathValue("id"), r.PathValue("propertyId")
	updated, err := d.Store.DeleteIssuePropertyValue(r.Context(), member.WorkspaceID, issueID, propertyID)
	if errors.Is(err, ErrNotFound) {
		httpapi.NotFound(w, "issue not found")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	d.publishIssueProperties(member.WorkspaceID, issueID, updated)
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"properties": json.RawMessage(updated)})
}

func (d *Deps) publishIssueProperties(workspaceID, issueID string, properties json.RawMessage) {
	if d.Publisher == nil {
		return
	}
	d.Publisher.Publish(workspaceID, realtime.Event{
		Type:    "issue_properties:changed",
		Payload: map[string]any{"issue_id": issueID, "properties": json.RawMessage(properties)},
	})
}

// validatePropertyValue проверяет/нормализует value под тип определения
// (контракт §setIssuePropertyValue) и возвращает его как валидный JSON для
// прямой записи в jsonb.
func validatePropertyValue(prop Property, raw json.RawMessage) (json.RawMessage, error) {
	switch prop.Type {
	case "text", "url":
		var s string
		if err := json.Unmarshal(raw, &s); err != nil || strings.TrimSpace(s) == "" {
			return nil, fmt.Errorf("value must be a non-empty string for a %s property", prop.Type)
		}
		return raw, nil
	case "number":
		var f float64
		if err := json.Unmarshal(raw, &f); err != nil {
			return nil, errors.New("value must be a number")
		}
		return raw, nil
	case "checkbox":
		var b bool
		if err := json.Unmarshal(raw, &b); err != nil {
			return nil, errors.New("value must be a boolean")
		}
		return raw, nil
	case "date":
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, errors.New("value must be a YYYY-MM-DD string")
		}
		if _, err := time.Parse("2006-01-02", s); err != nil {
			return nil, errors.New("value must be a YYYY-MM-DD date")
		}
		return raw, nil
	case "select":
		var id string
		if err := json.Unmarshal(raw, &id); err != nil {
			return nil, errors.New("value must be an option id")
		}
		if !hasOption(prop.Config.Options, id) {
			return nil, errors.New("value is not one of this property's options")
		}
		return raw, nil
	case "multi_select":
		var ids []string
		if err := json.Unmarshal(raw, &ids); err != nil || len(ids) == 0 {
			return nil, errors.New("value must be a non-empty array of option ids")
		}
		ordered := normalizeMultiSelect(prop.Config.Options, ids)
		if len(ordered) == 0 {
			return nil, errors.New("value is not one of this property's options")
		}
		return json.Marshal(ordered)
	default:
		return nil, fmt.Errorf("unknown property type %q", prop.Type)
	}
}

func hasOption(options []PropertyOption, id string) bool {
	for _, o := range options {
		if o.ID == id {
			return true
		}
	}
	return false
}

// normalizeMultiSelect схлопывает дубликаты и упорядочивает по порядку
// вариантов в определении (контракт: "дубликаты схлопываются, порядок
// нормализуется по порядку вариантов в определении").
func normalizeMultiSelect(options []PropertyOption, ids []string) []string {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var out []string
	for _, o := range options {
		if want[o.ID] {
			out = append(out, o.ID)
		}
	}
	return out
}
