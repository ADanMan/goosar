package asset

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/adanman/goosar/server2/internal/httpapi"
	"github.com/adanman/goosar/server2/internal/realtime"
)

// maxUploadBytes — предел тела multipart-запроса (контракт: "100 MiB cap").
const maxUploadBytes = 100 << 20

// inlinePreviewLimit — лимит инлайн-превью getAttachmentContent; контракт не
// фиксирует точное число, только "малого размера" — 256 КБ разумный предел
// для текстоподобных файлов (see server2/docs/decisions.md, пробел спецификации).
const inlinePreviewLimit = 256 << 10

func (d *Deps) handleUploadFile(w http.ResponseWriter, r *http.Request) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return
	}
	if d.Storage == nil {
		httpapi.WriteJSON(w, http.StatusServiceUnavailable, httpapi.Error{Error: "no storage backend configured", Code: "storage_not_configured"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		httpapi.BadRequest(w, "file too large or invalid multipart form (100 MiB cap)")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		httpapi.BadRequest(w, "missing file field")
		return
	}
	defer file.Close()

	// Воркспейс опционален (см. docs/50-api-contract.md §1.4): без него —
	// персональная загрузка под users/{user_id}, с ним — обязательное членство.
	scope := "users/" + actor.UserID
	var workspaceID *string
	if member, ok := d.optionalWorkspace(w, r); !ok {
		return
	} else if member != nil {
		scope = "workspaces/" + member.WorkspaceID
		workspaceID = &member.WorkspaceID
	}

	uploaderType := "member"
	if !actor.IsHuman {
		uploaderType = "agent"
	}

	contentType, body, err := sniffContentType(file, header.Filename)
	if err != nil {
		httpapi.BadRequest(w, "could not read uploaded file")
		return
	}

	stored, err := d.Storage.Save(r.Context(), scope, header.Filename, body)
	if err != nil {
		d.Logger.Error("asset: сохранение файла", "err", err)
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}

	req := parseUploadLinks(r)
	if workspaceID == nil {
		// Без контекста воркспейса вложение не может ссылаться на сущности
		// внутри пространства (issue/comment/chat — все они принадлежат
		// какому-то workspace_id NOT NULL в assets); контракт этого не
		// оговаривает явно — решение см. server2/docs/decisions.md.
		req = uploadLinks{}
	}
	wsID := ""
	if workspaceID != nil {
		wsID = *workspaceID
	}
	att, err := d.Store.Create(r.Context(), CreateParams{
		WorkspaceID:   wsID,
		IssueID:       req.issueID,
		CommentID:     req.commentID,
		ChatSessionID: req.chatSessionID,
		ChatMessageID: req.chatMessageID,
		UploaderType:  uploaderType,
		UploaderID:    actor.UserID,
		Filename:      header.Filename,
		StorageURL:    stored.URL,
		ContentType:   contentType,
		SizeBytes:     header.Size,
	})
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, att)
}

type uploadLinks struct {
	issueID       *string
	commentID     *string
	chatSessionID *string
	chatMessageID *string
}

func parseUploadLinks(r *http.Request) uploadLinks {
	get := func(key string) *string {
		v := strings.TrimSpace(r.FormValue(key))
		if v == "" {
			return nil
		}
		return &v
	}
	return uploadLinks{
		issueID:       get("issue_id"),
		commentID:     get("comment_id"),
		chatSessionID: get("chat_session_id"),
		chatMessageID: get("chat_message_id"),
	}
}

// sniffContentType определяет MIME по расширению (приоритет — контракт:
// "content type определяется по сигнатуре+расширению"), а при неизвестном
// расширении — по сигнатуре первых 512 байт; возвращает содержимое файла
// целиком заново собранным в один Reader (буферизованный префикс уже прочитан).
func sniffContentType(file io.Reader, filename string) (string, io.Reader, error) {
	head := make([]byte, 512)
	n, err := io.ReadFull(file, head)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return "", nil, err
	}
	head = head[:n]
	full := io.MultiReader(bytes.NewReader(head), file)

	if ct := mime.TypeByExtension(filepath.Ext(filename)); ct != "" {
		return ct, full, nil
	}
	return http.DetectContentType(head), full, nil
}

// optionalWorkspace — как wsctx.Resolver.RequireMember, но отсутствие
// контекста воркспейса не ошибка (см. §1.4): возвращает (nil, true), если
// заголовок/query не заданы вовсе.
func (d *Deps) optionalWorkspace(w http.ResponseWriter, r *http.Request) (*memberRef, bool) {
	actor, _ := httpapi.ActorFrom(r.Context())
	if _, _, ok := httpapi.ResolveWorkspaceRef(r, actor); !ok {
		return nil, true
	}
	m, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return nil, false
	}
	return &memberRef{WorkspaceID: m.WorkspaceID}, true
}

type memberRef struct{ WorkspaceID string }

// --- /api/attachments/{id} ---------------------------------------------------

func (d *Deps) requireAttachmentAccess(w http.ResponseWriter, r *http.Request) (Attachment, bool) {
	actor, ok := httpapi.RequireActor(w, r)
	if !ok {
		return Attachment{}, false
	}
	id := r.PathValue("id")
	att, err := d.Store.GetByID(r.Context(), id)
	if errors.Is(err, ErrNotFound) {
		httpapi.NotFound(w, "attachment not found")
		return Attachment{}, false
	}
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return Attachment{}, false
	}
	if _, err := d.Resolver.MemberOf(r.Context(), att.WorkspaceID, actor.UserID); err != nil {
		httpapi.NotFound(w, "attachment not found")
		return Attachment{}, false
	}
	return att, true
}

