package handler

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/auth"
	"github.com/adanman/goosar/server/internal/middleware"
	"github.com/adanman/goosar/server/internal/storage"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

var extContentTypes = map[string]string{
	".svg":  "image/svg+xml",
	".css":  "text/css",
	".js":   "application/javascript",
	".mjs":  "application/javascript",
	".json": "application/json",
	".wasm": "application/wasm",

	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	".doc":  "application/msword",
	".xls":  "application/vnd.ms-excel",
	".ppt":  "application/vnd.ms-powerpoint",
	".odt":  "application/vnd.oasis.opendocument.text",
	".ods":  "application/vnd.oasis.opendocument.spreadsheet",
	".odp":  "application/vnd.oasis.opendocument.presentation",
	".rtf":  "application/rtf",
	".epub": "application/epub+zip",

	".html":     "text/html; charset=utf-8",
	".htm":      "text/html; charset=utf-8",
	".csv":      "text/csv; charset=utf-8",
	".tsv":      "text/tab-separated-values; charset=utf-8",
	".md":       "text/markdown; charset=utf-8",
	".markdown": "text/markdown; charset=utf-8",
	".yaml":     "application/yaml",
	".yml":      "application/yaml",
	".toml":     "application/toml",
}

var genericSniffedContentTypes = map[string]struct{}{
	"":                         {},
	"application/octet-stream": {},
	"application/zip":          {},
	"text/plain":               {},
}

func resolveAttachmentContentType(stored, filename string) string {
	normalized := strings.ToLower(strings.TrimSpace(stored))
	if idx := strings.IndexByte(normalized, ';'); idx >= 0 {
		normalized = strings.TrimSpace(normalized[:idx])
	}
	if _, generic := genericSniffedContentTypes[normalized]; generic {
		if byExt, ok := extContentTypes[strings.ToLower(path.Ext(filename))]; ok {
			return byExt
		}
	}
	if stored == "" {
		return "application/octet-stream"
	}
	return stored
}

const maxUploadSize = 100 << 20

const defaultAttachmentDownloadURLTTL = 30 * time.Minute

type attachmentDownloadMode string

const (
	attachmentDownloadModeAuto       attachmentDownloadMode = "auto"
	attachmentDownloadModeCloudFront attachmentDownloadMode = "cloudfront"
	attachmentDownloadModePresign    attachmentDownloadMode = "presign"
	attachmentDownloadModeProxy      attachmentDownloadMode = "proxy"
)

const (
	maxPreviewHTMLSize = 16 << 20

	maxPreviewTextSize = 8 << 20
)

