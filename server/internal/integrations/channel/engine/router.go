package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/integrations/channel"
	"github.com/adanman/goosar/server/internal/service"
)

type Router struct {
	mu   sync.RWMutex
	sets map[channel.Type]ResolverSet

	issues IssueCreator
	tasks  TaskEnqueuer
	reader SessionReader

	batcher *pendingBatcher

	replyTimeout time.Duration
	mediaTimeout time.Duration
	mediaCtx     context.Context
	mediaCancel  context.CancelFunc
	mediaSem     chan struct{}
	replyWg      sync.WaitGroup
	mediaWg      sync.WaitGroup

	mediaQueueMu sync.Mutex
	mediaQueues  map[string]*mediaQueueEntry
	stopping     bool

	logger *slog.Logger

	pendingFreshMu sync.Mutex
	pendingFresh   map[string]bool
}

type RouterConfig struct {
	ReplyTimeout time.Duration

	MediaTimeout time.Duration

	MediaConcurrency int
	Logger           *slog.Logger
}

func NewRouter(issues IssueCreator, tasks TaskEnqueuer, reader SessionReader, cfg RouterConfig) *Router {
	if cfg.ReplyTimeout == 0 {
		cfg.ReplyTimeout = 2500 * time.Millisecond
	}
	if cfg.MediaTimeout == 0 {
		cfg.MediaTimeout = DefaultMediaTimeout
	}
	if cfg.MediaConcurrency == 0 {
		cfg.MediaConcurrency = 8
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	mediaCtx, mediaCancel := context.WithCancel(context.Background())
	return &Router{
		sets:         make(map[channel.Type]ResolverSet),
		issues:       issues,
		tasks:        tasks,
		reader:       reader,
		replyTimeout: cfg.ReplyTimeout,
		mediaTimeout: cfg.MediaTimeout,
		mediaCtx:     mediaCtx,
		mediaCancel:  mediaCancel,
		mediaSem:     make(chan struct{}, cfg.MediaConcurrency),
		logger:       cfg.Logger,
		pendingFresh: make(map[string]bool),
		mediaQueues:  make(map[string]*mediaQueueEntry),
	}
}

const DefaultMediaTimeout = 45 * time.Second

type mediaQueueEntry struct {
	tail chan struct{}
}

func (r *Router) Register(t channel.Type, set ResolverSet) {
	if t == "" || set.Installation == nil || set.Identity == nil || set.Dedup == nil || set.Session == nil || set.Audit == nil {
		r.logger.Warn("channel router: ignoring incomplete resolver set", "channel_type", string(t))
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sets[t] = set
}

func (r *Router) EnableRunBatching(window time.Duration) {
	r.batcher = newPendingBatcher(window)
}

func (r *Router) Drain(ctx context.Context) bool {
	r.mediaQueueMu.Lock()
	r.stopping = true
	r.mediaCancel()
	r.mediaQueueMu.Unlock()

	done := make(chan struct{})
	go func() {
		if r.batcher != nil {
			r.batcher.FlushAll()
		}
		r.mediaWg.Wait()
		r.replyWg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-ctx.Done():
		return false
	}
}

var ErrNoResolverSet = errors.New("channel router: no resolver set for channel type")

func (r *Router) Handle(ctx context.Context, msg channel.InboundMessage) error {
	r.mu.RLock()
	set, ok := r.sets[msg.Source.ChannelType]
	r.mu.RUnlock()
	if !ok {
		r.logger.Error("channel router: no resolver set", "channel_type", string(msg.Source.ChannelType))
		return ErrNoResolverSet
	}

	res, inst, err := r.dispatch(ctx, set, msg)
	if err != nil {
		r.logger.Error("channel router: dispatch error",
			"channel_type", string(msg.Source.ChannelType),
			"event_id", msg.EventID,
			"error", err,
		)
		return err
	}
	r.logger.Debug("channel router: dispatch outcome",
		"channel_type", string(msg.Source.ChannelType),
		"event_id", msg.EventID,
		"outcome", string(res.Outcome),
		"drop_reason", string(res.DropReason),
	)

	if res.Outcome == OutcomeIngested && set.Typing != nil {
		go func() {
			tctx, cancel := context.WithTimeout(context.Background(), r.replyTimeout)
			defer cancel()
			set.Typing.OnIngested(tctx, inst, msg, res.ChatSessionID)
		}()
	}
	r.scheduleReply(set, inst, msg, res)
	return nil
}

func (r *Router) dispatch(ctx context.Context, set ResolverSet, msg channel.InboundMessage) (Result, ResolvedInstallation, error) {

	inst, err := set.Installation.ResolveInstallation(ctx, msg)
	if err != nil {
		if errors.Is(err, ErrInstallationNotFound) {
			_ = set.Audit.RecordDrop(ctx, pgtype.UUID{}, msg, DropReasonInvalidEvent)
			return Result{Outcome: OutcomeDropped, DropReason: DropReasonInvalidEvent}, ResolvedInstallation{}, nil
		}
		return Result{}, ResolvedInstallation{}, fmt.Errorf("resolve installation: %w", err)
	}
	if !inst.Active {
		return r.drop(ctx, set, msg, inst.ID, DropReasonRevokedInstallation), inst, nil
	}

	var claimToken pgtype.UUID
	claimed := false
	if msg.MessageID != "" {
		token, err := set.Dedup.Claim(ctx, inst.ID, msg.MessageID)
		if err != nil {
			if errors.Is(err, ErrDuplicate) {
				return r.drop(ctx, set, msg, inst.ID, DropReasonDuplicate), inst, nil
			}
			return Result{}, inst, fmt.Errorf("dedup claim: %w", err)
		}
		claimToken = token
		claimed = true
	}

	res, finalize, err := r.processClaimed(ctx, set, msg, inst, claimToken)

	if claimed {
		r.applyFinalize(ctx, set, inst.ID, msg.MessageID, claimToken, finalize)
	}

	if errors.Is(err, ErrClaimLost) {
		return r.drop(ctx, set, msg, inst.ID, DropReasonDuplicate), inst, nil
	}
	return res, inst, err
}

type dedupFinalize int

const (
	finalizeNone dedupFinalize = iota
	finalizeMark
	finalizeRelease
)

func (r *Router) processClaimed(ctx context.Context, set ResolverSet, msg channel.InboundMessage, inst ResolvedInstallation, claimToken pgtype.UUID) (Result, dedupFinalize, error) {

	if msg.Source.ChatType == channel.ChatTypeGroup && !msg.AddressedToBot {
		return r.drop(ctx, set, msg, inst.ID, DropReasonNotAddressedInGroup), finalizeMark, nil
	}

	identity, err := set.Identity.ResolveSender(ctx, inst, msg)
	if err != nil {
		switch {
		case errors.Is(err, ErrSenderUnbound):
			_ = set.Audit.RecordDrop(ctx, inst.ID, msg, DropReasonUnboundUser)
			return Result{
				Outcome:        OutcomeNeedsBinding,
				DropReason:     DropReasonUnboundUser,
				InstallationID: inst.ID,
				Sender:         msg.Source.SenderID,
			}, finalizeMark, nil
		case errors.Is(err, ErrSenderNotMember):
			return r.drop(ctx, set, msg, inst.ID, DropReasonNonWorkspaceMember), finalizeMark, nil
		default:
			return Result{}, finalizeRelease, fmt.Errorf("resolve sender: %w", err)
		}
	}

	sessionCreator := identity.UserID
	if msg.Source.ChatType == channel.ChatTypeGroup {
		sessionCreator = inst.InstallerUserID
	}
	sessionID, err := set.Session.EnsureSession(ctx, EnsureSessionParams{
		Installation: inst,
		Sender:       sessionCreator,
		Message:      msg,
	})
	if err != nil {

		return Result{}, finalizeRelease, fmt.Errorf("ensure chat session: %w", err)
	}

	mediaPendingSeconds := 0.0
	resolveMedia := set.Media != nil && set.Media.HasMedia(msg)

	localMediaDeadline := time.Now().Add(r.mediaTimeout)
	if resolveMedia {
		mediaPendingSeconds = r.mediaTimeout.Seconds()
	}
	appendRes, err := set.Session.AppendMessage(ctx, AppendParams{
		SessionID:           sessionID,
		Sender:              identity.UserID,
		InstallationID:      inst.ID,
		Message:             msg,
		ClaimToken:          claimToken,
		MediaPendingSeconds: mediaPendingSeconds,
	})
	if err != nil {
		if errors.Is(err, ErrClaimLost) {
			return Result{}, finalizeNone, err
		}
		return Result{}, finalizeRelease, fmt.Errorf("append user message: %w", err)
	}

	postAppendFinalize := finalizeNone
	if !appendRes.DedupMarked {
		postAppendFinalize = finalizeMark
	}

	res := Result{
		Outcome:        OutcomeIngested,
		InstallationID: inst.ID,
		ChatSessionID:  sessionID,
		Sender:         msg.Source.SenderID,
	}

	if appendRes.IssueCommand != nil {
		issueRes, err := r.createIssue(ctx, inst, set.OriginType, identity.UserID, sessionID, *appendRes.IssueCommand)
		if err != nil {
			return Result{}, postAppendFinalize, fmt.Errorf("create issue from command: %w", err)
		}
		res.IssueID = issueRes.Issue.ID
		res.IssueNumber = issueRes.Issue.Number
		res.IssueTitle = issueRes.Issue.Title
		if ws, werr := r.reader.GetWorkspace(ctx, inst.WorkspaceID); werr == nil && ws.IssuePrefix != "" {
			res.IssueIdentifier = fmt.Sprintf("%s-%d", ws.IssuePrefix, issueRes.Issue.Number)
		} else {
			res.IssueIdentifier = fmt.Sprintf("#%d", issueRes.Issue.Number)
		}
	}

	r.scheduleRun(set, inst, msg, sessionID, identity.UserID)
	if resolveMedia {
		r.enqueueMedia(set, inst, identity, appendRes.MessageID, msg, sessionID, localMediaDeadline)
	}
	return res, postAppendFinalize, nil
}

func (r *Router) enqueueMedia(set ResolverSet, inst ResolvedInstallation, identity ResolvedIdentity, chatMessageID pgtype.UUID, msg channel.InboundMessage, sessionID pgtype.UUID, deadline time.Time) {
	key := keyForSession(sessionID)
	done := make(chan struct{})

	r.mediaQueueMu.Lock()
	if r.stopping {
		r.mediaQueueMu.Unlock()
		return
	}
	entry, ok := r.mediaQueues[key]
	var previous <-chan struct{}
	if !ok {
		entry = &mediaQueueEntry{}
		r.mediaQueues[key] = entry
	} else {
		previous = entry.tail
	}
	entry.tail = done
	r.mediaWg.Add(1)
	r.mediaQueueMu.Unlock()

	go func() {
		defer r.mediaWg.Done()
		defer close(done)
		defer r.finishMediaQueue(key, done)

		expiry := time.NewTimer(time.Until(deadline))
		defer expiry.Stop()
		expired := false
		if previous != nil {
			select {
			case <-previous:
			case <-r.mediaCtx.Done():
			case <-expiry.C:
				expired = true
			}
		}
		if !expired {
			select {
			case r.mediaSem <- struct{}{}:
				defer func() { <-r.mediaSem }()
			case <-r.mediaCtx.Done():

			case <-expiry.C:

			}
		}
		r.resolveAndBindMedia(set, inst, identity, chatMessageID, msg, sessionID, deadline)
	}()
}

const mediaFinalizeTimeout = 5 * time.Second

func (r *Router) resolveAndBindMedia(set ResolverSet, inst ResolvedInstallation, identity ResolvedIdentity, chatMessageID pgtype.UUID, msg channel.InboundMessage, sessionID pgtype.UUID, deadline time.Time) {
	ctx, cancel := context.WithDeadline(r.mediaCtx, deadline)
	defer cancel()

	resolved := msg
	if ctx.Err() == nil {

		resolved = set.Media.ResolveMedia(ctx, inst, identity, sessionID, chatMessageID, msg)
	}
	finalizeCtx, finalizeCancel := context.WithTimeout(context.Background(), mediaFinalizeTimeout)
	defer finalizeCancel()
	if err := ctx.Err(); err != nil {

		resolved.MediaRefs = nil
		r.logger.Warn("channel router: media resolution incomplete; using placeholder",
			"channel_type", string(msg.Source.ChannelType),
			"event_id", msg.EventID,
			"message_id", msg.MessageID,
			"error", err)
	}
	if err := set.Session.BindMedia(finalizeCtx, BindMediaParams{
		MessageID:   chatMessageID,
		SessionID:   sessionID,
		WorkspaceID: inst.WorkspaceID,
		Sender:      identity.UserID,
		MediaRefs:   resolved.MediaRefs,
	}); err != nil {

		r.logger.Warn("channel router: media attachment binding failed",
			"channel_type", string(msg.Source.ChannelType),
			"event_id", msg.EventID,
			"message_id", msg.MessageID,
			"err", err)
	}
	if err := r.tasks.PromoteChannelChatTasksIfMediaReady(finalizeCtx, sessionID); err != nil {
		r.logger.Warn("channel router: media-ready task promotion failed",
			"channel_type", string(msg.Source.ChannelType),
			"event_id", msg.EventID,
			"message_id", msg.MessageID,
			"err", err)
	}
}

func (r *Router) finishMediaQueue(key string, done chan struct{}) {
	r.mediaQueueMu.Lock()
	defer r.mediaQueueMu.Unlock()
	entry, ok := r.mediaQueues[key]
	if !ok || entry.tail != done {
		return
	}
	delete(r.mediaQueues, key)
}

func (r *Router) scheduleRun(set ResolverSet, inst ResolvedInstallation, msg channel.InboundMessage, sessionID, initiatorUserID pgtype.UUID) {
	key := keyForSession(sessionID)
	fresh := msg.ForceFresh
	if r.batcher == nil {
		r.flushChatRun(set, inst, msg, sessionID, initiatorUserID, fresh)
		return
	}
	if fresh {
		r.markPendingFresh(key)
	}
	flush := func() {
		r.flushChatRun(set, inst, msg, sessionID, initiatorUserID, r.takePendingFresh(key, fresh))
	}
	r.batcher.Schedule(key, flush)
}

const chatRunFlushTimeout = 10 * time.Second

func (r *Router) flushChatRun(set ResolverSet, inst ResolvedInstallation, msg channel.InboundMessage, sessionID, initiatorUserID pgtype.UUID, forceFresh bool) {
	ctx, cancel := context.WithTimeout(context.Background(), chatRunFlushTimeout)
	defer cancel()

	session, err := r.reader.GetChatSession(ctx, sessionID)
	if err != nil {
		r.logger.Error("channel router: flush reload chat session failed",
			"chat_session_id", uuidString(sessionID), "err", err.Error())
		r.clearTyping(ctx, set, sessionID)
		return
	}
	if _, err := r.tasks.EnqueueChatTask(ctx, session, initiatorUserID, forceFresh); err != nil {

		r.clearTyping(ctx, set, sessionID)
		switch {
		case errors.Is(err, service.ErrChatTaskAgentNoRuntime):
			r.emitFlushReply(ctx, set, inst, msg, sessionID, OutcomeAgentOffline)
		case errors.Is(err, service.ErrChatTaskAgentArchived):
			r.emitFlushReply(ctx, set, inst, msg, sessionID, OutcomeAgentArchived)
		default:
			r.logger.Error("channel router: flush enqueue chat task failed",
				"chat_session_id", uuidString(sessionID), "err", err.Error())
		}
	}
}

func (r *Router) clearTyping(ctx context.Context, set ResolverSet, sessionID pgtype.UUID) {
	if set.Typing != nil {
		set.Typing.OnSettled(ctx, sessionID)
	}
}

func (r *Router) markPendingFresh(key string) {
	r.pendingFreshMu.Lock()
	defer r.pendingFreshMu.Unlock()
	r.pendingFresh[key] = true
}

func (r *Router) takePendingFresh(key string, fallback bool) bool {
	r.pendingFreshMu.Lock()
	defer r.pendingFreshMu.Unlock()
	fresh := fallback || r.pendingFresh[key]
	delete(r.pendingFresh, key)
	return fresh
}

func (r *Router) emitFlushReply(ctx context.Context, set ResolverSet, inst ResolvedInstallation, msg channel.InboundMessage, sessionID pgtype.UUID, outcome Outcome) {
	if set.Replier == nil {
		return
	}
	set.Replier.Reply(ctx, inst, msg, Result{
		Outcome:        outcome,
		InstallationID: inst.ID,
		ChatSessionID:  sessionID,
		Sender:         msg.Source.SenderID,
	})
}

func (r *Router) scheduleReply(set ResolverSet, inst ResolvedInstallation, msg channel.InboundMessage, res Result) {
	if set.Replier == nil {
		return
	}
	r.replyWg.Add(1)
	go func() {
		defer r.replyWg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), r.replyTimeout)
		defer cancel()
		set.Replier.Reply(ctx, inst, msg, res)
		if ctx.Err() == context.DeadlineExceeded {
			r.logger.Warn("channel router: outbound reply timed out",
				"event_id", msg.EventID, "outcome", string(res.Outcome),
				"timeout", r.replyTimeout.String())
		}
	}()
}

