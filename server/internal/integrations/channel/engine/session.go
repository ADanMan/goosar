package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/integrations/channel"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

const pgSQLStateUniqueViolation = "23505"

type TxStarter interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

type SessionQueries interface {
	WithTx(tx pgx.Tx) SessionQueries
	GetChannelChatSessionBinding(ctx context.Context, arg db.GetChannelChatSessionBindingParams) (db.ChannelChatSessionBinding, error)
	LockWorkspaceForChatSessionCreate(ctx context.Context, id pgtype.UUID) (pgtype.UUID, error)
	CreateChatSession(ctx context.Context, arg db.CreateChatSessionParams) (db.ChatSession, error)
	CreateChannelChatSessionBinding(ctx context.Context, arg db.CreateChannelChatSessionBindingParams) (db.ChannelChatSessionBinding, error)
	CreateChatMessage(ctx context.Context, arg db.CreateChatMessageParams) (db.ChatMessage, error)
	ClearChatMessageChannelMediaPending(ctx context.Context, arg db.ClearChatMessageChannelMediaPendingParams) error
	CreateAttachment(ctx context.Context, arg db.CreateAttachmentParams) (db.Attachment, error)
	LinkAttachmentsToChatMessage(ctx context.Context, arg db.LinkAttachmentsToChatMessageParams) ([]pgtype.UUID, error)
	ClaimChannelMediaPendingObjectsForBind(ctx context.Context, arg db.ClaimChannelMediaPendingObjectsForBindParams) ([]string, error)
	TouchChatSession(ctx context.Context, id pgtype.UUID) error
	GetMostRecentUserChatMessage(ctx context.Context, chatSessionID pgtype.UUID) (db.ChatMessage, error)
	UpdateChannelChatSessionBindingReplyTarget(ctx context.Context, arg db.UpdateChannelChatSessionBindingReplyTargetParams) error
	MarkChannelInboundDedupProcessed(ctx context.Context, arg db.MarkChannelInboundDedupProcessedParams) (int64, error)
}

type dbSessionQueries struct{ q *db.Queries }

func (a dbSessionQueries) WithTx(tx pgx.Tx) SessionQueries {
	return dbSessionQueries{q: a.q.WithTx(tx)}
}

func (a dbSessionQueries) GetChannelChatSessionBinding(ctx context.Context, arg db.GetChannelChatSessionBindingParams) (db.ChannelChatSessionBinding, error) {
	return a.q.GetChannelChatSessionBinding(ctx, arg)
}

func (a dbSessionQueries) LockWorkspaceForChatSessionCreate(ctx context.Context, id pgtype.UUID) (pgtype.UUID, error) {
	return a.q.LockWorkspaceForChatSessionCreate(ctx, id)
}

func (a dbSessionQueries) CreateChatSession(ctx context.Context, arg db.CreateChatSessionParams) (db.ChatSession, error) {
	return a.q.CreateChatSession(ctx, arg)
}

func (a dbSessionQueries) CreateChannelChatSessionBinding(ctx context.Context, arg db.CreateChannelChatSessionBindingParams) (db.ChannelChatSessionBinding, error) {
	return a.q.CreateChannelChatSessionBinding(ctx, arg)
}

func (a dbSessionQueries) CreateChatMessage(ctx context.Context, arg db.CreateChatMessageParams) (db.ChatMessage, error) {
	return a.q.CreateChatMessage(ctx, arg)
}

func (a dbSessionQueries) ClearChatMessageChannelMediaPending(ctx context.Context, arg db.ClearChatMessageChannelMediaPendingParams) error {
	return a.q.ClearChatMessageChannelMediaPending(ctx, arg)
}

func (a dbSessionQueries) CreateAttachment(ctx context.Context, arg db.CreateAttachmentParams) (db.Attachment, error) {
	return a.q.CreateAttachment(ctx, arg)
}

func (a dbSessionQueries) LinkAttachmentsToChatMessage(ctx context.Context, arg db.LinkAttachmentsToChatMessageParams) ([]pgtype.UUID, error) {
	return a.q.LinkAttachmentsToChatMessage(ctx, arg)
}

