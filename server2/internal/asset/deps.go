package asset

import (
	"log/slog"
	"net/http"

	"github.com/adanman/goosar/server2/internal/config"
	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/realtime"
	"github.com/adanman/goosar/server2/internal/store"
	"github.com/adanman/goosar/server2/internal/wsctx"
)

// Deps — зависимости домена asset (POST /api/upload-file, /api/attachments/**,
// GET /uploads/{key}, GET /api/issues/{id}/attachments).
type Deps struct {
	Store     *Store
	Storage   Storage // nil, если ни LOCAL_UPLOAD_DIR, ни S3_BUCKET не заданы
	Resolver  *wsctx.Resolver
	Publisher realtime.Publisher
	Config    config.Config
	Logger    *slog.Logger
	db        *store.Store // прямые запросы к tickets для проверки принадлежности (см. handlers.go)
}

func New(db *store.Store, pub realtime.Publisher, cfg config.Config, logger *slog.Logger) *Deps {
	return &Deps{
		Store:     NewStore(db),
		Storage:   NewStorageFromConfig(cfg),
		Resolver:  wsctx.New(db),
		Publisher: pub,
		Config:    cfg,
		Logger:    logger,
		db:        db,
	}
}

// NewStorageFromConfig выбирает backend по переменным окружения контракта
// (docs/50-api-contract.md §1.9): LOCAL_UPLOAD_DIR — локальный диск и
// маршрут /uploads/*; иначе, если задан S3_BUCKET, — заведомо
// нереализованный S3-backend (ErrStorageNotImplemented на каждый вызов, см.
// storage.go); ни то ни другое — Storage == nil, и обработчики отвечают 503
// "хранилище не настроено", как того явно требует контракт для upload-file/
// attachment content.
func NewStorageFromConfig(cfg config.Config) Storage {
	if cfg.LocalUploadDir != "" {
		return NewLocalStorage(cfg.LocalUploadDir)
	}
	if cfg.S3Bucket != "" {
		return NewS3Storage()
	}
	return nil
}

// Register регистрирует операции тега Attachments + upload-file + список
// вложений задачи. GET /uploads/{key} — единственный путь, который контракт
// (docs/50-api-contract.yaml) описывает буквально этой строкой, и его этот
// домен занимает всегда (не только когда backend — локальный диск):
// handleServeLocalUpload уже отвечает 404, когда Storage не *LocalStorage —
// раньше при отсутствии LOCAL_UPLOAD_DIR путь не занимался этим доменом
// вовсе и падал в genstubs-заглушку 501, что расходилось с собственным
// комментарием этого домена ("иначе 404, не ошибка конфигурации"); это и
// есть T-027 доводка для этого маршрута.
//
// Настоящий объектный ключ (newObjectKey) — "scope/имя-файла", то есть
// содержит "/": одного "{key}" (один сегмент, ровно то, что называет
// контракт) для него недостаточно — net/http's ServeMux сопоставляет "{key}"
// только одному сегменту пути. Поэтому когда backend локальный, домен
// дополнительно занимает "{key...}" (хвост из нескольких сегментов) той же
// ручкой; это не входит в TestRouteCoverageMatchesContract (internal/app),
// который строит роутер без сконфигурированного хранилища и потому никогда
// не видит этот второй путь — тот же компромисс, что был в исходном коде до
// этой правки, задокументирован в server2/docs/decisions.md.
func Register(router *httpapi.Router, deps *Deps) {
	table := [...]struct {
		method  string
		pattern string
		handler http.HandlerFunc
	}{
		{http.MethodPost, "/api/upload-file", deps.handleUploadFile},
		{http.MethodGet, "/api/attachments/{id}", deps.handleGetAttachment},
		{http.MethodDelete, "/api/attachments/{id}", deps.handleDeleteAttachment},
		{http.MethodGet, "/api/attachments/{id}/content", deps.handleAttachmentContent},
		{http.MethodGet, "/api/attachments/{id}/download", deps.handleAttachmentDownload},
		{http.MethodGet, "/api/issues/{id}/attachments", deps.handleListIssueAttachments},
		{http.MethodGet, "/uploads/{key}", deps.handleServeLocalUpload},
	}
	for _, rt := range table {
		router.Handle(rt.method, rt.pattern, rt.handler)
	}
	if _, isLocal := deps.Storage.(*LocalStorage); isLocal {
		router.Handle(http.MethodGet, "/uploads/{key...}", deps.handleServeLocalUpload)
	}
}