func maxPreviewSizeFor(contentType, filename string) int64 {
	resolved := strings.ToLower(resolveAttachmentContentType(contentType, filename))
	if strings.HasPrefix(resolved, "text/html") {
		return maxPreviewHTMLSize
	}
	return maxPreviewTextSize
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for size := n / unit; size >= unit; size /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

type AttachmentResponse struct {
	ID            string  `json:"id"`
	WorkspaceID   string  `json:"workspace_id"`
	IssueID       *string `json:"issue_id"`
	CommentID     *string `json:"comment_id"`
	ChatSessionID *string `json:"chat_session_id"`
	ChatMessageID *string `json:"chat_message_id"`
	UploaderType  string  `json:"uploader_type"`
	UploaderID    string  `json:"uploader_id"`
	Filename      string  `json:"filename"`
	URL           string  `json:"url"`
	DownloadURL   string  `json:"download_url"`

	MarkdownURL string `json:"markdown_url"`

	DownloadTicketURL string `json:"download_ticket_url,omitempty"`
	ContentType       string `json:"content_type"`
	SizeBytes         int64  `json:"size_bytes"`
	CreatedAt         string `json:"created_at"`
}

func (h *Handler) attachmentToResponse(a db.Attachment) AttachmentResponse {
	id := uuidToString(a.ID)
	resp := AttachmentResponse{
		ID:           id,
		WorkspaceID:  uuidToString(a.WorkspaceID),
		UploaderType: a.UploaderType,
		UploaderID:   uuidToString(a.UploaderID),
		Filename:     a.Filename,
		URL:          a.Url,
		DownloadURL:  attachmentDownloadPath(id),
		MarkdownURL:  h.buildMarkdownURL(a, id),
		ContentType:  a.ContentType,
		SizeBytes:    a.SizeBytes,
		CreatedAt:    a.CreatedAt.Time.Format("2006-01-02T15:04:05Z07:00"),
	}
	if h.CFSigner != nil {
		resp.DownloadURL = h.CFSigner.SignedURL(a.Url, time.Now().Add(h.attachmentDownloadURLTTL()))
	}
	if a.IssueID.Valid {
		s := uuidToString(a.IssueID)
		resp.IssueID = &s
	}
	if a.CommentID.Valid {
		s := uuidToString(a.CommentID)
		resp.CommentID = &s
	}
	if a.ChatSessionID.Valid {
		s := uuidToString(a.ChatSessionID)
		resp.ChatSessionID = &s
	}
	if a.ChatMessageID.Valid {
		s := uuidToString(a.ChatMessageID)
		resp.ChatMessageID = &s
	}
	return resp
}

func attachmentDownloadPath(id string) string {
	return "/api/attachments/" + id + "/download"
}

func (h *Handler) attachmentDownloadTicketURL(attachmentID, userID string) (string, error) {
	ticket, err := auth.SignAttachmentDownloadTicket(
		attachmentID,
		userID,
		time.Now().Add(h.attachmentDownloadURLTTL()),
	)
	if err != nil {
		return "", err
	}
	query := url.Values{middleware.AttachmentDownloadTicketParam: []string{ticket}}
	return attachmentDownloadPath(attachmentID) + "?" + query.Encode(), nil
}

func (h *Handler) buildMarkdownURL(a db.Attachment, id string) string {
	relPath := attachmentDownloadPath(id)
	publicURL := strings.TrimRight(h.cfg.PublicURL, "/")

	if h.storageURLIsPubliclyReadable(a.Url) {
		return a.Url
	}

	if publicURL != "" {
		return publicURL + relPath
	}
	return relPath
}

func (h *Handler) storageURLIsPubliclyReadable(rawURL string) bool {
	if h.Storage == nil || h.CFSigner != nil {

		return false
	}
	if h.Storage.CdnDomain() == "" {

		return false
	}
	return isDurablePublicURL(rawURL)
}

func isDurablePublicURL(rawURL string) bool {
	if rawURL == "" {
		return false
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	if u.Host == "" {
		return false
	}
	q := u.Query()
	for _, k := range []string{
		"Signature",
		"X-Amz-Signature",
		"Key-Pair-Id",
		"Expires",
		"X-Amz-Expires",
	} {
		if q.Get(k) != "" {
			return false
		}
	}
	return true
}

func normalizeAttachmentDownloadMode(raw string) (attachmentDownloadMode, bool) {
	switch attachmentDownloadMode(strings.ToLower(strings.TrimSpace(raw))) {
	case "", attachmentDownloadModeAuto:
		return attachmentDownloadModeAuto, true
	case attachmentDownloadModeCloudFront:
		return attachmentDownloadModeCloudFront, true
	case attachmentDownloadModePresign:
		return attachmentDownloadModePresign, true
	case attachmentDownloadModeProxy:
		return attachmentDownloadModeProxy, true
	default:
		return attachmentDownloadModeAuto, false
	}
}

func (h *Handler) attachmentDownloadMode() attachmentDownloadMode {
	mode, _ := normalizeAttachmentDownloadMode(h.cfg.AttachmentDownloadMode)
	return mode
}

func (h *Handler) attachmentDownloadURLTTL() time.Duration {
	if h.cfg.AttachmentDownloadURLTTL > 0 {
		return h.cfg.AttachmentDownloadURLTTL
	}
	return defaultAttachmentDownloadURLTTL
}

func (h *Handler) groupAttachments(r *http.Request, commentIDs []pgtype.UUID) map[string][]AttachmentResponse {
	if len(commentIDs) == 0 {
		return nil
	}
	workspaceID := h.resolveWorkspaceID(r)
	attachments, err := h.Queries.ListAttachmentsByCommentIDs(r.Context(), db.ListAttachmentsByCommentIDsParams{
		Column1:     commentIDs,
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		slog.Error("failed to load attachments for comments", "error", err)
		return nil
	}
	grouped := make(map[string][]AttachmentResponse, len(commentIDs))
	for _, a := range attachments {
		cid := uuidToString(a.CommentID)
		grouped[cid] = append(grouped[cid], h.attachmentToResponse(a))
	}
	return grouped
}

func (h *Handler) groupChatMessageAttachments(ctx context.Context, workspaceID string, messageIDs []pgtype.UUID) map[string][]AttachmentResponse {
	if len(messageIDs) == 0 {
		return nil
	}
	attachments, err := h.Queries.ListAttachmentsByChatMessageIDs(ctx, db.ListAttachmentsByChatMessageIDsParams{
		Column1:     messageIDs,
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		slog.Error("failed to load attachments for chat messages", "error", err)
		return nil
	}
	grouped := make(map[string][]AttachmentResponse, len(messageIDs))
	for _, a := range attachments {
		mid := uuidToString(a.ChatMessageID)
		grouped[mid] = append(grouped[mid], h.attachmentToResponse(a))
	}
	return grouped
}

func (h *Handler) UploadFile(w http.ResponseWriter, r *http.Request) {
	if h.Storage == nil {
		writeError(w, http.StatusServiceUnavailable, "file upload not configured")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	workspaceID := h.resolveWorkspaceID(r)

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		writeError(w, http.StatusBadRequest, "file too large or invalid multipart form")
		return
	}
	defer r.MultipartForm.RemoveAll()

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("missing file field: %v", err))
		return
	}
	defer file.Close()

	buf := make([]byte, 512)
	n, err := file.Read(buf)
	if err != nil && err != io.EOF {
		writeError(w, http.StatusBadRequest, "failed to read file")
		return
	}

	contentType := resolveAttachmentContentType(http.DetectContentType(buf[:n]), header.Filename)

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read file")
		return
	}

	data, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read file")
		return
	}

	id, err := uuid.NewV7()
	if err != nil {
		slog.Error("failed to generate uuid", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	filename := id.String() + path.Ext(header.Filename)
	var key string
	if workspaceID != "" {
		key = "workspaces/" + workspaceID + "/" + filename
	} else {
		key = "users/" + userID + "/" + filename
	}

	if workspaceID != "" {
		if _, err := h.getWorkspaceMember(r.Context(), userID, workspaceID); err != nil {
			writeError(w, http.StatusForbidden, "not a member of this workspace")
			return
		}

		uploaderType, uploaderID := h.resolveActor(r, userID, workspaceID)

		params := db.CreateAttachmentParams{
			ID:           pgtype.UUID{Bytes: id, Valid: true},
			WorkspaceID:  parseUUID(workspaceID),
			UploaderType: uploaderType,
			UploaderID:   parseUUID(uploaderID),
			Filename:     header.Filename,
			ContentType:  contentType,
			SizeBytes:    int64(len(data)),
		}

		if issueID := r.FormValue("issue_id"); issueID != "" {
			issueUUID, ok := parseUUIDOrBadRequest(w, issueID, "issue_id")
			if !ok {
				return
			}
			issue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
				ID:          issueUUID,
				WorkspaceID: parseUUID(workspaceID),
			})
			if err != nil {
				writeError(w, http.StatusForbidden, "invalid issue_id")
				return
			}
			params.IssueID = issue.ID
		}
		if commentID := r.FormValue("comment_id"); commentID != "" {
			commentUUID, ok := parseUUIDOrBadRequest(w, commentID, "comment_id")
			if !ok {
				return
			}
			comment, err := h.Queries.GetComment(r.Context(), commentUUID)
			if err != nil || uuidToString(comment.WorkspaceID) != workspaceID {
				writeError(w, http.StatusForbidden, "invalid comment_id")
				return
			}
			params.CommentID = comment.ID
		}
		if chatSessionID := r.FormValue("chat_session_id"); chatSessionID != "" {

			session, ok := h.gateChatSessionForUser(w, r, userID, workspaceID, chatSessionID)
			if !ok {
				return
			}
			params.ChatSessionID = session.ID
		}

		if taskID := r.FormValue("task_id"); taskID != "" {

			if r.Header.Get("X-Actor-Source") != "task_token" {
				writeError(w, http.StatusForbidden, "task_id upload is only available from within an agent task")
				return
			}
			taskUUID, ok := parseUUIDOrBadRequest(w, taskID, "task_id")
			if !ok {
				return
			}

			boundTaskID := strings.TrimSpace(r.Header.Get("X-Task-ID"))
			if boundTaskID == "" || !strings.EqualFold(boundTaskID, strings.TrimSpace(taskID)) {
				writeError(w, http.StatusForbidden, "task_id must match the request's task token")
				return
			}
			task, err := h.Queries.GetAgentTaskInWorkspace(r.Context(), db.GetAgentTaskInWorkspaceParams{
				ID:          taskUUID,
				WorkspaceID: parseUUID(workspaceID),
			})
			if err != nil {
				writeError(w, http.StatusForbidden, "invalid task_id")
				return
			}
			if uploaderType != "agent" || uuidToString(task.AgentID) != uploaderID {
				writeError(w, http.StatusForbidden, "task_id upload requires the task's own agent")
				return
			}
			if !task.ChatSessionID.Valid {
				writeError(w, http.StatusBadRequest, "task_id upload requires a chat task")
				return
			}
			params.TaskID = task.ID

			params.ChatSessionID = task.ChatSessionID
		}

		link, err := h.Storage.Upload(r.Context(), key, data, contentType, header.Filename)
		if err != nil {
			slog.Error("file upload failed", "error", err)
			writeError(w, http.StatusInternalServerError, "upload failed")
			return
		}
		params.Url = link

		att, err := h.Queries.CreateAttachment(r.Context(), params)
		if err != nil {
			slog.Error("failed to create attachment record", "error", err)

		} else {
			writeJSON(w, http.StatusOK, h.attachmentToResponse(att))
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{
			"id":       "",
			"url":      link,
			"filename": header.Filename,
		})
		return
	}

	link, err := h.Storage.Upload(r.Context(), key, data, contentType, header.Filename)
	if err != nil {
		slog.Error("file upload failed", "error", err)
		writeError(w, http.StatusInternalServerError, "upload failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"id":       id.String(),
		"url":      link,
		"filename": header.Filename,
	})
}

