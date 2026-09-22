package slack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/slack-go/slack"

	"github.com/adanman/goosar/server/internal/integrations/channel"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

var ErrNoSlackSession = errors.New("slack: session has no slack channel binding")

const (
	defaultHistoryLimit = 20

	maxHistoryLimit = 50
)

type historyQueries interface {
	GetChannelChatSessionBindingBySession(ctx context.Context, arg db.GetChannelChatSessionBindingBySessionParams) (db.ChannelChatSessionBinding, error)
	GetChannelInstallation(ctx context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error)
}

type historyClient interface {
	GetConversationHistoryContext(ctx context.Context, params *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error)
	GetConversationRepliesContext(ctx context.Context, params *slack.GetConversationRepliesParameters) ([]slack.Message, bool, string, error)
	GetUsersInfoContext(ctx context.Context, users ...string) (*[]slack.User, error)
}

type History struct {
	q         historyQueries
	decrypt   Decrypter
	logger    *slog.Logger
	newClient func(botToken string) historyClient
}

func NewHistory(q historyQueries, decrypt Decrypter, logger *slog.Logger) *History {
	if logger == nil {
		logger = slog.Default()
	}
	h := &History{q: q, decrypt: decrypt, logger: logger}
	h.newClient = func(botToken string) historyClient {

		return slack.New(botToken)
	}
	return h
}

type slackTarget struct {
	client     historyClient
	channelID  string
	threadRoot string
	botUserID  string
}

func (h *History) resolve(ctx context.Context, chatSessionID pgtype.UUID) (slackTarget, error) {
	binding, err := h.q.GetChannelChatSessionBindingBySession(ctx, db.GetChannelChatSessionBindingBySessionParams{
		ChatSessionID: chatSessionID,
		ChannelType:   string(TypeSlack),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return slackTarget{}, ErrNoSlackSession
		}
		return slackTarget{}, fmt.Errorf("lookup slack chat binding: %w", err)
	}
	inst, err := h.q.GetChannelInstallation(ctx, db.GetChannelInstallationParams{
		ID:          binding.InstallationID,
		ChannelType: string(TypeSlack),
	})
	if err != nil {
		return slackTarget{}, fmt.Errorf("load slack installation: %w", err)
	}
	if inst.Status != "active" {
		return slackTarget{}, ErrNoSlackSession
	}
	creds, err := decodeCredentials(inst.Config, h.decrypt)
	if err != nil {
		return slackTarget{}, fmt.Errorf("decode slack credentials: %w", err)
	}
	channelID, threadRoot := historyTarget(binding)
	return slackTarget{
		client:     h.newClient(creds.BotToken),
		channelID:  channelID,
		threadRoot: threadRoot,
		botUserID:  creds.BotUserID,
	}, nil
}

func (h *History) ChannelOverview(ctx context.Context, chatSessionID pgtype.UUID, opts channel.HistoryOptions) (channel.HistoryPage, error) {
	t, err := h.resolve(ctx, chatSessionID)
	if err != nil {
		return channel.HistoryPage{}, err
	}
	limit := clampHistoryLimit(opts.Limit)
	resp, err := t.client.GetConversationHistoryContext(ctx, &slack.GetConversationHistoryParameters{
		ChannelID: t.channelID,
		Latest:    opts.Before,
		Inclusive: false,
		Limit:     limit,
	})
	if err != nil {
		return channel.HistoryPage{}, fmt.Errorf("read slack channel: %w", err)
	}
	page := normalizePage(ctx, t.client, h.logger, resp.Messages, t.botUserID, limit, true)
	page.ChannelType = string(TypeSlack)
	return page, nil
}