func (d *Deps) handleGetAttachment(w http.ResponseWriter, r *http.Request) {
	att, ok := d.requireAttachmentAccess(w, r)
	if !ok {
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, att)
}

func (d *Deps) handleDeleteAttachment(w http.ResponseWriter, r *http.Request) {
	att, ok := d.requireAttachmentAccess(w, r)
	if !ok {
		return
	}
	if err := d.Store.Delete(r.Context(), att.WorkspaceID, att.ID); err != nil && !errors.Is(err, ErrNotFound) {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if d.Storage != nil && att.StorageKey != "" {
		if err := d.Storage.Delete(r.Context(), att.StorageKey); err != nil {
			d.Logger.Warn("asset: удаление файла из хранилища", "err", err, "attachment_id", att.ID)
		}
	}
	if d.Publisher != nil {
		d.Publisher.Publish(att.WorkspaceID, realtime.Event{Type: "attachment.deleted", Payload: map[string]string{"id": att.ID}})
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d *Deps) handleAttachmentContent(w http.ResponseWriter, r *http.Request) {
	att, ok := d.requireAttachmentAccess(w, r)
	if !ok {
		return
	}
	if d.Storage == nil {
		httpapi.WriteError(w, http.StatusServiceUnavailable, "object storage is not configured", "storage_not_configured")
		return
	}
	if !isTextLike(att.ContentType) {
		httpapi.WriteError(w, http.StatusUnsupportedMediaType, "file type does not support text preview", "unsupported_preview")
		return
	}
	if att.SizeBytes > inlinePreviewLimit {
		httpapi.WriteError(w, http.StatusRequestEntityTooLarge, "file exceeds the inline preview limit for its type", "preview_too_large")
		return
	}
	f, _, err := d.Storage.Open(r.Context(), att.StorageKey)
	if err != nil {
		httpapi.NotFound(w, "attachment not found")
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.Copy(w, io.LimitReader(f, inlinePreviewLimit))
}

func isTextLike(contentType string) bool {
	ct := strings.ToLower(contentType)
	if strings.HasPrefix(ct, "text/") {
		return true
	}
	switch ct {
	case "application/json", "application/xml", "application/yaml", "application/x-yaml",
		"application/javascript", "application/toml":
		return true
	}
	return false
}

func (d *Deps) handleAttachmentDownload(w http.ResponseWriter, r *http.Request) {
	// Токен-авторизация (одноразовый подписанный ?token=) — вне объёма этой
	// сессии: контракт описывает его как альтернативу сессии, но не задаёт
	// формат подписи; здесь принимается только обычная сессия/PAT + членство
	// (см. server2/docs/decisions.md, раздел T-027, «attachment download ticket»).
	att, ok := d.requireAttachmentAccess(w, r)
	if !ok {
		return
	}
	if d.Storage == nil {
		httpapi.WriteError(w, http.StatusServiceUnavailable, "object storage is not configured", "storage_not_configured")
		return
	}
	f, size, err := d.Storage.Open(r.Context(), att.StorageKey)
	if err != nil {
		httpapi.NotFound(w, "attachment not found")
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", att.ContentType)
	w.Header().Set("Content-Disposition", "attachment; filename=\""+att.Filename+"\"")
	http.ServeContent(w, r, att.Filename, att.CreatedAt, sizedReadSeeker{f, size})
}

// sizedReadSeeker адаптирует io.ReadCloser (файл на диске уже реализует
// io.ReadSeeker сам по себе — os.File) под http.ServeContent, которому нужен
// io.ReadSeeker; для локального диска f уже *os.File, поэтому приведение
// работает без промежуточного буфера.
type sizedReadSeeker struct {
	io.ReadCloser
	size int64
}

func (s sizedReadSeeker) Seek(offset int64, whence int) (int64, error) {
	seeker, ok := s.ReadCloser.(io.Seeker)
	if !ok {
		return 0, errors.New("asset: хранилище не поддерживает Range для этого объекта")
	}
	return seeker.Seek(offset, whence)
}

// handleServeLocalUpload — GET /uploads/{key}, монтируется только когда
// backend локальный (см. register.go).
func (d *Deps) handleServeLocalUpload(w http.ResponseWriter, r *http.Request) {
	local, ok := d.Storage.(*LocalStorage)
	if !ok {
		httpapi.NotFound(w, "not found")
		return
	}
	key := r.PathValue("key")
	f, size, err := local.Open(r.Context(), key)
	if err != nil {
		httpapi.NotFound(w, "not found")
		return
	}
	defer f.Close()
	http.ServeContent(w, r, key, time.Time{}, sizedReadSeeker{f, size})
}

func (d *Deps) handleListIssueAttachments(w http.ResponseWriter, r *http.Request) {
	member, ok := d.Resolver.RequireMember(w, r)
	if !ok {
		return
	}
	issueID := r.PathValue("id")
	exists, err := d.ticketExists(r.Context(), member.WorkspaceID, issueID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if !exists {
		httpapi.NotFound(w, "issue not found")
		return
	}
	list, err := d.Store.ListForIssue(r.Context(), member.WorkspaceID, issueID)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if list == nil {
		list = []Attachment{}
	}
	httpapi.WriteJSON(w, http.StatusOK, list)
}

func (d *Deps) ticketExists(ctx context.Context, workspaceID, ticketID string) (bool, error) {
	return d.db.RowExists(ctx, `SELECT EXISTS(SELECT 1 FROM tickets WHERE workspace_id = $1 AND id = $2)`,
		workspaceID, ticketID)
}