func (h *Handler) ListAttachments(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	attachments, err := h.Queries.ListAttachmentsByIssue(r.Context(), db.ListAttachmentsByIssueParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		slog.Error("failed to list attachments", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list attachments")
		return
	}

	resp := make([]AttachmentResponse, len(attachments))
	for i, a := range attachments {
		resp[i] = h.attachmentToResponse(a)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) GetAttachmentByID(w http.ResponseWriter, r *http.Request) {

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	att, ok := h.loadAttachmentForRequest(w, r)
	if !ok {
		return
	}

	attachmentID := uuidToString(att.ID)
	resp := h.attachmentToResponse(att)

	if h.CFSigner == nil && h.resolveAttachmentDownloadMode(att.Url) == attachmentDownloadModePresign {
		if presigner, ok := h.Storage.(storage.DownloadPresigner); ok {
			key := h.Storage.KeyFromURL(att.Url)
			signedURL, err := presigner.PresignGetWithContentDisposition(r.Context(), key, h.attachmentDownloadURLTTL(), "")
			if err != nil {
				slog.Warn("failed to presign inline attachment URL", "id", attachmentID, "key", key, "error", err)
			} else {
				resp.DownloadURL = signedURL
			}
		}
	}

	ticketURL, err := h.attachmentDownloadTicketURL(attachmentID, userID)
	if err != nil {
		slog.Error("failed to mint attachment download ticket", "id", attachmentID, "error", err)
	} else {
		resp.DownloadTicketURL = ticketURL
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) loadAttachmentForRequest(w http.ResponseWriter, r *http.Request) (db.Attachment, bool) {
	attachmentID := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return db.Attachment{}, false
	}

	attUUID, ok := parseUUIDOrBadRequest(w, attachmentID, "attachment id")
	if !ok {
		return db.Attachment{}, false
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return db.Attachment{}, false
	}

	att, err := h.Queries.GetAttachment(r.Context(), db.GetAttachmentParams{
		ID:          attUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "attachment not found")
		return db.Attachment{}, false
	}

	return att, true
}

func (h *Handler) loadAttachmentForDownload(w http.ResponseWriter, r *http.Request) (db.Attachment, bool) {
	attachmentID := chi.URLParam(r, "id")
	attUUID, ok := parseUUIDOrBadRequest(w, attachmentID, "attachment id")
	if !ok {
		return db.Attachment{}, false
	}
	att, err := h.Queries.GetAttachmentByIDOnly(r.Context(), attUUID)
	if err != nil {

		writeError(w, http.StatusNotFound, "attachment not found")
		return db.Attachment{}, false
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return db.Attachment{}, false
	}

	workspaceID := uuidToString(att.WorkspaceID)
	if workspaceID == "" {
		writeError(w, http.StatusNotFound, "attachment not found")
		return db.Attachment{}, false
	}
	if h.MembershipCache.Get(r.Context(), userID, workspaceID) {
		return att, true
	}
	if _, err := h.getWorkspaceMember(r.Context(), userID, workspaceID); err != nil {
		writeError(w, http.StatusNotFound, "attachment not found")
		return db.Attachment{}, false
	}
	h.MembershipCache.Set(r.Context(), userID, workspaceID)
	return att, true
}

func writeDownloadError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Disposition", "inline")
	writeError(w, status, msg)
}

func (h *Handler) DownloadAttachment(w http.ResponseWriter, r *http.Request) {
	att, ok := h.loadAttachmentForDownload(w, r)
	if !ok {
		return
	}
	if h.Storage == nil {
		writeDownloadError(w, http.StatusServiceUnavailable, "storage not configured")
		return
	}

	key := h.Storage.KeyFromURL(att.Url)
	switch h.resolveAttachmentDownloadMode(att.Url) {
	case attachmentDownloadModeCloudFront:
		if h.CFSigner == nil {
			writeDownloadError(w, http.StatusInternalServerError, "cloudfront attachment downloads are not configured")
			return
		}
		h.setAttachmentPreviewSecurityHeaders(w)
		http.Redirect(
			w,
			r,
			h.CFSigner.SignedURLWithContentDisposition(
				att.Url,
				storage.AttachmentContentDisposition(att.Filename),
				time.Now().Add(h.attachmentDownloadURLTTL()),
			),
			http.StatusFound,
		)
	case attachmentDownloadModePresign:
		presigner, ok := h.Storage.(storage.DownloadPresigner)
		if !ok {
			writeDownloadError(w, http.StatusInternalServerError, "attachment storage does not support presigned downloads")
			return
		}
		signedURL, err := presigner.PresignGetWithContentDisposition(
			r.Context(),
			key,
			h.attachmentDownloadURLTTL(),
			storage.AttachmentContentDisposition(att.Filename),
		)
		if err != nil {
			slog.Error("failed to presign attachment download", "id", uuidToString(att.ID), "key", key, "error", err)
			writeDownloadError(w, http.StatusBadGateway, "failed to create download URL")
			return
		}
		h.setAttachmentPreviewSecurityHeaders(w)
		http.Redirect(w, r, signedURL, http.StatusFound)
	case attachmentDownloadModeProxy:
		h.proxyAttachmentDownload(w, r, att, key)
	default:
		writeDownloadError(w, http.StatusInternalServerError, "invalid attachment download mode")
	}
}

func (h *Handler) resolveAttachmentDownloadMode(rawURL string) attachmentDownloadMode {
	switch h.attachmentDownloadMode() {
	case attachmentDownloadModeCloudFront:
		return attachmentDownloadModeCloudFront
	case attachmentDownloadModePresign:
		return attachmentDownloadModePresign
	case attachmentDownloadModeProxy:
		return attachmentDownloadModeProxy
	}
	if h.CFSigner != nil {
		return attachmentDownloadModeCloudFront
	}
	if shouldProxyAttachmentURL(rawURL) {
		return attachmentDownloadModeProxy
	}
	if _, ok := h.Storage.(storage.DownloadPresigner); ok {
		return attachmentDownloadModePresign
	}
	return attachmentDownloadModeProxy
}

func shouldProxyAttachmentURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return true
	}
	host := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(u.Hostname()), "."))
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	if !strings.Contains(host, ".") {
		return true
	}
	switch {
	case strings.HasSuffix(host, ".local"),
		strings.HasSuffix(host, ".localdomain"),
		strings.HasSuffix(host, ".internal"),
		strings.HasSuffix(host, ".lan"),
		strings.HasSuffix(host, ".home"),
		strings.HasSuffix(host, ".docker"):
		return true
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return addr.IsLoopback() ||
			addr.IsPrivate() ||
			addr.IsLinkLocalUnicast() ||
			addr.IsLinkLocalMulticast() ||
			addr.IsUnspecified()
	}
	return false
}

