// Package asset реализует POST /api/upload-file и /api/attachments/**
// (+ GET /api/issues/{id}/attachments, GET /uploads/{key} для локального
// хранилища) — таблица assets, 009_feed.up.sql.
package asset

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/adanman/goosar/server2/internal/store"
)

var ErrNotFound = errors.New("asset: вложение не найдено")

// Attachment — форма components/schemas/Attachment.
type Attachment struct {
	ID                string    `json:"id"`
	WorkspaceID       string    `json:"workspace_id"`
	IssueID           *string   `json:"issue_id"`
	CommentID         *string   `json:"comment_id"`
	ChatSessionID     *string   `json:"chat_session_id"`
	ChatMessageID     *string   `json:"chat_message_id"`
	UploaderType      string    `json:"uploader_type"`
	UploaderID        string    `json:"uploader_id"`
	Filename          string    `json:"filename"`
	URL               string    `json:"url"`
	DownloadURL       string    `json:"download_url"`
	MarkdownURL       string    `json:"markdown_url"`
	DownloadTicketURL *string   `json:"download_ticket_url,omitempty"`
	ContentType       string    `json:"content_type"`
	SizeBytes         int64     `json:"size_bytes"`
	CreatedAt         time.Time `json:"created_at"`

	// StorageKey — не часть ответа API (json:"-"): ключ в Storage, нужен
	// внутри домена, чтобы открыть/удалить сам файл.
	StorageKey string `json:"-"`
}

const attachmentColumns = `id, workspace_id, ticket_id, ticket_note_id, convo_id, convo_message_id,
	as_uploader_type, as_uploader_id, as_filename, as_storage_uri, as_download_path, as_markdown_ref,
	as_download_ticket_uri, as_content_type, as_size_bytes, created_at`

func scanAttachment(row pgx.Row) (Attachment, error) {
	var a Attachment
	var storageURI string
	if err := row.Scan(&a.ID, &a.WorkspaceID, &a.IssueID, &a.CommentID, &a.ChatSessionID, &a.ChatMessageID,
		&a.UploaderType, &a.UploaderID, &a.Filename, &storageURI, &a.DownloadURL, &a.MarkdownURL,
		&a.DownloadTicketURL, &a.ContentType, &a.SizeBytes, &a.CreatedAt); err != nil {
		return Attachment{}, err
	}
	a.URL = storageURI
	a.StorageKey = storageKeyFromURL(storageURI)
	return a, nil
}

// storageKeyFromURL извлекает ключ Storage из as_storage_uri, записанного в
// формате "local://<key>" (см. LocalStorage.Save); для чужого/будущего вида
// URL (S3 и т.п.) возвращает пусто — Open/Delete на таком объекте не пытаются.
func storageKeyFromURL(u string) string {
	const prefix = "local://"
	if len(u) > len(prefix) && u[:len(prefix)] == prefix {
		return u[len(prefix):]
	}
	return ""
}

// CreateParams — вход Create. DownloadPath/MarkdownURL не входят сюда: они
// ссылаются на собственный id вложения (/api/attachments/{id}/download),
// который известен только после INSERT — Create сама делает второй,
// уточняющий UPDATE, вызывающему думать об этом не нужно.
type CreateParams struct {
	WorkspaceID   string
	IssueID       *string
	CommentID     *string
	ChatSessionID *string
	ChatMessageID *string
	UploaderType  string
	UploaderID    string
	Filename      string
	StorageURL    string
	ContentType   string
	SizeBytes     int64
}

type Store struct{ db *store.Store }

func NewStore(db *store.Store) *Store { return &Store{db: db} }

func (s *Store) Create(ctx context.Context, p CreateParams) (Attachment, error) {
	row := s.db.Pool.QueryRow(ctx, `
		INSERT INTO assets (workspace_id, ticket_id, ticket_note_id, convo_id, convo_message_id,
			as_uploader_type, as_uploader_id, as_filename, as_storage_uri, as_download_path, as_markdown_ref,
			as_content_type, as_size_bytes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, '', '', $10, $11)
		RETURNING `+attachmentColumns,
		p.WorkspaceID, p.IssueID, p.CommentID, p.ChatSessionID, p.ChatMessageID,
		p.UploaderType, p.UploaderID, p.Filename, p.StorageURL,
		p.ContentType, p.SizeBytes)
	a, err := scanAttachment(row)
	if err != nil {
		return Attachment{}, fmt.Errorf("asset: создание вложения: %w", err)
	}
	downloadPath := "/api/attachments/" + a.ID + "/download"
	markdown := fmt.Sprintf("[%s](%s)", a.Filename, downloadPath)
	return s.updateDownloadPath(ctx, a.ID, downloadPath, markdown)
}

