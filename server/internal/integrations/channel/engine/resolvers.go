package engine

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/integrations/channel"
	"github.com/adanman/goosar/server/internal/service"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

type Outcome string

const (
	OutcomeDropped       Outcome = "dropped"
	OutcomeNeedsBinding  Outcome = "needs_binding"
	OutcomeIngested      Outcome = "ingested"
	OutcomeAgentOffline  Outcome = "agent_offline"
	OutcomeAgentArchived Outcome = "agent_archived"
)

type DropReason string

const (
	DropReasonUnboundUser         DropReason = "unbound_user"
	DropReasonNonWorkspaceMember  DropReason = "non_workspace_member"
	DropReasonNotAddressedInGroup DropReason = "not_addressed_in_group"
	DropReasonDuplicate           DropReason = "duplicate"
	DropReasonRevokedInstallation DropReason = "revoked_installation"
	DropReasonInvalidEvent        DropReason = "invalid_event"
)

type Result struct {
	Outcome        Outcome
	DropReason     DropReason
	InstallationID pgtype.UUID
	ChatSessionID  pgtype.UUID

	Sender          string
	IssueID         pgtype.UUID
	IssueNumber     int32
	IssueIdentifier string
	IssueTitle      string
}

type ResolvedInstallation struct {
	ID              pgtype.UUID
	WorkspaceID     pgtype.UUID
	AgentID         pgtype.UUID
	InstallerUserID pgtype.UUID
	Active          bool
	Platform        any
}

type ResolvedIdentity struct {
	UserID pgtype.UUID
}

type EnsureSessionParams struct {
	Installation ResolvedInstallation
	Sender       pgtype.UUID
	Message      channel.InboundMessage
}

type AppendParams struct {
	SessionID           pgtype.UUID
	Sender              pgtype.UUID
	InstallationID      pgtype.UUID
	Message             channel.InboundMessage
	ClaimToken          pgtype.UUID
	MediaPendingSeconds float64
}

type AppendResult struct {
	MessageID pgtype.UUID

	IssueCommand *IssueCommand

	DedupMarked bool
}

type BindMediaParams struct {
	MessageID   pgtype.UUID
	SessionID   pgtype.UUID
	WorkspaceID pgtype.UUID
	Sender      pgtype.UUID
	MediaRefs   []channel.MediaRef
}

type IssueCommand struct {
	Title       string
	Description string
}

var (
	ErrInstallationNotFound = errors.New("engine: installation not found")

	ErrSenderUnbound = errors.New("engine: sender unbound")

	ErrSenderNotMember = errors.New("engine: sender not a workspace member")

	ErrDuplicate = errors.New("engine: duplicate message")

	ErrClaimLost = errors.New("engine: dedup claim lost")
)

type InstallationResolver interface {
	ResolveInstallation(ctx context.Context, msg channel.InboundMessage) (ResolvedInstallation, error)
}

type IdentityResolver interface {
	ResolveSender(ctx context.Context, inst ResolvedInstallation, msg channel.InboundMessage) (ResolvedIdentity, error)
}

type Deduper interface {
	Claim(ctx context.Context, installationID pgtype.UUID, messageID string) (claimToken pgtype.UUID, err error)
	Mark(ctx context.Context, installationID pgtype.UUID, messageID string, claimToken pgtype.UUID) error
	Release(ctx context.Context, installationID pgtype.UUID, messageID string, claimToken pgtype.UUID) error
}

type SessionBinder interface {
	EnsureSession(ctx context.Context, p EnsureSessionParams) (pgtype.UUID, error)
	AppendMessage(ctx context.Context, p AppendParams) (AppendResult, error)
	BindMedia(ctx context.Context, p BindMediaParams) error
}

type MediaResolver interface {
	HasMedia(msg channel.InboundMessage) bool

	ResolveMedia(ctx context.Context, inst ResolvedInstallation, sender ResolvedIdentity, sessionID, chatMessageID pgtype.UUID, msg channel.InboundMessage) channel.InboundMessage
}

type MediaIntentLedger interface {
	RecordPendingMediaObject(ctx context.Context, p RecordPendingMediaObjectParams) (ok bool, err error)
}

type RecordPendingMediaObjectParams struct {
	StorageKey     string
	WorkspaceID    pgtype.UUID
	ChatMessageID  pgtype.UUID
	StorageURL     string
	InstallationID pgtype.UUID
}

func NewDBMediaIntentLedger(q *db.Queries) MediaIntentLedger {
	return dbMediaIntentLedger{q: q}
}

type dbMediaIntentLedger struct{ q *db.Queries }

func (l dbMediaIntentLedger) RecordPendingMediaObject(ctx context.Context, p RecordPendingMediaObjectParams) (bool, error) {
	_, err := l.q.RecordChannelMediaPendingObject(ctx, db.RecordChannelMediaPendingObjectParams{
		StorageKey:     p.StorageKey,
		WorkspaceID:    p.WorkspaceID,
		ChatMessageID:  p.ChatMessageID,
		StorageUrl:     p.StorageURL,
		InstallationID: p.InstallationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {

		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

type Auditor interface {
	RecordDrop(ctx context.Context, instID pgtype.UUID, msg channel.InboundMessage, reason DropReason) error
}

type OutboundReplier interface {
	Reply(ctx context.Context, inst ResolvedInstallation, msg channel.InboundMessage, res Result)
}

type TypingNotifier interface {
	OnIngested(ctx context.Context, inst ResolvedInstallation, msg channel.InboundMessage, sessionID pgtype.UUID)

	OnSettled(ctx context.Context, sessionID pgtype.UUID)
}

type ResolverSet struct {
	Installation InstallationResolver
	Identity     IdentityResolver
	Dedup        Deduper
	Session      SessionBinder
	Media        MediaResolver
	Audit        Auditor
	Replier      OutboundReplier
	Typing       TypingNotifier
	OriginType   string
}

type IssueCreator interface {
	Create(ctx context.Context, p service.IssueCreateParams, opts service.IssueCreateOpts) (service.IssueCreateResult, error)
}

type TaskEnqueuer interface {
	EnqueueChatTask(ctx context.Context, session db.ChatSession, initiatorUserID pgtype.UUID, forceFreshSession bool) (db.AgentTaskQueue, error)
	PromoteChannelChatTasksIfMediaReady(ctx context.Context, sessionID pgtype.UUID) error
}

type SessionReader interface {
	GetChatSession(ctx context.Context, id pgtype.UUID) (db.ChatSession, error)
	GetWorkspace(ctx context.Context, id pgtype.UUID) (db.Workspace, error)
}