func (h *Handler) ServeLocalUpload(w http.ResponseWriter, r *http.Request) {
	local, ok := h.Storage.(*storage.LocalStorage)
	if !ok {
		http.NotFound(w, r)
		return
	}
	h.setAttachmentPreviewSecurityHeaders(w)
	key := strings.TrimPrefix(r.URL.Path, "/uploads/")
	local.ServeFile(w, r, key)
}

func (h *Handler) proxyAttachmentDownload(w http.ResponseWriter, r *http.Request, att db.Attachment, key string) {
	reader, err := h.Storage.GetReader(r.Context(), key)
	if err != nil {
		slog.Error("failed to open attachment for download", "id", uuidToString(att.ID), "key", key, "error", err)
		writeDownloadError(w, http.StatusNotFound, "attachment object not found")
		return
	}
	defer reader.Close()

	contentType := resolveAttachmentContentType(att.ContentType, att.Filename)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", storage.ContentDisposition(contentType, att.Filename))

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	h.setAttachmentPreviewSecurityHeaders(w)

	if seeker, ok := reader.(io.ReadSeeker); ok {
		http.ServeContent(w, r, att.Filename, time.Time{}, seeker)
		return
	}

	h.serveProxyRange(w, r, att, reader)
}

func (h *Handler) serveProxyRange(w http.ResponseWriter, r *http.Request, att db.Attachment, reader io.Reader) {
	total := att.SizeBytes
	w.Header().Set("Accept-Ranges", "bytes")

	rangeHeader := strings.TrimSpace(r.Header.Get("Range"))

	serveFull := rangeHeader == "" || total < 0
	var start, length int64
	if !serveFull {
		var outcome rangeParseOutcome
		start, length, outcome = parseSingleByteRange(rangeHeader, total)
		switch outcome {
		case rangeUnsupported:

			serveFull = true
		case rangeUnsatisfiable:

			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", total))
			writeError(w, http.StatusRequestedRangeNotSatisfiable, "requested range not satisfiable")
			return
		}
	}

	if serveFull {
		if total >= 0 {
			w.Header().Set("Content-Length", strconv.FormatInt(total, 10))
		}
		if _, err := io.Copy(w, reader); err != nil {
			slog.Error("failed to stream attachment download", "id", uuidToString(att.ID), "error", err)
		}
		return
	}

	if start > 0 {
		if _, err := io.CopyN(io.Discard, reader, start); err != nil {

			slog.Error("failed to skip to range start for attachment", "id", uuidToString(att.ID), "error", err)
			writeDownloadError(w, http.StatusBadGateway, "failed to read attachment range")
			return
		}
	}
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, start+length-1, total))
	w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
	w.WriteHeader(http.StatusPartialContent)
	if _, err := io.CopyN(w, reader, length); err != nil {
		slog.Error("failed to stream attachment range", "id", uuidToString(att.ID), "error", err)
	}
}

