package slack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/adanman/goosar/server/internal/integrations/channel"
	"github.com/adanman/goosar/server/internal/integrations/channel/engine"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

const originSlackChat = "slack_chat"

func NewSlackResolverSet(q *db.Queries, tx engine.TxStarter, replier engine.OutboundReplier, typing *TypingIndicatorManager) engine.ResolverSet {
	set := engine.ResolverSet{
		Installation: &installationResolver{q: q},
		Identity:     &identityResolver{q: q},
		Dedup:        &deduper{q: q},
		Session: &sessionBinder{session: engine.NewChatSession(q, tx, TypeSlack, engine.SessionTitles{
			Group:    "Slack channel",
			Direct:   "Slack direct message",
			Fallback: "Slack chat",
		})},
		Audit:      &auditor{q: q},
		Replier:    replier,
		OriginType: originSlackChat,
	}

	if typing != nil {
		set.Typing = &slackTypingNotifier{mgr: typing}
	}
	return set
}

var (
	_ engine.InstallationResolver = (*installationResolver)(nil)
	_ engine.IdentityResolver     = (*identityResolver)(nil)
	_ engine.Deduper              = (*deduper)(nil)
	_ engine.SessionBinder        = (*sessionBinder)(nil)
	_ engine.Auditor              = (*auditor)(nil)
	_ engine.TypingNotifier       = (*slackTypingNotifier)(nil)
)

type slackBindingConfig struct {
	ChannelID string `json:"channel_id"`
}

func slackSessionRouting(msg channel.InboundMessage) (bindingKey string, config []byte, replyThread string) {
	chatID := msg.Source.ChatID
	cfg, _ := json.Marshal(slackBindingConfig{ChannelID: chatID})
	if msg.Source.ChatType == channel.ChatTypeP2P {
		return chatID, cfg, msg.Source.ThreadID
	}

	threadRoot := msg.Source.ThreadID
	if threadRoot == "" {
		threadRoot = msg.MessageID
	}
	return chatID + ":" + threadRoot, cfg, threadRoot
}

func decodeSlackRaw(msg channel.InboundMessage) (slackRawEvent, error) {
	var raw slackRawEvent
	if len(msg.Raw) == 0 {
		return slackRawEvent{}, errors.New("slack: inbound message Raw is empty")
	}
	if err := json.Unmarshal(msg.Raw, &raw); err != nil {
		return slackRawEvent{}, fmt.Errorf("decode slack inbound raw: %w", err)
	}
	return raw, nil
}

func nullText(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

func installTeamID(installConfigJSON json.RawMessage) string {
	var cfg installConfig
	_ = json.Unmarshal(installConfigJSON, &cfg)
	return cfg.TeamID
}

func installationServesTeam(installConfigJSON json.RawMessage, eventTeamID string) bool {
	teamID := installTeamID(installConfigJSON)
	return teamID == "" || teamID == eventTeamID
}

type installationResolver struct{ q *db.Queries }

func (r *installationResolver) ResolveInstallation(ctx context.Context, msg channel.InboundMessage) (engine.ResolvedInstallation, error) {
	raw, err := decodeSlackRaw(msg)
	if err != nil {
		return engine.ResolvedInstallation{}, err
	}
	inst, err := r.q.GetChannelInstallationByAppID(ctx, db.GetChannelInstallationByAppIDParams{
		ChannelType: string(TypeSlack),

		AppID: raw.APIAppID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return engine.ResolvedInstallation{}, engine.ErrInstallationNotFound
		}
		return engine.ResolvedInstallation{}, err
	}
	if !installationServesTeam(inst.Config, raw.TeamID) {
		return engine.ResolvedInstallation{}, engine.ErrInstallationNotFound
	}
	return engine.ResolvedInstallation{
		ID:              inst.ID,
		WorkspaceID:     inst.WorkspaceID,
		AgentID:         inst.AgentID,
		InstallerUserID: inst.InstallerUserID,
		Active:          inst.Status == "active",
		Platform:        inst,
	}, nil
}

type identityQueries interface {
	GetChannelUserBindingByUserID(ctx context.Context, arg db.GetChannelUserBindingByUserIDParams) (db.ChannelUserBinding, error)
	FindReusableChannelUserBinding(ctx context.Context, arg db.FindReusableChannelUserBindingParams) (db.ChannelUserBinding, error)
	GetMemberByUserAndWorkspace(ctx context.Context, arg db.GetMemberByUserAndWorkspaceParams) (db.Member, error)
	CreateChannelUserBinding(ctx context.Context, arg db.CreateChannelUserBindingParams) (db.ChannelUserBinding, error)
}

type identityResolver struct{ q identityQueries }

func (r *identityResolver) ResolveSender(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage) (engine.ResolvedIdentity, error) {
	senderID := msg.Source.SenderID
	binding, err := r.q.GetChannelUserBindingByUserID(ctx, db.GetChannelUserBindingByUserIDParams{
		InstallationID: inst.ID,
		ChannelUserID:  senderID,
	})
	reused := false
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return engine.ResolvedIdentity{}, err
		}

		cand, ok, ferr := r.reusableBinding(ctx, inst, senderID)
		if ferr != nil {
			return engine.ResolvedIdentity{}, ferr
		}
		if !ok {
			return engine.ResolvedIdentity{}, engine.ErrSenderUnbound
		}
		binding, reused = cand, true
	}

	if _, err := r.q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      binding.GoosarUserID,
		WorkspaceID: inst.WorkspaceID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			if reused {

				return engine.ResolvedIdentity{}, engine.ErrSenderUnbound
			}
			return engine.ResolvedIdentity{}, engine.ErrSenderNotMember
		}
		return engine.ResolvedIdentity{}, err
	}
	if reused {

		if _, err := r.q.CreateChannelUserBinding(ctx, db.CreateChannelUserBindingParams{
			WorkspaceID:    inst.WorkspaceID,
			GoosarUserID:   binding.GoosarUserID,
			InstallationID: inst.ID,
			ChannelType:    string(TypeSlack),
			ChannelUserID:  senderID,
			Config:         []byte(`{}`),
		}); err != nil {
			return engine.ResolvedIdentity{}, fmt.Errorf("materialize reused slack binding: %w", err)
		}
	}
	return engine.ResolvedIdentity{UserID: binding.GoosarUserID}, nil
}

