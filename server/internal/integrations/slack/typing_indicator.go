package slack

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/slack-go/slack"

	"github.com/adanman/goosar/server/internal/events"
	"github.com/adanman/goosar/server/internal/util"
	db "github.com/adanman/goosar/server/pkg/db/generated"
	"github.com/adanman/goosar/server/pkg/protocol"
)

const typingEmoji = "eyes"

const typingIndicatorMaxAge = 2 * time.Minute

type reactionAPI interface {
	AddReactionContext(ctx context.Context, name string, item slack.ItemRef) error
	RemoveReactionContext(ctx context.Context, name string, item slack.ItemRef) error
}

type typingState struct {
	ChannelID string
	MessageTS string
}

type TypingIndicatorQueries interface {
	GetChannelChatSessionBindingBySession(ctx context.Context, arg db.GetChannelChatSessionBindingBySessionParams) (db.ChannelChatSessionBinding, error)
	GetChannelInstallation(ctx context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error)
}

type TypingIndicatorManager struct {
	q       TypingIndicatorQueries
	decrypt Decrypter
	log     *slog.Logger
	newAPI  func(creds credentials) reactionAPI

	mu     sync.RWMutex
	states map[string][]typingState
}

func NewTypingIndicatorManager(q TypingIndicatorQueries, decrypt Decrypter, logger *slog.Logger) *TypingIndicatorManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &TypingIndicatorManager{
		q:       q,
		decrypt: decrypt,
		log:     logger,
		newAPI:  func(c credentials) reactionAPI { return slack.New(c.BotToken) },
		states:  make(map[string][]typingState),
	}
}

func (m *TypingIndicatorManager) Add(ctx context.Context, inst db.ChannelInstallation, sessionID pgtype.UUID, channelID, messageTS string) {
	if channelID == "" || messageTS == "" {
		return
	}
	if isMessageTooOld(messageTS) {
		m.log.Debug("slack typing indicator: message too old, skipping",
			"chat_session_id", util.UUIDToString(sessionID), "message_ts", messageTS)
		return
	}
	creds, err := decodeCredentials(inst.Config, m.decrypt)
	if err != nil {
		m.log.Warn("slack typing indicator: decode credentials failed",
			"chat_session_id", util.UUIDToString(sessionID), "err", err)
		return
	}
	if err := m.newAPI(creds).AddReactionContext(ctx, typingEmoji, slack.NewRefToMessage(channelID, messageTS)); err != nil {
		m.log.Warn("slack typing indicator: add reaction failed",
			"chat_session_id", util.UUIDToString(sessionID), "message_ts", messageTS, "err", err)
		return
	}
	key := util.UUIDToString(sessionID)
	m.mu.Lock()
	m.states[key] = append(m.states[key], typingState{ChannelID: channelID, MessageTS: messageTS})
	m.mu.Unlock()
}

func (m *TypingIndicatorManager) Clear(ctx context.Context, sessionID pgtype.UUID) {
	key := util.UUIDToString(sessionID)
	m.mu.Lock()
	states := m.states[key]
	delete(m.states, key)
	m.mu.Unlock()
	if len(states) == 0 {
		return
	}

	binding, err := m.q.GetChannelChatSessionBindingBySession(ctx, db.GetChannelChatSessionBindingBySessionParams{
		ChatSessionID: sessionID,
		ChannelType:   string(TypeSlack),
	})
	if err != nil {

		if !errors.Is(err, pgx.ErrNoRows) {
			m.log.Warn("slack typing indicator: lookup binding for clear failed",
				"chat_session_id", key, "err", err)
		}
		return
	}
	inst, err := m.q.GetChannelInstallation(ctx, db.GetChannelInstallationParams{
		ID:          binding.InstallationID,
		ChannelType: string(TypeSlack),
	})
	if err != nil {
		m.log.Warn("slack typing indicator: lookup installation for clear failed",
			"chat_session_id", key, "err", err)
		return
	}
	creds, err := decodeCredentials(inst.Config, m.decrypt)
	if err != nil {
		m.log.Warn("slack typing indicator: decode credentials for clear failed",
			"chat_session_id", key, "err", err)
		return
	}

	api := m.newAPI(creds)
	for _, s := range states {
		if err := api.RemoveReactionContext(ctx, typingEmoji, slack.NewRefToMessage(s.ChannelID, s.MessageTS)); err != nil {
			m.log.Warn("slack typing indicator: remove reaction failed",
				"chat_session_id", key, "message_ts", s.MessageTS, "err", err)
		}
	}
}

func (m *TypingIndicatorManager) Register(bus *events.Bus) {
	bus.Subscribe(protocol.EventChatDone, m.handleEvent)
	bus.Subscribe(protocol.EventTaskFailed, m.handleEvent)
}

func (m *TypingIndicatorManager) handleEvent(e events.Event) {
	sessionID, ok := chatSessionIDFromEvent(e)
	if !ok {

		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	m.Clear(ctx, sessionID)
}

func chatSessionIDFromEvent(e events.Event) (pgtype.UUID, bool) {
	if e.ChatSessionID != "" {
		if id, err := util.ParseUUID(e.ChatSessionID); err == nil && id.Valid {
			return id, true
		}
	}
	if m, ok := e.Payload.(map[string]any); ok {
		if s, _ := m["chat_session_id"].(string); s != "" {
			if id, err := util.ParseUUID(s); err == nil && id.Valid {
				return id, true
			}
		}
	}
	return pgtype.UUID{}, false
}

func isMessageTooOld(ts string) bool {
	if ts == "" {
		return false
	}
	secs, err := strconv.ParseFloat(ts, 64)
	if err != nil {
		return false
	}
	return time.Since(time.Unix(0, int64(secs*float64(time.Second)))) > typingIndicatorMaxAge
}
