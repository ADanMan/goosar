package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/provisioning"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

type WorkspaceTemplateResponse struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type workspaceTemplatePin struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Version string `json:"version"`

	Enabled *bool `json:"enabled"`
}

type workspaceTemplateHelper struct {
	Name              map[string]string `json:"name"`
	ExtraInstructions map[string]string `json:"extra_instructions"`
}

func workspaceTemplateText(m map[string]string, lang string) string {
	if v := strings.TrimSpace(m[lang]); v != "" {
		return v
	}
	for _, fallback := range []string{"en", "ru"} {
		if v := strings.TrimSpace(m[fallback]); v != "" {
			return v
		}
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if v := strings.TrimSpace(m[k]); v != "" {
			return v
		}
	}
	return ""
}

func parseWorkspaceTemplateLangMap(raw []byte) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return m
}

func (h *Handler) workspaceTemplateContentLang(ctx context.Context, userID string, acceptLanguage string) string {
	storedLanguage := ""
	if user, err := h.Queries.GetUser(ctx, parseUUID(userID)); err == nil {
		storedLanguage = user.Language.String
	}
	return helperContentLang(storedLanguage, acceptLanguage)
}

func (h *Handler) ListWorkspaceTemplates(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	lang := h.workspaceTemplateContentLang(r.Context(), userID, r.Header.Get("Accept-Language"))

	rows, err := h.Queries.ListEnabledWorkspaceTemplates(r.Context())
	if err != nil {
		slog.Error("workspace templates: failed to list", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list workspace templates")
		return
	}
	resp := make([]WorkspaceTemplateResponse, 0, len(rows))
	for _, row := range rows {
		name := workspaceTemplateText(parseWorkspaceTemplateLangMap(row.DisplayName), lang)
		if name == "" {

			name = row.Key
		}
		resp = append(resp, WorkspaceTemplateResponse{
			Key:         row.Key,
			Name:        name,
			Description: workspaceTemplateText(parseWorkspaceTemplateLangMap(row.Description), lang),
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

func parseWorkspaceTemplatePins(raw []byte) ([]workspaceTemplatePin, error) {
	if len(raw) == 0 || isJSONNull(raw) {
		return nil, nil
	}
	var pins []workspaceTemplatePin
	if err := json.Unmarshal(raw, &pins); err != nil {
		return nil, errors.New("template pins must be a JSON array of {name, type, version} entries")
	}
	seen := make(map[string]bool, len(pins))
	for _, pin := range pins {
		if !provisioning.ValidPackageType(pin.Type) {
			return nil, fmt.Errorf("template pin has invalid type %q", pin.Type)
		}
		if !provisioning.ValidPackageIdentifier(pin.Name) {
			return nil, fmt.Errorf("template pin has invalid name %q", pin.Name)
		}
		if !provisioning.ValidPackageIdentifier(pin.Version) {
			return nil, fmt.Errorf("template pin %q has invalid version %q", pin.Name, pin.Version)
		}
		key := pin.Type + ":" + pin.Name
		if seen[key] {
			return nil, fmt.Errorf("template has a duplicate pin for %s", key)
		}
		seen[key] = true
	}
	return pins, nil
}

func validateTemplateConfigMCPDoc(raw []byte) error {
	if len(raw) > maxConfigMCPDocBytes {
		return fmt.Errorf("mcp document exceeds %d bytes", maxConfigMCPDocBytes)
	}
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return errors.New("mcp document must be a JSON object of server name to entry")
	}
	for name, entryRaw := range entries {
		if name == "" || len(name) > maxConfigMCPNameLength {
			return fmt.Errorf("mcp server name must be 1-%d characters", maxConfigMCPNameLength)
		}
		var entry struct {
			Enabled *bool             `json:"enabled"`
			Env     map[string]string `json:"env"`
		}
		dec := json.NewDecoder(bytes.NewReader(entryRaw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&entry); err != nil {
			return fmt.Errorf("mcp entry %q must be an object with only enabled/env fields", name)
		}
		for envKey := range entry.Env {
			if envKey == "" {
				return fmt.Errorf("mcp entry %q has an empty env variable name", name)
			}
		}
	}
	return nil
}

func validateWorkspaceTemplateMCPDoc(raw []byte) error {
	if err := validateTemplateConfigMCPDoc(raw); err != nil {
		return err
	}
	var entries map[string]struct {
		Enabled *bool             `json:"enabled"`
		Env     map[string]string `json:"env"`
	}
	if err := json.Unmarshal(raw, &entries); err != nil {
		return errors.New("mcp document must be a JSON object of server name to entry")
	}
	for name, entry := range entries {
		for envKey, envValue := range entry.Env {
			if envValue != "" {
				return fmt.Errorf("template mcp entry %q carries a value for env %q — templates must not contain secrets or env values", name, envKey)
			}
		}
	}
	return nil
}

func workspaceTemplateMCPDocEmpty(raw []byte) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) == 0 || string(trimmed) == "null" || string(trimmed) == "{}"
}

func (h *Handler) applyWorkspaceTemplate(ctx context.Context, qtx *db.Queries, tmpl db.WorkspaceTemplate, workspaceID, actorID pgtype.UUID) (int, string) {

	pins, err := parseWorkspaceTemplatePins(tmpl.Pins)
	if err != nil {
		return http.StatusBadRequest, fmt.Sprintf("workspace template %q is invalid: %v", tmpl.Key, err)
	}
	for _, pin := range pins {
		enabled := true
		if pin.Enabled != nil {
			enabled = *pin.Enabled
		}
		if _, err := qtx.UpsertProvisioningPin(ctx, db.UpsertProvisioningPinParams{
			WorkspaceID: workspaceID,
			PackageName: pin.Name,
			PackageType: pin.Type,
			Version:     pin.Version,
			Enabled:     enabled,
			UpdatedBy:   actorID,
		}); err != nil {
			slog.Error("workspace template: failed to apply pin",
				"template", tmpl.Key, "package", pin.Type+":"+pin.Name, "error", err)
			return http.StatusInternalServerError, "failed to apply workspace template"
		}
	}

	if !workspaceTemplateMCPDocEmpty(tmpl.McpDefaults) {
		if err := validateWorkspaceTemplateMCPDoc(tmpl.McpDefaults); err != nil {
			return http.StatusBadRequest, fmt.Sprintf("workspace template %q is invalid: %v", tmpl.Key, err)
		}
		sealed, err := h.sealConfigDocument(tmpl.McpDefaults)
		if err != nil {
			if errors.Is(err, errConfigSecretKeyUnset) {
				return http.StatusServiceUnavailable, "cannot apply template MCP presets: GOOSAR_MCP_SECRET_KEY is not configured on this server"
			}
			slog.Error("workspace template: failed to seal mcp defaults", "template", tmpl.Key, "error", err)
			return http.StatusInternalServerError, "failed to apply workspace template"
		}
		if _, err := qtx.UpsertWorkspaceConfig(ctx, db.UpsertWorkspaceConfigParams{
			WorkspaceID: workspaceID,
			McpDefaults: sealed,
			UpdatedBy:   actorID,
		}); err != nil {
			slog.Error("workspace template: failed to store mcp defaults", "template", tmpl.Key, "error", err)
			return http.StatusInternalServerError, "failed to apply workspace template"
		}
	}

	var helper workspaceTemplateHelper
	if len(tmpl.Helper) > 0 && !isJSONNull(tmpl.Helper) {
		if err := json.Unmarshal(tmpl.Helper, &helper); err != nil {
			return http.StatusBadRequest, fmt.Sprintf("workspace template %q is invalid: helper must be an object with name/extra_instructions maps", tmpl.Key)
		}
	}
	nameJSON, err := json.Marshal(orEmptyLangMap(helper.Name))
	if err != nil {
		slog.Error("workspace template: failed to marshal helper name", "template", tmpl.Key, "error", err)
		return http.StatusInternalServerError, "failed to apply workspace template"
	}
	extraJSON, err := json.Marshal(orEmptyLangMap(helper.ExtraInstructions))
	if err != nil {
		slog.Error("workspace template: failed to marshal helper instructions", "template", tmpl.Key, "error", err)
		return http.StatusInternalServerError, "failed to apply workspace template"
	}
	if _, err := qtx.CreateWorkspaceHelperDefault(ctx, db.CreateWorkspaceHelperDefaultParams{
		WorkspaceID:             workspaceID,
		TemplateKey:             tmpl.Key,
		HelperName:              nameJSON,
		HelperExtraInstructions: extraJSON,
	}); err != nil {
		slog.Error("workspace template: failed to store helper defaults", "template", tmpl.Key, "error", err)
		return http.StatusInternalServerError, "failed to apply workspace template"
	}

	return 0, ""
}

func orEmptyLangMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

func resolveWorkspaceTemplateForCreate(ctx context.Context, qtx *db.Queries, key string) (db.WorkspaceTemplate, int, string) {
	tmpl, err := qtx.GetEnabledWorkspaceTemplate(ctx, key)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.WorkspaceTemplate{}, http.StatusBadRequest, fmt.Sprintf("unknown workspace template %q", key)
		}
		slog.Error("workspace template: failed to load", "template", key, "error", err)
		return db.WorkspaceTemplate{}, http.StatusInternalServerError, "failed to load workspace template"
	}
	return tmpl, 0, ""
}