type rangeParseOutcome int

const (
	rangeSatisfiable rangeParseOutcome = iota

	rangeUnsatisfiable

	rangeUnsupported
)

func parseSingleByteRange(header string, size int64) (start, length int64, outcome rangeParseOutcome) {
	const prefix = "bytes="
	if !strings.HasPrefix(header, prefix) {

		return 0, 0, rangeUnsatisfiable
	}
	spec := strings.TrimSpace(header[len(prefix):])
	if spec == "" {
		return 0, 0, rangeUnsatisfiable
	}

	if strings.Contains(spec, ",") {
		return 0, 0, rangeUnsupported
	}
	dash := strings.IndexByte(spec, '-')
	if dash < 0 {
		return 0, 0, rangeUnsatisfiable
	}
	startStr := strings.TrimSpace(spec[:dash])
	endStr := strings.TrimSpace(spec[dash+1:])

	if startStr == "" {

		if endStr == "" {
			return 0, 0, rangeUnsatisfiable
		}
		n, err := strconv.ParseInt(endStr, 10, 64)
		if err != nil || n <= 0 {
			return 0, 0, rangeUnsatisfiable
		}
		if size == 0 {

			return 0, 0, rangeUnsupported
		}
		if n > size {
			n = size
		}
		return size - n, n, rangeSatisfiable
	}

	s, err := strconv.ParseInt(startStr, 10, 64)
	if err != nil || s < 0 {
		return 0, 0, rangeUnsatisfiable
	}
	if s >= size {

		if size == 0 {
			return 0, 0, rangeUnsupported
		}
		return 0, 0, rangeUnsatisfiable
	}
	if endStr == "" {
		return s, size - s, rangeSatisfiable
	}
	e, err := strconv.ParseInt(endStr, 10, 64)
	if err != nil || e < s {
		return 0, 0, rangeUnsatisfiable
	}
	if e >= size {
		e = size - 1
	}
	return s, e - s + 1, rangeSatisfiable
}

