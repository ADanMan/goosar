package skill

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// importRequest — форма JSON-режима importSkill.
type importRequest struct {
	URL        string  `json:"url"`
	OnConflict *string `json:"on_conflict"`
}

func (d *Deps) handleImport(w http.ResponseWriter, r *http.Request) {
	m, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}

	contentType := r.Header.Get("Content-Type")
	var (
		name, description, content string
		files                      []FileInput
		onConflict                 *string
		importErr                  error
	)

	if strings.HasPrefix(contentType, "multipart/form-data") {
		if err := r.ParseMultipartForm(MaxUploadBytes); err != nil {
			httpapi.BadRequest(w, "invalid multipart body")
			return
		}
		if oc := r.FormValue("on_conflict"); oc != "" {
			onConflict = &oc
		}
		file, _, ferr := r.FormFile("file")
		if ferr != nil {
			httpapi.BadRequest(w, "file is required")
			return
		}
		defer file.Close()
		data, rerr := io.ReadAll(io.LimitReader(file, MaxUploadBytes+1))
		if rerr != nil {
			httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
			return
		}
		if int64(len(data)) > MaxUploadBytes {
			httpapi.WriteError(w, http.StatusRequestEntityTooLarge, "uploaded archive exceeds 16MB", "import_too_large")
			return
		}
		name, description, content, files, importErr = FromZip(data)
	} else {
		var req importRequest
		if err := httpapi.DecodeJSON(r, &req); err != nil || req.URL == "" {
			httpapi.BadRequest(w, "url is required")
			return
		}
		onConflict = req.OnConflict
		name, description, content, files, importErr = FromURL(r.Context(), req.URL)
	}

	if importErr != nil {
		writeImportError(w, importErr)
		return
	}
	if name == "" {
		httpapi.BadRequest(w, "could not determine a name for the imported skill")
		return
	}

	if onConflict == nil {
		// on_conflict не передан вовсе: обратная совместимость — "голый"
		// Skill/ошибка, не SkillImportResult (contract §2).
		sk, storedFiles, err := d.Store.Create(r.Context(), CreateParams{
			WorkspaceID: m.WorkspaceID, Name: name, Summary: description, Content: content, CreatedBy: m.UserID, Files: files,
		})
		if errors.Is(err, ErrNameTaken) {
			existing, _, _ := d.Store.ByName(r.Context(), m.WorkspaceID, name)
			httpapi.WriteJSON(w, http.StatusConflict, map[string]any{
				"error": "a skill with this name already exists",
				"existing_skill": ExistingIdentity{ID: existing.ID, Name: existing.Name,
					CanOverwrite: existing.CreatedBy != nil && *existing.CreatedBy == m.UserID},
			})
			return
		}
		if err != nil {
			httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
			return
		}
		view := sk.WithFiles(storedFiles)
		d.notify(m.WorkspaceID, "skill:created", map[string]any{"skill": view})
		httpapi.WriteJSON(w, http.StatusCreated, view)
		return
	}

	result := applyOnConflict(r.Context(), d.DB, d.Store, m.WorkspaceID, m.UserID, *onConflict, name, description, content, files)
	switch result.Status {
	case "created":
		d.notify(m.WorkspaceID, "skill:created", map[string]any{"skill": result.Skill})
		httpapi.WriteJSON(w, http.StatusCreated, result)
	case "updated":
		d.notify(m.WorkspaceID, "skill:updated", map[string]any{"skill": result.Skill})
		httpapi.WriteJSON(w, http.StatusOK, result)
	case "failed":
		httpapi.WriteJSON(w, http.StatusForbidden, result)
	default: // skipped, conflict
		httpapi.WriteJSON(w, http.StatusOK, result)
	}
}

func writeImportError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrSkillMDMissing):
		httpapi.BadRequest(w, "SKILL.md is required and must not be empty")
	case errors.Is(err, ErrSourceUnsupported):
		httpapi.BadRequest(w, "unsupported import source")
	case errors.Is(err, ErrSourceDisabled):
		httpapi.WriteJSON(w, http.StatusForbidden, map[string]any{"code": "source_disabled", "error": err.Error()})
	case errors.Is(err, ErrTooManyFiles), errors.Is(err, ErrTooLarge):
		httpapi.WriteError(w, http.StatusRequestEntityTooLarge, err.Error(), "import_too_large")
	case errors.Is(err, ErrUpstreamUnavailable):
		httpapi.WriteJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
	default:
		httpapi.BadRequest(w, err.Error())
	}
}