func (r *identityResolver) reusableBinding(ctx context.Context, inst engine.ResolvedInstallation, senderID string) (db.ChannelUserBinding, bool, error) {
	ci, ok := inst.Platform.(db.ChannelInstallation)
	if !ok {
		return db.ChannelUserBinding{}, false, nil
	}
	teamID := installTeamID(ci.Config)
	if teamID == "" {
		return db.ChannelUserBinding{}, false, nil
	}
	cand, err := r.q.FindReusableChannelUserBinding(ctx, db.FindReusableChannelUserBindingParams{
		WorkspaceID:   inst.WorkspaceID,
		ChannelType:   string(TypeSlack),
		ChannelUserID: senderID,
		TeamID:        teamID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.ChannelUserBinding{}, false, nil
		}
		return db.ChannelUserBinding{}, false, err
	}
	return cand, true, nil
}

type deduper struct{ q *db.Queries }

func (r *deduper) Claim(ctx context.Context, installationID pgtype.UUID, messageID string) (pgtype.UUID, error) {
	claim, err := r.q.ClaimChannelInboundDedup(ctx, db.ClaimChannelInboundDedupParams{
		InstallationID: installationID,
		MessageID:      messageID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return pgtype.UUID{}, engine.ErrDuplicate
		}
		return pgtype.UUID{}, err
	}
	return claim.ClaimToken, nil
}

func (r *deduper) Mark(ctx context.Context, installationID pgtype.UUID, messageID string, claimToken pgtype.UUID) error {
	_, err := r.q.MarkChannelInboundDedupProcessed(ctx, db.MarkChannelInboundDedupProcessedParams{
		InstallationID: installationID,
		MessageID:      messageID,
		ClaimToken:     claimToken,
	})
	return err
}

func (r *deduper) Release(ctx context.Context, installationID pgtype.UUID, messageID string, claimToken pgtype.UUID) error {
	_, err := r.q.ReleaseChannelInboundDedup(ctx, db.ReleaseChannelInboundDedupParams{
		InstallationID: installationID,
		MessageID:      messageID,
		ClaimToken:     claimToken,
	})
	return err
}

type sessionBinder struct{ session *engine.ChatSession }

func (r *sessionBinder) EnsureSession(ctx context.Context, p engine.EnsureSessionParams) (pgtype.UUID, error) {
	bindingKey, config, _ := slackSessionRouting(p.Message)
	return r.session.EnsureSession(ctx, engine.EnsureSessionInput{
		WorkspaceID:    p.Installation.WorkspaceID,
		AgentID:        p.Installation.AgentID,
		InstallationID: p.Installation.ID,
		Sender:         p.Sender,
		BindingKey:     bindingKey,
		BindingConfig:  config,
		ChatType:       p.Message.Source.ChatType,
	})
}

func (r *sessionBinder) AppendMessage(ctx context.Context, p engine.AppendParams) (engine.AppendResult, error) {
	_, _, replyThread := slackSessionRouting(p.Message)
	return r.session.AppendUserMessage(ctx, engine.AppendInput{
		SessionID:      p.SessionID,
		Sender:         p.Sender,
		InstallationID: p.InstallationID,
		Body:           p.Message.Text,

		CommandText:         p.Message.Text,
		MessageID:           p.Message.MessageID,
		ThreadID:            replyThread,
		ClaimToken:          p.ClaimToken,
		MediaPendingSeconds: p.MediaPendingSeconds,
	})
}

func (r *sessionBinder) BindMedia(ctx context.Context, p engine.BindMediaParams) error {
	return r.session.BindMediaRefs(ctx, engine.BindMediaInput{
		MessageID:   p.MessageID,
		SessionID:   p.SessionID,
		WorkspaceID: p.WorkspaceID,
		Sender:      p.Sender,
		MediaRefs:   p.MediaRefs,
	})
}

type auditor struct{ q *db.Queries }

func (r *auditor) RecordDrop(ctx context.Context, instID pgtype.UUID, msg channel.InboundMessage, reason engine.DropReason) error {
	raw, _ := decodeSlackRaw(msg)
	return r.q.RecordChannelInboundDrop(ctx, db.RecordChannelInboundDropParams{
		ChannelType:      string(TypeSlack),
		EventType:        raw.EventType,
		DropReason:       string(reason),
		InstallationID:   instID,
		ChannelChatID:    nullText(msg.Source.ChatID),
		ChannelEventID:   nullText(msg.EventID),
		ChannelMessageID: nullText(msg.MessageID),
	})
}

type slackTypingNotifier struct{ mgr *TypingIndicatorManager }

func (n *slackTypingNotifier) OnIngested(ctx context.Context, inst engine.ResolvedInstallation, msg channel.InboundMessage, sessionID pgtype.UUID) {
	ci, ok := inst.Platform.(db.ChannelInstallation)
	if !ok {
		return
	}
	n.mgr.Add(ctx, ci, sessionID, msg.Source.ChatID, msg.MessageID)
}

func (n *slackTypingNotifier) OnSettled(ctx context.Context, sessionID pgtype.UUID) {
	n.mgr.Clear(ctx, sessionID)
}