func keyForSession(sessionID pgtype.UUID) string {
	return string(sessionID.Bytes[:])
}

func (r *Router) applyFinalize(ctx context.Context, set ResolverSet, instID pgtype.UUID, messageID string, claimToken pgtype.UUID, action dedupFinalize) {
	switch action {
	case finalizeMark:
		_ = set.Dedup.Mark(ctx, instID, messageID, claimToken)
	case finalizeRelease:
		_ = set.Dedup.Release(ctx, instID, messageID, claimToken)
	case finalizeNone:
	}
}

func (r *Router) drop(ctx context.Context, set ResolverSet, msg channel.InboundMessage, instID pgtype.UUID, reason DropReason) Result {
	_ = set.Audit.RecordDrop(ctx, instID, msg, reason)
	return Result{Outcome: OutcomeDropped, DropReason: reason, InstallationID: instID}
}

func (r *Router) createIssue(ctx context.Context, inst ResolvedInstallation, originType string, creatorUserID, sessionID pgtype.UUID, cmd IssueCommand) (service.IssueCreateResult, error) {
	if cmd.Title == "" {
		return service.IssueCreateResult{}, ErrEmptyIssueTitle
	}
	params := service.IssueCreateParams{
		WorkspaceID:  inst.WorkspaceID,
		Title:        cmd.Title,
		Description:  pgtype.Text{String: cmd.Description, Valid: cmd.Description != ""},
		Status:       "todo",
		Priority:     "none",
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
		AssigneeID:   inst.AgentID,
		CreatorType:  "member",
		CreatorID:    creatorUserID,
		OriginType:   pgtype.Text{String: originType, Valid: originType != ""},
		OriginID:     sessionID,
	}
	return r.issues.Create(ctx, params, service.IssueCreateOpts{})
}

var ErrEmptyIssueTitle = errors.New("issue title is empty")

var _ channel.InboundHandler = (*Router)(nil).Handle