func (a dbSessionQueries) ClaimChannelMediaPendingObjectsForBind(ctx context.Context, arg db.ClaimChannelMediaPendingObjectsForBindParams) ([]string, error) {
	return a.q.ClaimChannelMediaPendingObjectsForBind(ctx, arg)
}

func (a dbSessionQueries) TouchChatSession(ctx context.Context, id pgtype.UUID) error {
	return a.q.TouchChatSession(ctx, id)
}

func (a dbSessionQueries) GetMostRecentUserChatMessage(ctx context.Context, chatSessionID pgtype.UUID) (db.ChatMessage, error) {
	return a.q.GetMostRecentUserChatMessage(ctx, chatSessionID)
}

func (a dbSessionQueries) UpdateChannelChatSessionBindingReplyTarget(ctx context.Context, arg db.UpdateChannelChatSessionBindingReplyTargetParams) error {
	return a.q.UpdateChannelChatSessionBindingReplyTarget(ctx, arg)
}

func (a dbSessionQueries) MarkChannelInboundDedupProcessed(ctx context.Context, arg db.MarkChannelInboundDedupProcessedParams) (int64, error) {
	return a.q.MarkChannelInboundDedupProcessed(ctx, arg)
}

type SessionTitles struct {
	Group    string
	Direct   string
	Fallback string
}

func (t SessionTitles) forType(ct channel.ChatType) string {
	switch ct {
	case channel.ChatTypeGroup:
		return t.Group
	case channel.ChatTypeP2P:
		return t.Direct
	default:
		return t.Fallback
	}
}

type ChatSession struct {
	q           SessionQueries
	tx          TxStarter
	channelType channel.Type
	titles      SessionTitles
}

func NewChatSession(q *db.Queries, tx TxStarter, channelType channel.Type, titles SessionTitles) *ChatSession {
	return &ChatSession{q: dbSessionQueries{q: q}, tx: tx, channelType: channelType, titles: titles}
}

func newChatSessionWith(q SessionQueries, tx TxStarter, channelType channel.Type, titles SessionTitles) *ChatSession {
	return &ChatSession{q: q, tx: tx, channelType: channelType, titles: titles}
}

type EnsureSessionInput struct {
	WorkspaceID    pgtype.UUID
	AgentID        pgtype.UUID
	InstallationID pgtype.UUID
	Sender         pgtype.UUID
	BindingKey     string
	BindingConfig  []byte
	ChatType       channel.ChatType
}

func (s *ChatSession) EnsureSession(ctx context.Context, in EnsureSessionInput) (pgtype.UUID, error) {
	lookup := db.GetChannelChatSessionBindingParams{InstallationID: in.InstallationID, ChannelChatID: in.BindingKey}

	existing, err := s.q.GetChannelChatSessionBinding(ctx, lookup)
	if err == nil {
		return existing.ChatSessionID, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return pgtype.UUID{}, fmt.Errorf("lookup chat session binding: %w", err)
	}

	id, err := s.createSessionAndBinding(ctx, in)
	if err == nil {
		return id, nil
	}
	if isUniqueViolation(err) {
		existing, lookupErr := s.q.GetChannelChatSessionBinding(ctx, lookup)
		if lookupErr == nil {
			return existing.ChatSessionID, nil
		}
		return pgtype.UUID{}, fmt.Errorf("race re-read after unique violation: %w", lookupErr)
	}
	return pgtype.UUID{}, err
}

func (s *ChatSession) createSessionAndBinding(ctx context.Context, in EnsureSessionInput) (pgtype.UUID, error) {
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.q.WithTx(tx)

	if _, err := qtx.LockWorkspaceForChatSessionCreate(ctx, in.WorkspaceID); err != nil {
		return pgtype.UUID{}, fmt.Errorf("lock workspace for chat session create: %w", err)
	}

	session, err := qtx.CreateChatSession(ctx, db.CreateChatSessionParams{
		WorkspaceID: in.WorkspaceID,
		AgentID:     in.AgentID,
		CreatorID:   in.Sender,
		Title:       s.titles.forType(in.ChatType),
	})
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("create chat session: %w", err)
	}
	bindingConfig := in.BindingConfig
	if len(bindingConfig) == 0 {
		bindingConfig = []byte("{}")
	}
	if _, err := qtx.CreateChannelChatSessionBinding(ctx, db.CreateChannelChatSessionBindingParams{
		ChatSessionID:  session.ID,
		InstallationID: in.InstallationID,
		ChannelType:    string(s.channelType),
		ChannelChatID:  in.BindingKey,
		ChatType:       string(in.ChatType),
		Config:         bindingConfig,
	}); err != nil {
		return pgtype.UUID{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return pgtype.UUID{}, fmt.Errorf("commit: %w", err)
	}
	return session.ID, nil
}

