// Package export реализует `/api/workspaces/{id}/export/**` (полный архив
// данных пространства, асинхронно — job в space_export_jobs,
// 002_workspace.up.sql) и `GET /api/me/export` (синхронный поток GDPR-архива
// вызывающего). Оба используют internal/asset.Storage (T-027) для самого
// файла: contract требует 503 "не настроено файловое хранилище" на
// workspace-export, когда backend вложений не выбран — тот же Storage, тем
// же признаком "не настроено = nil", что internal/asset уже вводит.
package export

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/adanman/goosar/server2/internal/asset"
	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/store"
)

// Deps — зависимости домена export.
type Deps struct {
	Store   *Store
	Storage asset.Storage // nil — 503 на startWorkspaceExport (contract)
	Logger  *slog.Logger

	// JobTimeout — contract: "таймаут настраивается деплоем, по умолчанию 2
	// часа"; имя переменной окружения контракт не называет — решение T-029
	// в server2/docs/decisions.md: не заводить новую переменную ради
	// значения, которое проверить в песочнице всё равно нечем, дефолт 2ч
	// зашит константой ниже.
	JobTimeout time.Duration

	// Retention — сколько хранить готовый архив до "истекло хранение" (410
	// на download); contract не называет срок явно, решение T-029: 7 дней.
	Retention time.Duration
}

const (
	defaultJobTimeout = 2 * time.Hour
	defaultRetention  = 7 * 24 * time.Hour
)

// New собирает Deps; jobTimeout/retention — GOOSAR_EXPORT_TIMEOUT/
// GOOSAR_EXPORT_RETENTION (contract, «Хранение/retention»): 0 сводится к
// встроенному дефолту этого пакета, а не к "без таймаута"/"без удаления" —
// пустое значение обеих переменных контракт не документирует особо, в
// отличие от GOOSAR_RETENTION_* (см. server2/docs/decisions.md).
func New(db *store.Store, storage asset.Storage, jobTimeout, retention time.Duration, logger *slog.Logger) *Deps {
	if jobTimeout <= 0 {
		jobTimeout = defaultJobTimeout
	}
	if retention <= 0 {
		retention = defaultRetention
	}
	return &Deps{
		Store:      NewStore(db),
		Storage:    storage,
		Logger:     logger,
		JobTimeout: jobTimeout,
		Retention:  retention,
	}
}

// Register занимает /api/workspaces/{id}/export/** и GET /api/me/export.
func Register(router *httpapi.Router, deps *Deps) {
	router.Handle(http.MethodPost, "/api/workspaces/{id}/export", deps.handleStartWorkspaceExport)
	router.Handle(http.MethodGet, "/api/workspaces/{id}/export/{jobId}", deps.handleGetWorkspaceExport)
	router.Handle(http.MethodGet, "/api/workspaces/{id}/export/{jobId}/download", deps.handleDownloadWorkspaceExport)
	router.Handle(http.MethodGet, "/api/me/export", deps.handleMeExport)
}