func (h *History) Thread(ctx context.Context, chatSessionID pgtype.UUID, threadID string, opts channel.HistoryOptions) (channel.HistoryPage, error) {
	t, err := h.resolve(ctx, chatSessionID)
	if err != nil {
		return channel.HistoryPage{}, err
	}
	limit := clampHistoryLimit(opts.Limit)
	ts := threadID
	if ts == "" {
		ts = t.threadRoot
	}

	var raw []slack.Message
	if ts == "" {

		resp, herr := t.client.GetConversationHistoryContext(ctx, &slack.GetConversationHistoryParameters{
			ChannelID: t.channelID,
			Latest:    opts.Before,
			Inclusive: false,
			Limit:     limit,
		})
		if herr != nil {
			return channel.HistoryPage{}, fmt.Errorf("read slack thread: %w", herr)
		}
		raw = resp.Messages
	} else {
		msgs, _, _, rerr := t.client.GetConversationRepliesContext(ctx, &slack.GetConversationRepliesParameters{
			ChannelID: t.channelID,
			Timestamp: ts,
			Latest:    opts.Before,
			Inclusive: false,
			Limit:     limit,
		})
		if rerr != nil {
			return channel.HistoryPage{}, fmt.Errorf("read slack thread: %w", rerr)
		}
		raw = msgs
	}
	page := normalizePage(ctx, t.client, h.logger, raw, t.botUserID, limit, false)
	page.ChannelType = string(TypeSlack)
	page.ThreadID = ts
	return page, nil
}

func clampHistoryLimit(n int) int {
	if n <= 0 {
		return defaultHistoryLimit
	}
	if n > maxHistoryLimit {
		return maxHistoryLimit
	}
	return n
}

func historyTarget(b db.ChannelChatSessionBinding) (channelID, threadRoot string) {
	channelID = b.ChannelChatID
	if len(b.Config) > 0 {
		var cfg slackBindingConfig
		if err := json.Unmarshal(b.Config, &cfg); err == nil && cfg.ChannelID != "" {
			channelID = cfg.ChannelID
		}
	}
	if b.LastThreadID.Valid && b.LastThreadID.String != "" {
		threadRoot = b.LastThreadID.String
	} else if i := strings.IndexByte(b.ChannelChatID, ':'); i >= 0 {
		threadRoot = b.ChannelChatID[i+1:]
	}
	return channelID, threadRoot
}

func normalizePage(ctx context.Context, client historyClient, logger *slog.Logger, raw []slack.Message, botUserID string, limit int, overview bool) channel.HistoryPage {
	sort.SliceStable(raw, func(i, j int) bool { return slackTSLess(raw[i].Timestamp, raw[j].Timestamp) })

	names := resolveUserNames(ctx, client, logger, raw, botUserID)
	labeler := newHistoryLabeler(names)

	out := make([]channel.HistoryMessage, 0, len(raw))
	for i := range raw {
		m := raw[i]
		text := flattenSlackText(m)
		if text == "" {
			continue
		}
		own := m.User != "" && m.User == botUserID
		role := channel.HistoryRoleUser
		if own {
			role = channel.HistoryRoleAssistant
		}
		hm := channel.HistoryMessage{
			ID:       m.Timestamp,
			Author:   labeler.label(m, own),
			AuthorID: m.User,
			Role:     role,
			Text:     text,
			TS:       m.Timestamp,
		}
		if overview && m.ReplyCount > 0 {
			hm.ThreadID = m.Timestamp
			hm.ReplyCount = m.ReplyCount
			hm.LatestReply = m.LatestReply
		}
		out = append(out, hm)
	}

	page := channel.HistoryPage{Messages: out}

	if len(raw) >= limit && len(out) > 0 {
		page.NextCursor = out[0].TS
	}
	return page
}

const maxDerivedTextLen = 4000

func flattenSlackText(m slack.Message) string {
	if t := strings.TrimSpace(m.Text); t != "" {
		return t
	}
	parts := make([]string, 0, len(m.Attachments)+1)
	for i := range m.Attachments {
		if t := attachmentText(m.Attachments[i]); t != "" {
			parts = append(parts, t)
		}
	}
	if len(parts) == 0 {
		if t := flattenBlocks(m.Blocks); t != "" {
			parts = append(parts, t)
		}
	}
	return truncateRunes(strings.TrimSpace(strings.Join(parts, "\n")), maxDerivedTextLen)
}

func attachmentText(a slack.Attachment) string {
	parts := make([]string, 0, 3+len(a.Fields))
	for _, s := range []string{a.Pretext, a.Title, a.Text} {
		if s = strings.TrimSpace(s); s != "" {
			parts = append(parts, s)
		}
	}
	for _, f := range a.Fields {
		if s := strings.TrimSpace(f.Title + " " + f.Value); s != "" {
			parts = append(parts, s)
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, "\n")
	}
	if t := strings.TrimSpace(a.Fallback); t != "" {
		return t
	}
	return flattenBlocks(a.Blocks)
}

