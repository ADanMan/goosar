package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/dataexport"
	"github.com/adanman/goosar/server/internal/storage"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

const (
	ExportDirEnvVar = "GOOSAR_EXPORT_DIR"

	DefaultUploadDir = "./data/uploads"

	ExportDirSuffix = storage.ExportsSubdir

	ExportTimeoutEnvVar = "GOOSAR_EXPORT_TIMEOUT"

	DefaultExportTimeout = 2 * time.Hour

	ExportMaxBytesEnvVar = "GOOSAR_EXPORT_MAX_BYTES"

	ExportRetentionEnvVar = "GOOSAR_EXPORT_RETENTION"

	DefaultExportRetention = 7 * 24 * time.Hour

	adminAuditActionWorkspaceExport = "workspace.export"
	adminAuditActionUserExport      = "user.export"
)

type ExportJobResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	Status      string  `json:"status"`
	Error       *string `json:"error"`
	SizeBytes   int64   `json:"size_bytes"`
	CreatedAt   string  `json:"created_at"`
	CompletedAt *string `json:"completed_at"`

	Manifest json.RawMessage `json:"manifest"`

	DownloadURL *string `json:"download_url"`
}

func exportJobResponse(job db.ExportJob) ExportJobResponse {
	resp := ExportJobResponse{
		ID:          uuidToString(job.ID),
		WorkspaceID: uuidToString(job.WorkspaceID),
		Status:      job.Status,
		SizeBytes:   job.SizeBytes,
		CreatedAt:   job.CreatedAt.Time.UTC().Format(time.RFC3339),
	}
	if job.Error.Valid {
		msg := job.Error.String
		resp.Error = &msg
	}
	if job.CompletedAt.Valid {
		at := job.CompletedAt.Time.UTC().Format(time.RFC3339)
		resp.CompletedAt = &at
	}
	if len(job.Manifest) > 0 {
		resp.Manifest = json.RawMessage(job.Manifest)
	}
	if job.Status == "completed" && job.FilePath.Valid {
		url := "/api/workspaces/" + uuidToString(job.WorkspaceID) + "/export/" + uuidToString(job.ID) + "/download"
		resp.DownloadURL = &url
	}
	return resp
}

func ExportDir() string {
	if dir := strings.TrimSpace(os.Getenv(ExportDirEnvVar)); dir != "" {
		return dir
	}
	uploads := strings.TrimSpace(os.Getenv("LOCAL_UPLOAD_DIR"))
	if uploads == "" {
		uploads = DefaultUploadDir
	}
	return filepath.Join(uploads, ExportDirSuffix)
}

const ExportPartialSuffix = ".part"

func ExportArchivePath(workspaceID, jobID string) string {
	return filepath.Join(ExportDir(), "workspace-"+workspaceID+"-"+jobID+".tar.gz")
}

func ExportTimeout() time.Duration {
	return envPositiveDuration(ExportTimeoutEnvVar, DefaultExportTimeout)
}

func ExportRetention() time.Duration {
	raw := strings.TrimSpace(os.Getenv(ExportRetentionEnvVar))
	if raw == "" {
		return DefaultExportRetention
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value < 0 {
		slog.Warn("export retention: value is not a duration, using the default",
			"var", ExportRetentionEnvVar, "value", raw, "default", DefaultExportRetention.String())
		return DefaultExportRetention
	}
	return value
}

func ExportMaxBytes() int64 {
	raw := strings.TrimSpace(os.Getenv(ExportMaxBytesEnvVar))
	if raw == "" {
		return dataexport.DefaultMaxBytes
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		slog.Warn("export limit: value is not a positive integer, using the default",
			"var", ExportMaxBytesEnvVar, "value", raw, "default_bytes", dataexport.DefaultMaxBytes)
		return dataexport.DefaultMaxBytes
	}
	return value
}

func envPositiveDuration(name string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		slog.Warn("export config: value is not a positive duration, using the default",
			"var", name, "value", raw, "default", fallback.String())
		return fallback
	}
	return value
}

func (h *Handler) StartWorkspaceExport(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	requester, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	if requester.Role != "owner" {
		writeError(w, http.StatusForbidden, "only a workspace owner can export the workspace")
		return
	}
	if h.Storage == nil {
		writeError(w, http.StatusServiceUnavailable, "no storage backend is configured, so attachment files could not be included in the export")
		return
	}

	actorUUID := parseUUID(requestUserID(r))
	job, err := h.Queries.CreateExportJob(r.Context(), db.CreateExportJobParams{
		WorkspaceID: requester.WorkspaceID,
		RequestedBy: actorUUID,
	})
	if isUniqueViolation(err) {
		writeError(w, http.StatusConflict, "an export of this workspace is already running")
		return
	}
	if err != nil {
		slog.Error("workspace export: filing the job failed", "error", err, "workspace_id", workspaceID)
		writeError(w, http.StatusInternalServerError, "failed to start the export")
		return
	}

	h.writeWorkspaceExportAudit(r, actorUUID, adminAuditActionWorkspaceExport, workspaceID)
	h.runExportJob(job)
	writeJSON(w, http.StatusAccepted, exportJobResponse(job))
}

func (h *Handler) GetWorkspaceExport(w http.ResponseWriter, r *http.Request) {
	job, ok := h.loadExportJob(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, exportJobResponse(job))
}