// updateDownloadPath заполняет as_download_path/as_markdown_ref, зависящие
// от собственного id строки (см. Create).
func (s *Store) updateDownloadPath(ctx context.Context, id, downloadPath, markdown string) (Attachment, error) {
	row := s.db.Pool.QueryRow(ctx, `
		UPDATE assets SET as_download_path = $2, as_markdown_ref = $3 WHERE id = $1
		RETURNING `+attachmentColumns, id, downloadPath, markdown)
	a, err := scanAttachment(row)
	if err != nil {
		return Attachment{}, fmt.Errorf("asset: обновление пути скачивания: %w", err)
	}
	return a, nil
}

func (s *Store) Get(ctx context.Context, workspaceID, id string) (Attachment, error) {
	row := s.db.Pool.QueryRow(ctx, `SELECT `+attachmentColumns+` FROM assets WHERE workspace_id = $1 AND id = $2`,
		workspaceID, id)
	a, err := scanAttachment(row)
	if store.IsNoRows(err) {
		return Attachment{}, ErrNotFound
	}
	if err != nil {
		return Attachment{}, fmt.Errorf("asset: получение вложения: %w", err)
	}
	return a, nil
}

// GetByWorkspaceOnly — как Get, но перед проверкой членства нужно узнать
// сам workspace_id вложения (для авторизации по held download-ticket, где
// пространство ещё не известно вызывающему).
func (s *Store) GetByID(ctx context.Context, id string) (Attachment, error) {
	row := s.db.Pool.QueryRow(ctx, `SELECT `+attachmentColumns+` FROM assets WHERE id = $1`, id)
	a, err := scanAttachment(row)
	if store.IsNoRows(err) {
		return Attachment{}, ErrNotFound
	}
	if err != nil {
		return Attachment{}, fmt.Errorf("asset: получение вложения по id: %w", err)
	}
	return a, nil
}

