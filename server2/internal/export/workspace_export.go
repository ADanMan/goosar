package export

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// archiveWriter собирает .tar.gz из именованных JSON-документов — общая для
// workspace- и me-экспорта форма архива (contract не фиксирует формат
// содержимого, только то, что это application/gzip-поток; решение T-029 в
// server2/docs/decisions.md: один JSON-файл на сущность внутри архива,
// понятный без документации самим именем файла).
type archiveWriter struct {
	gz *gzip.Writer
	tw *tar.Writer
}

func newArchiveWriter(w io.Writer) *archiveWriter {
	gz := gzip.NewWriter(w)
	return &archiveWriter{gz: gz, tw: tar.NewWriter(gz)}
}

func (a *archiveWriter) WriteJSON(name string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := a.tw.WriteHeader(&tar.Header{Name: name, Size: int64(len(raw)), Mode: 0o644}); err != nil {
		return err
	}
	_, err = a.tw.Write(raw)
	return err
}

func (a *archiveWriter) Close() error {
	if err := a.tw.Close(); err != nil {
		return err
	}
	return a.gz.Close()
}

func requireOwner(w http.ResponseWriter, r *http.Request) (*httpapi.Actor, string, bool) {
	actor, ok := httpapi.RequireHuman(w, r)
	if !ok {
		return nil, "", false
	}
	return actor, r.PathValue("id"), true
}