type AppendInput struct {
	SessionID           pgtype.UUID
	Sender              pgtype.UUID
	InstallationID      pgtype.UUID
	Body                string
	CommandText         string
	MessageID           string
	ThreadID            string
	ClaimToken          pgtype.UUID
	MediaPendingSeconds float64
}

type BindMediaInput struct {
	MessageID   pgtype.UUID
	SessionID   pgtype.UUID
	WorkspaceID pgtype.UUID
	Sender      pgtype.UUID
	MediaRefs   []channel.MediaRef
}

func (s *ChatSession) AppendUserMessage(ctx context.Context, in AppendInput) (AppendResult, error) {
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return AppendResult{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.q.WithTx(tx)

	commandSource := in.CommandText
	if commandSource == "" {
		commandSource = in.Body
	}
	cmd, _ := ParseIssueCommand(commandSource)
	if cmd != nil && cmd.Title == "" {
		prev, err := qtx.GetMostRecentUserChatMessage(ctx, in.SessionID)
		if err == nil {
			cmd.Title = titleFromPreviousMessage(prev.Content)
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return AppendResult{}, fmt.Errorf("previous message lookup: %w", err)
		}
	}

	msg, err := qtx.CreateChatMessage(ctx, db.CreateChatMessageParams{
		ChatSessionID:           in.SessionID,
		Role:                    "user",
		Content:                 in.Body,
		ChannelMediaPendingSecs: pgtype.Float8{Float64: in.MediaPendingSeconds, Valid: in.MediaPendingSeconds > 0},
		ChannelIngested:         pgtype.Bool{Bool: true, Valid: true},
	})
	if err != nil {
		return AppendResult{}, fmt.Errorf("create chat message: %w", err)
	}
	if err := qtx.TouchChatSession(ctx, in.SessionID); err != nil {
		return AppendResult{}, fmt.Errorf("touch chat session: %w", err)
	}

	if in.MessageID != "" {
		if err := qtx.UpdateChannelChatSessionBindingReplyTarget(ctx, db.UpdateChannelChatSessionBindingReplyTargetParams{
			ChatSessionID: in.SessionID,
			LastMessageID: textOrNull(in.MessageID),
			LastThreadID:  textOrNull(in.ThreadID),
		}); err != nil {
			return AppendResult{}, fmt.Errorf("update reply target: %w", err)
		}
	}

	markedInTx := false
	if in.ClaimToken.Valid && in.MessageID != "" {
		rows, err := qtx.MarkChannelInboundDedupProcessed(ctx, db.MarkChannelInboundDedupProcessedParams{
			InstallationID: in.InstallationID,
			MessageID:      in.MessageID,
			ClaimToken:     in.ClaimToken,
		})
		if err != nil {
			return AppendResult{}, fmt.Errorf("mark dedup processed: %w", err)
		}
		if rows == 0 {

			return AppendResult{}, ErrClaimLost
		}
		markedInTx = true
	}

	if err := tx.Commit(ctx); err != nil {
		return AppendResult{}, fmt.Errorf("commit: %w", err)
	}
	return AppendResult{MessageID: msg.ID, IssueCommand: cmd, DedupMarked: markedInTx}, nil
}

func (s *ChatSession) BindMediaRefs(ctx context.Context, in BindMediaInput) error {
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin media tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.q.WithTx(tx)
	if len(in.MediaRefs) > 0 {
		if err := s.bindMediaRefs(ctx, qtx, in); err != nil {
			_ = tx.Rollback(ctx)
			if clearErr := s.clearMediaPending(ctx, s.q, in); clearErr != nil {
				return errors.Join(err, clearErr)
			}
			return err
		}
	}
	if err := s.clearMediaPending(ctx, qtx, in); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {

		return fmt.Errorf("commit media: %w", err)
	}
	return nil
}

func (s *ChatSession) clearMediaPending(ctx context.Context, q SessionQueries, in BindMediaInput) error {
	if err := q.ClearChatMessageChannelMediaPending(ctx, db.ClearChatMessageChannelMediaPendingParams{
		ID:            in.MessageID,
		ChatSessionID: in.SessionID,
	}); err != nil {
		return fmt.Errorf("clear chat message media pending: %w", err)
	}
	return nil
}

func (s *ChatSession) bindMediaRefs(ctx context.Context, qtx SessionQueries, in BindMediaInput) error {
	if !in.WorkspaceID.Valid {
		return errors.New("bind media refs: workspace_id is required")
	}
	if !in.MessageID.Valid {
		return errors.New("bind media refs: message_id is required")
	}
	keys := make([]string, 0, len(in.MediaRefs))
	for _, ref := range in.MediaRefs {
		if ref.StorageURL == "" {
			return errors.New("bind media refs: storage_url is required")
		}
		if ref.StorageKey == "" {
			return errors.New("bind media refs: storage_key is required")
		}
		keys = append(keys, ref.StorageKey)
	}

	claimedKeys, err := qtx.ClaimChannelMediaPendingObjectsForBind(ctx, db.ClaimChannelMediaPendingObjectsForBindParams{
		StorageKeys: keys,
		WorkspaceID: in.WorkspaceID,
	})
	if err != nil {
		return fmt.Errorf("claim media intents: %w", err)
	}
	claimed := make(map[string]bool, len(claimedKeys))
	for _, k := range claimedKeys {
		claimed[k] = true
	}
	ids := make([]pgtype.UUID, 0, len(in.MediaRefs))
	for _, ref := range in.MediaRefs {
		if !claimed[ref.StorageKey] {
			slog.Warn("channel media: intent claimed by reconciler; skipping attach",
				"storage_key", ref.StorageKey)
			continue
		}
		id, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("create attachment id: %w", err)
		}
		contentType := ref.MimeType
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		filename := ref.Filename
		if filename == "" {
			filename = defaultMediaFilename(ref.Type, id.String(), contentType)
		}
		att, err := qtx.CreateAttachment(ctx, db.CreateAttachmentParams{
			ID:            pgtype.UUID{Bytes: id, Valid: true},
			WorkspaceID:   in.WorkspaceID,
			ChatSessionID: in.SessionID,
			UploaderType:  "member",
			UploaderID:    in.Sender,
			Filename:      filename,
			Url:           ref.StorageURL,
			ContentType:   contentType,
			SizeBytes:     ref.SizeBytes,
		})
		if err != nil {
			return fmt.Errorf("create chat attachment: %w", err)
		}
		ids = append(ids, att.ID)
	}
	if len(ids) == 0 {
		return nil
	}
	if _, err := qtx.LinkAttachmentsToChatMessage(ctx, db.LinkAttachmentsToChatMessageParams{
		ChatMessageID: in.MessageID,
		ChatSessionID: in.SessionID,
		WorkspaceID:   in.WorkspaceID,
		UploaderType:  "member",
		UploaderID:    in.Sender,
		AttachmentIds: ids,
	}); err != nil {
		return fmt.Errorf("link chat attachments: %w", err)
	}
	return nil
}

func defaultMediaFilename(kind channel.MsgType, id, contentType string) string {
	prefix := "attachment"
	switch kind {
	case channel.MsgTypeImage:
		prefix = "image"
	case channel.MsgTypeVideo:
		prefix = "video"
	case channel.MsgTypeAudio:
		prefix = "audio"
	case channel.MsgTypeFile:
		prefix = "file"
	}
	ext := ""
	switch contentType {
	case "image/jpeg":
		ext = ".jpg"
	case "image/png":
		ext = ".png"
	case "image/gif":
		ext = ".gif"
	case "image/webp":
		ext = ".webp"
	case "video/mp4":
		ext = ".mp4"
	}
	return prefix + "-" + id + ext
}

func isUniqueViolation(err error) bool {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg.Code == pgSQLStateUniqueViolation
	}
	return false
}

func textOrNull(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}