func (h *Handler) setAttachmentPreviewSecurityHeaders(w http.ResponseWriter) {

	w.Header().Set("Content-Security-Policy", attachmentPreviewCSPHeader(h.cfg.AttachmentFrameAncestors))
}

func attachmentPreviewCSPHeader(frameAncestors []string) string {
	ancestors := []string{"'self'"}
	seen := map[string]struct{}{"'self'": {}}
	for _, raw := range frameAncestors {
		source, ok := normalizeFrameAncestorSource(raw)
		if !ok {
			continue
		}
		if _, exists := seen[source]; exists {
			continue
		}
		seen[source] = struct{}{}
		ancestors = append(ancestors, source)
	}
	return "default-src 'none'; " +
		"img-src 'self' data:; " +
		"media-src 'self'; " +
		"frame-ancestors " + strings.Join(ancestors, " ") + "; " +
		"object-src 'none'; " +
		"base-uri 'none'; " +
		"form-action 'none'"
}

func normalizeFrameAncestorSource(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "*" {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", false
	}
	return scheme + "://" + strings.ToLower(u.Host), true
}

func (h *Handler) GetAttachmentContent(w http.ResponseWriter, r *http.Request) {
	att, ok := h.loadAttachmentForRequest(w, r)
	if !ok {
		return
	}
	attachmentID := uuidToString(att.ID)

	if !isTextPreviewable(att.ContentType, att.Filename) {
		writeError(w, http.StatusUnsupportedMediaType, "preview not supported for this file type")
		return
	}

	if h.Storage == nil {
		writeError(w, http.StatusServiceUnavailable, "storage not configured")
		return
	}

	limit := maxPreviewSizeFor(att.ContentType, att.Filename)
	if att.SizeBytes > limit {
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf(
			"file is %s; inline preview for this file type is limited to %s",
			humanBytes(att.SizeBytes), humanBytes(limit),
		))
		return
	}

	key := h.Storage.KeyFromURL(att.Url)
	reader, err := h.Storage.GetReader(r.Context(), key)
	if err != nil {
		slog.Error("failed to open attachment for preview", "id", attachmentID, "key", key, "error", err)
		writeError(w, http.StatusNotFound, "attachment object not found")
		return
	}
	defer reader.Close()

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Original-Content-Type", att.ContentType)

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	h.setAttachmentPreviewSecurityHeaders(w)

	if att.SizeBytes <= 0 {
		h.writeBufferedPreview(w, attachmentID, reader, limit)
		return
	}

	w.Header().Set("Content-Length", strconv.FormatInt(att.SizeBytes, 10))

	if _, err := io.Copy(w, io.LimitReader(reader, limit)); err != nil {

		slog.Error("failed to stream attachment preview body", "id", attachmentID, "error", err)
	}
}

