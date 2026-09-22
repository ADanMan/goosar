package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"
)

type WorkspaceCapabilityResponse struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

type WorkspaceCapabilitiesResponse struct {
	TemplateKey string `json:"template_key"`

	RoleName    string `json:"role_name"`
	RoleSummary string `json:"role_summary"`

	Capabilities []WorkspaceCapabilityResponse `json:"capabilities"`

	SampleTasks []WorkspaceSampleTaskResponse `json:"sample_tasks"`
}

type WorkspaceSampleTaskResponse struct {
	Key   string `json:"key"`
	Title string `json:"title"`

	Prompt string `json:"prompt"`

	Requires []string `json:"requires"`
}

type workspaceTemplateCapability struct {
	Key   string            `json:"key"`
	Title map[string]string `json:"title"`
	Body  map[string]string `json:"body"`
}

type workspaceTemplateSampleTask struct {
	Key      string            `json:"key"`
	Title    map[string]string `json:"title"`
	Prompt   map[string]string `json:"prompt"`
	Requires []string          `json:"requires"`
}

func parseWorkspaceTemplateSampleTasks(raw []byte) []workspaceTemplateSampleTask {
	if len(raw) == 0 || isJSONNull(raw) {
		return nil
	}
	var rawEntries []json.RawMessage
	if err := json.Unmarshal(raw, &rawEntries); err != nil {
		return nil
	}
	entries := make([]workspaceTemplateSampleTask, 0, len(rawEntries))
	for _, rawEntry := range rawEntries {
		var entry workspaceTemplateSampleTask
		if err := json.Unmarshal(rawEntry, &entry); err != nil {
			slog.Warn("workspace capabilities: skipping a malformed sample task",
				"error", err)
			continue
		}
		entries = append(entries, entry)
	}
	return entries
}

func parseWorkspaceTemplateCapabilities(raw []byte) []workspaceTemplateCapability {
	if len(raw) == 0 || isJSONNull(raw) {
		return nil
	}
	var rawEntries []json.RawMessage
	if err := json.Unmarshal(raw, &rawEntries); err != nil {
		return nil
	}
	entries := make([]workspaceTemplateCapability, 0, len(rawEntries))
	for _, rawEntry := range rawEntries {
		var entry workspaceTemplateCapability
		if err := json.Unmarshal(rawEntry, &entry); err != nil {
			slog.Warn("workspace capabilities: skipping a malformed capability entry",
				"error", err)
			continue
		}
		entries = append(entries, entry)
	}
	return entries
}

func (h *Handler) GetWorkspaceCapabilities(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	id := workspaceIDFromURL(r, "id")
	idUUID, ok := parseUUIDOrBadRequest(w, id, "workspace id")
	if !ok {
		return
	}

	empty := WorkspaceCapabilitiesResponse{
		Capabilities: []WorkspaceCapabilityResponse{},
		SampleTasks:  []WorkspaceSampleTaskResponse{},
	}

	defaults, err := h.Queries.GetWorkspaceHelperDefault(r.Context(), idUUID)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Error("workspace capabilities: failed to read helper defaults",
				"workspace_id", id, "error", err)
		}
		writeJSON(w, http.StatusOK, empty)
		return
	}
	if defaults.TemplateKey == "" {
		writeJSON(w, http.StatusOK, empty)
		return
	}
	empty.TemplateKey = defaults.TemplateKey

	tmpl, err := h.Queries.GetEnabledWorkspaceTemplate(r.Context(), defaults.TemplateKey)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Error("workspace capabilities: failed to read template",
				"template", defaults.TemplateKey, "error", err)
		}
		writeJSON(w, http.StatusOK, empty)
		return
	}

	lang := h.workspaceTemplateContentLang(r.Context(), userID, r.Header.Get("Accept-Language"))
	resp := WorkspaceCapabilitiesResponse{
		TemplateKey: tmpl.Key,
		RoleName:    workspaceTemplateText(parseWorkspaceTemplateLangMap(tmpl.DisplayName), lang),
		RoleSummary: workspaceTemplateText(parseWorkspaceTemplateLangMap(tmpl.Description), lang),

		Capabilities: []WorkspaceCapabilityResponse{},
		SampleTasks:  []WorkspaceSampleTaskResponse{},
	}

	for _, entry := range parseWorkspaceTemplateCapabilities(tmpl.Capabilities) {
		title := workspaceTemplateText(entry.Title, lang)
		if title == "" {

			continue
		}
		resp.Capabilities = append(resp.Capabilities, WorkspaceCapabilityResponse{
			Key:   entry.Key,
			Title: title,
			Body:  workspaceTemplateText(entry.Body, lang),
		})
	}
	for _, entry := range parseWorkspaceTemplateSampleTasks(tmpl.SampleTasks) {
		title := workspaceTemplateText(entry.Title, lang)
		prompt := workspaceTemplateText(entry.Prompt, lang)

		if title == "" || prompt == "" {
			continue
		}
		requires := entry.Requires
		if requires == nil {
			requires = []string{}
		}
		resp.SampleTasks = append(resp.SampleTasks, WorkspaceSampleTaskResponse{
			Key:      entry.Key,
			Title:    title,
			Prompt:   prompt,
			Requires: requires,
		})
	}
	writeJSON(w, http.StatusOK, resp)
}