func flattenBlocks(blocks slack.Blocks) string {
	parts := make([]string, 0, len(blocks.BlockSet))
	add := func(s string) {
		if s = strings.TrimSpace(s); s != "" {
			parts = append(parts, s)
		}
	}
	for _, b := range blocks.BlockSet {
		switch v := b.(type) {
		case *slack.SectionBlock:
			if v.Text != nil {
				add(v.Text.Text)
			}
			for _, f := range v.Fields {
				if f != nil {
					add(f.Text)
				}
			}
		case *slack.HeaderBlock:
			if v.Text != nil {
				add(v.Text.Text)
			}
		case *slack.MarkdownBlock:
			add(v.Text)
		case *slack.ContextBlock:
			for _, el := range v.ContextElements.Elements {
				if tb, ok := el.(*slack.TextBlockObject); ok {
					add(tb.Text)
				}
			}
		case *slack.RichTextBlock:
			add(richTextBlockText(v))
		}
	}
	return strings.Join(parts, "\n")
}

func richTextBlockText(b *slack.RichTextBlock) string {
	var lines []string
	var writeElement func(el slack.RichTextElement)
	writeSection := func(els []slack.RichTextSectionElement) {
		var sb strings.Builder
		for _, e := range els {
			switch v := e.(type) {
			case *slack.RichTextSectionTextElement:
				sb.WriteString(v.Text)
			case *slack.RichTextSectionLinkElement:
				if v.Text != "" {
					sb.WriteString(v.Text)
				} else {
					sb.WriteString(v.URL)
				}
			}
		}
		if s := strings.TrimSpace(sb.String()); s != "" {
			lines = append(lines, s)
		}
	}
	writeElement = func(el slack.RichTextElement) {
		switch v := el.(type) {
		case *slack.RichTextSection:
			writeSection(v.Elements)
		case *slack.RichTextQuote:
			writeSection(v.Elements)
		case *slack.RichTextPreformatted:
			writeSection(v.Elements)
		case *slack.RichTextList:
			for _, item := range v.Elements {
				writeElement(item)
			}
		}
	}
	for _, el := range b.Elements {
		writeElement(el)
	}
	return strings.Join(lines, "\n")
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

func resolveUserNames(ctx context.Context, client historyClient, logger *slog.Logger, msgs []slack.Message, botUserID string) map[string]string {
	seen := make(map[string]bool)
	ids := make([]string, 0, len(msgs))
	for i := range msgs {
		u := msgs[i].User
		if u == "" || u == botUserID || seen[u] {
			continue
		}
		seen[u] = true
		ids = append(ids, u)
	}
	if len(ids) == 0 {
		return nil
	}
	users, err := client.GetUsersInfoContext(ctx, ids...)
	if err != nil || users == nil {
		if err != nil {
			logger.WarnContext(ctx, "slack history: user name resolution failed", "ids", len(ids), "error", err)
		}
		return nil
	}
	names := make(map[string]string, len(*users))
	for _, u := range *users {
		if name := slackDisplayName(u); name != "" {
			names[u.ID] = name
		}
	}
	return names
}

func slackDisplayName(u slack.User) string {
	switch {
	case u.Profile.DisplayName != "":
		return u.Profile.DisplayName
	case u.RealName != "":
		return u.RealName
	default:
		return u.Name
	}
}

type historyLabeler struct {
	names map[string]string
	seen  map[string]string
	n     int
}

func newHistoryLabeler(names map[string]string) *historyLabeler {
	return &historyLabeler{names: names, seen: make(map[string]string)}
}

func (l *historyLabeler) label(m slack.Message, own bool) string {
	if own {
		return "Bot"
	}
	key := m.User
	if key == "" {
		if m.Username != "" {
			return m.Username
		}
		key = "bot:" + m.BotID
	}
	if lbl, ok := l.seen[key]; ok {
		return lbl
	}
	var lbl string
	if name := l.names[m.User]; name != "" {
		lbl = name
	} else if m.Username != "" {
		lbl = m.Username
	} else {
		l.n++
		lbl = fmt.Sprintf("User %d", l.n)
	}
	l.seen[key] = lbl
	return lbl
}

func slackTSLess(a, b string) bool {
	return parseSlackTS(a) < parseSlackTS(b)
}

func parseSlackTS(ts string) float64 {
	f, _ := strconv.ParseFloat(ts, 64)
	return f
}