// handleStartWorkspaceExport — POST /api/workspaces/{id}/export.
func (d *Deps) handleStartWorkspaceExport(w http.ResponseWriter, r *http.Request) {
	actor, workspaceID, ok := requireOwner(w, r)
	if !ok {
		return
	}
	if d.Storage == nil {
		httpapi.WriteError(w, http.StatusServiceUnavailable, "не настроено файловое хранилище", "storage_not_configured")
		return
	}
	job, err := d.Store.CreateJob(r.Context(), workspaceID)
	if errors.Is(err, ErrActiveJobExists()) {
		httpapi.WriteError(w, http.StatusConflict, "экспорт этого пространства уже выполняется", "export_already_running")
		return
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if err := d.Store.RecordAudit(r.Context(), workspaceID, actor.UserID, "workspace.export", job.ID); err != nil {
		d.Logger.Warn("export: не удалось записать аудит", "err", err)
	}
	go d.runWorkspaceExport(job.ID, workspaceID)
	httpapi.WriteJSON(w, http.StatusAccepted, jobJSON(job))
}

// runWorkspaceExport — фоновая сборка архива; таймаут — d.JobTimeout
// (contract: "таймаут настраивается деплоем, по умолчанию 2 часа").
func (d *Deps) runWorkspaceExport(jobID, workspaceID string) {
	ctx, cancel := context.WithTimeout(context.Background(), d.JobTimeout)
	defer cancel()

	if err := d.Store.MarkRunning(ctx, jobID); err != nil {
		d.Logger.Error("export: MarkRunning", "job_id", jobID, "err", err)
		return
	}
	size, manifest, storageURI, err := d.buildWorkspaceArchive(ctx, jobID, workspaceID)
	if err != nil {
		d.Logger.Error("export: сборка архива провалилась", "job_id", jobID, "err", err)
		_ = d.Store.MarkFailed(context.Background(), jobID, err.Error())
		return
	}
	if err := d.Store.MarkCompleted(context.Background(), jobID, storageURI, size, manifest); err != nil {
		d.Logger.Error("export: MarkCompleted", "job_id", jobID, "err", err)
	}
}

func storageKey(jobID string) string { return jobID + ".tar.gz" }

func (d *Deps) buildWorkspaceArchive(ctx context.Context, jobID, workspaceID string) (size int64, manifestOut map[string]any, storageURI string, err error) {
	ws, err := d.Store.Workspace(ctx, workspaceID)
	if err != nil {
		return 0, nil, "", err
	}
	members, err := d.Store.MembersJSON(ctx, workspaceID)
	if err != nil {
		return 0, nil, "", err
	}
	tickets, err := d.Store.TicketsJSON(ctx, workspaceID)
	if err != nil {
		return 0, nil, "", err
	}
	notes, err := d.Store.NotesJSON(ctx, workspaceID)
	if err != nil {
		return 0, nil, "", err
	}
	projects, err := d.Store.ProjectsJSON(ctx, workspaceID)
	if err != nil {
		return 0, nil, "", err
	}

	var buf bytes.Buffer
	aw := newArchiveWriter(&buf)
	manifest := map[string]any{
		"generated_at": time.Now().UTC(),
		"workspace":    map[string]any{"id": ws.ID, "name": ws.Name, "slug": ws.Slug},
		"counts": map[string]any{
			"members": len(members), "tickets": len(tickets), "notes": len(notes), "projects": len(projects),
		},
	}
	files := []struct {
		name string
		v    any
	}{
		{"manifest.json", manifest},
		{"workspace.json", ws},
		{"members.json", members},
		{"tickets.json", tickets},
		{"notes.json", notes},
		{"projects.json", projects},
	}
	for _, f := range files {
		if err := aw.WriteJSON(f.name, f.v); err != nil {
			return 0, nil, "", err
		}
	}
	if err := aw.Close(); err != nil {
		return 0, nil, "", err
	}

	stored, err := d.Storage.Save(ctx, "exports/workspace/"+workspaceID, storageKey(jobID), bytes.NewReader(buf.Bytes()))
	if err != nil {
		return 0, nil, "", err
	}
	return int64(buf.Len()), manifest, stored.Key, nil
}

func jobJSON(j Job) map[string]any {
	var downloadURL any
	if j.Status == "completed" {
		downloadURL = "/api/workspaces/" + j.WorkspaceID + "/export/" + j.ID + "/download"
	}
	return map[string]any{
		"id": j.ID, "workspace_id": j.WorkspaceID, "status": j.Status, "error": j.Error,
		"size_bytes": j.SizeBytes, "created_at": j.CreatedAt, "completed_at": j.CompletedAt,
		"manifest": j.Manifest, "download_url": downloadURL,
	}
}

// handleGetWorkspaceExport — GET /api/workspaces/{id}/export/{jobId}.
func (d *Deps) handleGetWorkspaceExport(w http.ResponseWriter, r *http.Request) {
	_, workspaceID, ok := requireOwner(w, r)
	if !ok {
		return
	}
	job, found, err := d.Store.GetJob(r.Context(), workspaceID, r.PathValue("jobId"))
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if !found {
		httpapi.NotFound(w, "export job not found")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, jobJSON(job))
}

// handleDownloadWorkspaceExport — GET /api/workspaces/{id}/export/{jobId}/download.
func (d *Deps) handleDownloadWorkspaceExport(w http.ResponseWriter, r *http.Request) {
	_, workspaceID, ok := requireOwner(w, r)
	if !ok {
		return
	}
	job, found, err := d.Store.GetJob(r.Context(), workspaceID, r.PathValue("jobId"))
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if !found {
		httpapi.NotFound(w, "export job not found")
		return
	}
	if job.Status != "completed" {
		httpapi.WriteError(w, http.StatusConflict, "job ещё не завершён успешно — архива нет", "export_not_ready")
		return
	}
	if job.StorageURI == nil || d.Storage == nil {
		httpapi.WriteError(w, http.StatusGone, "архив был на диске, но уже удалён (истекло хранение)", "export_expired")
		return
	}
	if time.Since(job.CreatedAt) > d.Retention {
		_ = d.Store.ClearStorageURI(r.Context(), job.ID)
		httpapi.WriteError(w, http.StatusGone, "архив был на диске, но уже удалён (истекло хранение)", "export_expired")
		return
	}
	content, size, err := d.Storage.Open(r.Context(), *job.StorageURI)
	if err != nil {
		httpapi.WriteError(w, http.StatusGone, "архив был на диске, но уже удалён (истекло хранение)", "export_expired")
		return
	}
	defer content.Close()
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="workspace-export.tar.gz"`)
	if size > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, content)
}

// handleMeExport — GET /api/me/export: стримит .tar.gz со всеми данными
// вызывающего (contract §3.9). Синхронно (в отличие от workspace-экспорта):
// contract не описывает job/поллинг для этой ручки, только прямой поток
// ответа — решение T-029: собрать архив целиком в памяти и отдать одним
// Write, тот же archiveWriter, что и workspace-экспорт выше.
func (d *Deps) handleMeExport(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireHuman(w, r)
	if !ok {
		return
	}
	profile, err := d.Store.Profile(r.Context(), actor.UserID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	memberships, err := d.Store.MembershipsJSON(r.Context(), actor.UserID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	tickets, err := d.Store.TicketsCreatedByJSON(r.Context(), actor.UserID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	tokens, err := d.Store.TokensJSON(r.Context(), actor.UserID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	sessions, err := d.Store.SessionsJSON(r.Context(), actor.UserID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}

	var buf bytes.Buffer
	aw := newArchiveWriter(&buf)
	meFiles := []struct {
		name string
		v    any
	}{
		{"manifest.json", map[string]any{"generated_at": time.Now().UTC(), "account_id": actor.UserID}},
		{"profile.json", profile},
		{"memberships.json", memberships},
		{"tickets_created.json", tickets},
		{"tokens.json", tokens},
		{"sessions.json", sessions},
	}
	for _, f := range meFiles {
		if err := aw.WriteJSON(f.name, f.v); err != nil {
			httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
			return
		}
	}
	if err := aw.Close(); err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}

	if err := d.Store.RecordAudit(r.Context(), "", actor.UserID, "me.export", actor.UserID); err != nil {
		d.Logger.Warn("export: не удалось записать аудит me/export", "err", err)
	}

	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="my-data-export.tar.gz"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}