func (h *Handler) writeBufferedPreview(w http.ResponseWriter, attachmentID string, reader io.Reader, limit int64) {
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		slog.Error("failed to read attachment body for preview", "id", attachmentID, "error", err)
		writeError(w, http.StatusBadGateway, "failed to read attachment body")
		return
	}
	if int64(len(body)) > limit {
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf(
			"file is larger than %s, the inline preview limit for this file type",
			humanBytes(limit),
		))
		return
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	if _, err := w.Write(body); err != nil {
		slog.Error("failed to write attachment preview body", "id", attachmentID, "error", err)
	}
}

func isTextPreviewable(contentType, filename string) bool {
	ct := strings.ToLower(strings.TrimSpace(contentType))

	if idx := strings.Index(ct, ";"); idx >= 0 {
		ct = strings.TrimSpace(ct[:idx])
	}
	if strings.HasPrefix(ct, "text/") {
		return true
	}
	switch ct {
	case "application/json",
		"application/javascript",
		"application/xml",
		"application/x-yaml",
		"application/yaml",
		"application/toml",
		"application/x-sh",
		"application/x-httpd-php":
		return true
	}

	ext := strings.ToLower(path.Ext(filename))
	switch ext {
	case ".md", ".markdown",
		".txt", ".log",
		".csv", ".tsv",
		".html", ".htm",
		".json", ".xml",
		".yml", ".yaml", ".toml", ".ini", ".conf",
		".sh", ".bash", ".zsh",
		".py", ".rb", ".go", ".rs",
		".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs",
		".css", ".scss", ".sass", ".less",
		".sql",
		".java", ".kt", ".swift",
		".c", ".cc", ".cpp", ".h", ".hpp",
		".cs", ".php", ".lua", ".vim",
		".dockerfile", ".makefile", ".gitignore":
		return true
	}

	base := strings.ToLower(path.Base(filename))
	switch base {
	case "dockerfile", "makefile", ".env":
		return true
	}
	return false
}