func (s *Store) Delete(ctx context.Context, workspaceID, id string) error {
	tag, err := s.db.Pool.Exec(ctx, `DELETE FROM assets WHERE workspace_id = $1 AND id = $2`, workspaceID, id)
	if err != nil {
		return fmt.Errorf("asset: удаление вложения: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListForIssue — вложения, прикреплённые непосредственно к задаче (не к её
// комментариям): GET /api/issues/{id}/attachments.
func (s *Store) ListForIssue(ctx context.Context, workspaceID, issueID string) ([]Attachment, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT `+attachmentColumns+` FROM assets
		WHERE workspace_id = $1 AND ticket_id = $2 AND ticket_note_id IS NULL
		ORDER BY created_at`, workspaceID, issueID)
	if err != nil {
		return nil, fmt.Errorf("asset: список вложений задачи: %w", err)
	}
	defer rows.Close()
	return scanAttachments(rows)
}

// ListForComment — вложения одного комментария, для встраивания в
// IssueComment.attachments.
func (s *Store) ListForComment(ctx context.Context, commentID string) ([]Attachment, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT `+attachmentColumns+` FROM assets WHERE ticket_note_id = $1 ORDER BY created_at`, commentID)
	if err != nil {
		return nil, fmt.Errorf("asset: список вложений комментария: %w", err)
	}
	defer rows.Close()
	return scanAttachments(rows)
}

// ListForComments — то же самое сразу для набора комментариев (листинг
// треда), сгруппированное по comment_id — один запрос вместо N.
func (s *Store) ListForComments(ctx context.Context, commentIDs []string) (map[string][]Attachment, error) {
	out := map[string][]Attachment{}
	if len(commentIDs) == 0 {
		return out, nil
	}
	rows, err := s.db.Pool.Query(ctx, `
		SELECT `+attachmentColumns+` FROM assets WHERE ticket_note_id = ANY($1) ORDER BY created_at`, commentIDs)
	if err != nil {
		return nil, fmt.Errorf("asset: список вложений тредов: %w", err)
	}
	defer rows.Close()
	list, err := scanAttachments(rows)
	if err != nil {
		return nil, err
	}
	for _, a := range list {
		if a.CommentID != nil {
			out[*a.CommentID] = append(out[*a.CommentID], a)
		}
	}
	return out, nil
}

func scanAttachments(rows pgx.Rows) ([]Attachment, error) {
	var out []Attachment
	for rows.Next() {
		a, err := scanAttachment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// AttachToComment переприкрепляет набор ранее загруженных вложений (по id) к
// commentID, полностью заменяя прежний набор вложений этого комментария —
// ровно семантика attachment_ids в CreateCommentRequest/UpdateCommentRequest
// ("если ключ присутствует — полностью заменяет"). Вложения, не входящие в
// ids, но принадлежавшие комментарию, отвязываются (не удаляются как файлы:
// они просто перестают быть вложениями этого комментария; сам файл остаётся
// адресуем по id, пока не будет удалён явно через DELETE /api/attachments/{id}).
func (s *Store) AttachToComment(ctx context.Context, workspaceID, ticketID, commentID string, ids []string) error {
	return s.db.WithTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE assets SET ticket_note_id = NULL
			WHERE workspace_id = $1 AND ticket_note_id = $2`, workspaceID, commentID); err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		tag, err := tx.Exec(ctx, `UPDATE assets SET ticket_note_id = $3, ticket_id = COALESCE(ticket_id, $4)
			WHERE workspace_id = $1 AND id = ANY($2)`, workspaceID, ids, commentID, ticketID)
		if err != nil {
			return err
		}
		if int(tag.RowsAffected()) != len(dedup(ids)) {
			return fmt.Errorf("asset: часть attachment_ids не найдена в этом воркспейсе")
		}
		return nil
	})
}

func dedup(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	out := ids[:0:0]
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// --- хранилище байтов вложений -----------------------------------------------

// ErrStorageNotConfigured — ни LOCAL_UPLOAD_DIR, ни S3_BUCKET не заданы:
// контракт требует 503 "no storage backend configured" в этом случае
// (POST /api/upload-file, GET /api/attachments/{id}/content).
var ErrStorageNotConfigured = errors.New("asset: хранилище вложений не настроено")

// ErrStorageNotImplemented — выбран (по наличию S3_BUCKET) S3-backend, но он
// не реализован в server2 (см. server2/docs/decisions.md, раздел T-027):
// сознательное решение не тянуть AWS SDK ради интерфейса, которым в этой
// сессии некому воспользоваться (в контракте нет отдельного эндпоинта,
// который требовал бы именно S3 для прохождения контрактных тестов).
var ErrStorageNotImplemented = errors.New("asset: S3-backend хранилища вложений ещё не реализован")

// StoredObject — результат успешного сохранения файла в Storage.
type StoredObject struct {
	Key          string // ключ хранилища, например "workspaces/{id}/{случайный суффикс}-{filename}"
	URL          string // внутренний URL хранилища (Attachment.url / as_storage_uri)
	DownloadPath string // путь/URL для скачивания (Attachment.download_url / as_download_path)
}

// Storage — то, куда server2 кладёт байты вложений. Реализации: LocalStorage
// (диск, ниже) и заглушка s3Storage — понятная ошибка конфигурации
// (ErrStorageNotImplemented), а не молчаливый провал, по прямому указанию
// задачи T-027.
type Storage interface {
	// Save сохраняет content под новым ключом внутри scope (обычно
	// "workspaces/{id}" либо "users/{id}") с исходным именем filename.
	Save(ctx context.Context, scope, filename string, content io.Reader) (StoredObject, error)
	// Open открывает ранее сохранённый объект для чтения (инлайн-превью,
	// проксирующая раздача) вместе с его размером в байтах.
	Open(ctx context.Context, key string) (io.ReadCloser, int64, error)
	// Delete удаляет объект по ключу; отсутствие объекта — не ошибка.
	Delete(ctx context.Context, key string) error
}

// LocalStorage кладёт файлы под root на диске; ключ объекта — тот же путь,
// каким его потом отдаёт GET /uploads/{key} (см. docs/50-api-contract.yaml,
// маршрут монтируется только при этом backend'е).
type LocalStorage struct {
	root string
}

func NewLocalStorage(root string) *LocalStorage { return &LocalStorage{root: root} }

// fullPath переводит ключ хранилища в абсолютный путь на диске, отклоняя
// всё, что после лексического разбора могло бы выйти за пределы root
// (filepath.IsLocal — проверка "путь не убегает наружу через .. или
// абсолютные сегменты", появившаяся в Go 1.20 специально для этого класса
// задач).
func (s *LocalStorage) fullPath(key string) (string, error) {
	if key == "" || !filepath.IsLocal(filepath.FromSlash(key)) {
		return "", fmt.Errorf("asset: недопустимый ключ хранилища: %q", key)
	}
	return filepath.Join(s.root, filepath.FromSlash(key)), nil
}

func newObjectKey(scope, filename string) string {
	suffix := make([]byte, 16)
	_, _ = rand.Read(suffix)
	return path.Join(scope, hex.EncodeToString(suffix)+"-"+sanitizeFilename(filename))
}

// Save пишет во временный файл рядом с конечным местом и переименовывает
// его на конечный путь последним шагом: rename на одной файловой системе —
// атомарная операция ОС, так что параллельный Open той же ключевой строки
// никогда не увидит частично записанный файл.
func (s *LocalStorage) Save(_ context.Context, scope, filename string, content io.Reader) (StoredObject, error) {
	key := newObjectKey(scope, filename)
	dest, err := s.fullPath(key)
	if err != nil {
		return StoredObject{}, err
	}
	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return StoredObject{}, fmt.Errorf("asset: подготовка каталога вложений: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		return StoredObject{}, fmt.Errorf("asset: создание временного файла вложения: %w", err)
	}
	defer os.Remove(tmp.Name())

	if _, err := io.Copy(tmp, content); err != nil {
		tmp.Close()
		return StoredObject{}, fmt.Errorf("asset: запись файла вложения: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return StoredObject{}, fmt.Errorf("asset: сохранение файла вложения: %w", err)
	}
	if err := os.Rename(tmp.Name(), dest); err != nil {
		return StoredObject{}, fmt.Errorf("asset: перемещение файла вложения на место: %w", err)
	}
	return StoredObject{Key: key, URL: "local://" + key, DownloadPath: "/uploads/" + key}, nil
}

func (s *LocalStorage) Open(_ context.Context, key string) (io.ReadCloser, int64, error) {
	dest, err := s.fullPath(key)
	if err != nil {
		return nil, 0, err
	}
	f, err := os.Open(dest)
	if err != nil {
		return nil, 0, err
	}
	info, statErr := f.Stat()
	if statErr != nil {
		_ = f.Close()
		return nil, 0, statErr
	}
	return f, info.Size(), nil
}

func (s *LocalStorage) Delete(_ context.Context, key string) error {
	dest, err := s.fullPath(key)
	if err != nil {
		return err
	}
	switch err := os.Remove(dest); {
	case err == nil, os.IsNotExist(err):
		return nil
	default:
		return fmt.Errorf("asset: удаление файла вложения: %w", err)
	}
}

func sanitizeFilename(name string) string {
	base := filepath.Base(strings.TrimSpace(name))
	if base == "" || base == "." || base == string(filepath.Separator) {
		return "file"
	}
	return base
}

// s3Storage — заглушка ещё не выбранного backend'а: любой вызов явно
// отказывает ErrStorageNotImplemented, чтобы обработчик мог вернуть 501 с
// понятным сообщением вместо того, чтобы притворяться работающим хранилищем.
type s3Storage struct{}

func NewS3Storage() Storage { return s3Storage{} }

func (s3Storage) Save(context.Context, string, string, io.Reader) (StoredObject, error) {
	return StoredObject{}, ErrStorageNotImplemented
}

func (s3Storage) Open(context.Context, string) (io.ReadCloser, int64, error) {
	return nil, 0, ErrStorageNotImplemented
}

func (s3Storage) Delete(context.Context, string) error { return ErrStorageNotImplemented }