func (h *Handler) DownloadWorkspaceExport(w http.ResponseWriter, r *http.Request) {
	job, ok := h.loadExportJob(w, r)
	if !ok {
		return
	}
	if job.Status != "completed" || !job.FilePath.Valid {
		writeError(w, http.StatusConflict, "this export has no archive to download")
		return
	}
	file, err := os.Open(job.FilePath.String)
	if err != nil {

		slog.Warn("workspace export: archive is missing from the volume",
			"error", err, "job_id", uuidToString(job.ID), "path", job.FilePath.String)
		writeError(w, http.StatusGone, "the archive is no longer on this deployment")
		return
	}
	defer file.Close()

	name := "hermes-workspace-" + uuidToString(job.WorkspaceID) + "-" + job.CreatedAt.Time.UTC().Format("20060102") + ".tar.gz"
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)

	w.Header().Set("Cache-Control", "no-store")
	if job.SizeBytes > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(job.SizeBytes, 10))
	}
	http.ServeContent(w, r, name, job.CreatedAt.Time, file)
}

func (h *Handler) loadExportJob(w http.ResponseWriter, r *http.Request) (db.ExportJob, bool) {
	workspaceID := workspaceIDFromURL(r, "id")
	requester, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return db.ExportJob{}, false
	}
	if requester.Role != "owner" {
		writeError(w, http.StatusForbidden, "only a workspace owner can read workspace exports")
		return db.ExportJob{}, false
	}
	jobUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "jobId"), "jobId")
	if !ok {
		return db.ExportJob{}, false
	}
	job, err := h.Queries.GetExportJobForWorkspace(r.Context(), db.GetExportJobForWorkspaceParams{
		ID:          jobUUID,
		WorkspaceID: requester.WorkspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "export not found")
		return db.ExportJob{}, false
	}
	if err != nil {
		slog.Error("workspace export: lookup failed", "error", err, "workspace_id", workspaceID)
		writeError(w, http.StatusInternalServerError, "failed to read the export")
		return db.ExportJob{}, false
	}
	return job, true
}

func (h *Handler) runExportJob(job db.ExportJob) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), ExportTimeout())
		defer cancel()
		if err := h.BuildExport(ctx, job); err != nil {
			slog.Error("workspace export: run failed",
				"error", err, "job_id", uuidToString(job.ID), "workspace_id", uuidToString(job.WorkspaceID))
		}
	}()
}

func (h *Handler) BuildExport(ctx context.Context, job db.ExportJob) error {
	claimed, err := h.Queries.StartExportJob(ctx, job.ID)
	if errors.Is(err, pgx.ErrNoRows) {

		return nil
	}
	if err != nil {
		return fmt.Errorf("claim: %w", err)
	}

	path, manifest, size, buildErr := h.writeExportArchive(ctx, claimed)
	if buildErr != nil {
		if path != "" {
			_ = os.Remove(path)
		}
		if _, err := h.Queries.FailExportJob(ctx, db.FailExportJobParams{
			ID:    claimed.ID,
			Error: pgtype.Text{String: truncateForColumn(buildErr.Error()), Valid: true},
		}); err != nil {
			return fmt.Errorf("record failure %v: %w", buildErr, err)
		}
		return buildErr
	}

	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}
	if _, err := h.Queries.CompleteExportJob(ctx, db.CompleteExportJobParams{
		ID:        claimed.ID,
		FilePath:  pgtype.Text{String: path, Valid: true},
		SizeBytes: size,
		Manifest:  manifestJSON,
	}); err != nil {
		return fmt.Errorf("record completion: %w", err)
	}
	slog.Info("workspace export: archive ready",
		"job_id", uuidToString(claimed.ID),
		"workspace_id", uuidToString(claimed.WorkspaceID),
		"bytes", size,
		"attachments", manifest.Attachments.Count,
		"truncated", manifest.Truncated)
	return nil
}

func (h *Handler) writeExportArchive(ctx context.Context, job db.ExportJob) (string, dataexport.Manifest, int64, error) {
	var manifest dataexport.Manifest
	dir := ExportDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", manifest, 0, fmt.Errorf("export directory %s: %w", dir, err)
	}
	final := ExportArchivePath(uuidToString(job.WorkspaceID), uuidToString(job.ID))
	partial := final + ExportPartialSuffix

	file, err := os.OpenFile(partial, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", manifest, 0, fmt.Errorf("create archive: %w", err)
	}
	manifest, writeErr := dataexport.WriteWorkspace(ctx, h.DB, h.Storage, uuidToString(job.WorkspaceID), file, dataexport.Options{
		MaxBytes: ExportMaxBytes(),
	})
	closeErr := file.Close()
	if writeErr != nil {
		_ = os.Remove(partial)
		return "", manifest, 0, writeErr
	}
	if closeErr != nil {
		_ = os.Remove(partial)
		return "", manifest, 0, fmt.Errorf("close archive: %w", closeErr)
	}
	if err := os.Rename(partial, final); err != nil {
		_ = os.Remove(partial)
		return "", manifest, 0, fmt.Errorf("publish archive: %w", err)
	}
	info, err := os.Stat(final)
	if err != nil {
		return final, manifest, 0, fmt.Errorf("stat archive: %w", err)
	}
	return final, manifest, info.Size(), nil
}

func (h *Handler) writeWorkspaceExportAudit(r *http.Request, actor pgtype.UUID, action, targetID string) {
	if _, err := h.Queries.InsertAdminAudit(r.Context(), db.InsertAdminAuditParams{
		ActorUserID: actor,
		Action:      action,
		TargetType:  "workspace",
		TargetID:    pgtype.Text{String: targetID, Valid: true},
		RequestID:   adminAuditRequestID(r),
	}); err != nil {
		slog.Error("admin audit write failed", "error", err, "action", action, "target_id", targetID)
	}
}

func truncateForColumn(s string) string {
	const max = 2000
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