func (h *Handler) DeleteAttachment(w http.ResponseWriter, r *http.Request) {
	attachmentID := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	attUUID, ok := parseUUIDOrBadRequest(w, attachmentID, "attachment id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	att, err := h.Queries.GetAttachment(r.Context(), db.GetAttachmentParams{
		ID:          attUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "attachment not found")
		return
	}

	uploaderID := uuidToString(att.UploaderID)
	isUploader := att.UploaderType == "member" && uploaderID == userID
	member, hasMember := ctxMember(r.Context())
	isAdmin := hasMember && (member.Role == "admin" || member.Role == "owner")

	if !isUploader && !isAdmin {
		writeError(w, http.StatusForbidden, "not authorized to delete this attachment")
		return
	}

	if err := h.Queries.DeleteAttachment(r.Context(), db.DeleteAttachmentParams{
		ID:          att.ID,
		WorkspaceID: att.WorkspaceID,
	}); err != nil {
		slog.Error("failed to delete attachment", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete attachment")
		return
	}

	h.deleteS3Object(r.Context(), att.Url)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) linkAttachmentsByIssueIDs(ctx context.Context, issueID, workspaceID pgtype.UUID, ids []pgtype.UUID) {
	if err := h.Queries.LinkAttachmentsToIssue(ctx, db.LinkAttachmentsToIssueParams{
		IssueID:     issueID,
		WorkspaceID: workspaceID,
		Column3:     ids,
	}); err != nil {
		slog.Error("failed to link attachments to issue", "error", err)
	}
}

func (h *Handler) linkAttachmentsByIDs(ctx context.Context, commentID, issueID pgtype.UUID, ids []pgtype.UUID) {
	if err := h.Queries.LinkAttachmentsToComment(ctx, db.LinkAttachmentsToCommentParams{
		CommentID: commentID,
		IssueID:   issueID,
		Column3:   ids,
	}); err != nil {
		slog.Error("failed to link attachments to comment", "error", err)
	}
}

func (h *Handler) deleteS3Object(ctx context.Context, url string) {
	if h.Storage == nil || url == "" {
		return
	}
	h.Storage.Delete(ctx, h.Storage.KeyFromURL(url))
}

func (h *Handler) deleteS3Objects(ctx context.Context, urls []string) {
	if h.Storage == nil || len(urls) == 0 {
		return
	}
	keys := make([]string, len(urls))
	for i, u := range urls {
		keys[i] = h.Storage.KeyFromURL(u)
	}
	h.Storage.DeleteKeys(ctx, keys)
}
