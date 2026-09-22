package slack

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/slack-go/slack/slackevents"

	"github.com/adanman/goosar/server/internal/integrations/channel"
)

type slackRawEvent struct {
	TeamID      string `json:"team_id"`
	APIAppID    string `json:"api_app_id,omitempty"`
	EventType   string `json:"event_type"`
	SubType     string `json:"subtype,omitempty"`
	ChannelType string `json:"channel_type,omitempty"`
}

func compileMentionRe(botUserID string) *regexp.Regexp {
	if botUserID == "" {
		return nil
	}
	return regexp.MustCompile(`<@` + regexp.QuoteMeta(botUserID) + `(\|[^>]*)?>`)
}

func inboundFromMessage(e slackevents.EventsAPIEvent, m *slackevents.MessageEvent, botUserID string, mentionRe *regexp.Regexp) (channel.InboundMessage, bool) {
	if m.BotID != "" || m.SubType == "bot_message" {
		return channel.InboundMessage{}, false
	}
	if m.User == "" || (botUserID != "" && m.User == botUserID) {
		return channel.InboundMessage{}, false
	}
	if !isIngestableSubtype(m.SubType) {
		return channel.InboundMessage{}, false
	}

	chatType := slackChatType(m.Channel, m.ChannelType)
	addressed := chatType == channel.ChatTypeP2P || mentionsBot(m.Text, mentionRe)
	return buildInbound(e, buildInboundParams{
		eventType: "message",
		subType:   m.SubType,
		channelID: m.Channel,
		userID:    m.User,
		text:      m.Text,
		ts:        m.TimeStamp,
		threadTS:  m.ThreadTimeStamp,
		chatType:  chatType,
		addressed: addressed,
	}, mentionRe), true
}

func inboundFromAppMention(e slackevents.EventsAPIEvent, m *slackevents.AppMentionEvent, botUserID string, mentionRe *regexp.Regexp) (channel.InboundMessage, bool) {
	if m.BotID != "" || m.User == "" || (botUserID != "" && m.User == botUserID) {
		return channel.InboundMessage{}, false
	}
	return buildInbound(e, buildInboundParams{
		eventType: "app_mention",
		channelID: m.Channel,
		userID:    m.User,
		text:      m.Text,
		ts:        m.TimeStamp,
		threadTS:  m.ThreadTimeStamp,
		chatType:  channel.ChatTypeGroup,
		addressed: true,
	}, mentionRe), true
}

type buildInboundParams struct {
	eventType string
	subType   string
	channelID string
	userID    string
	text      string
	ts        string
	threadTS  string
	chatType  channel.ChatType
	addressed bool
}

func buildInbound(e slackevents.EventsAPIEvent, p buildInboundParams, mentionRe *regexp.Regexp) channel.InboundMessage {
	raw, _ := json.Marshal(slackRawEvent{
		TeamID:      e.TeamID,
		APIAppID:    e.APIAppID,
		EventType:   p.eventType,
		SubType:     p.subType,
		ChannelType: string(p.chatType),
	})
	var reply *channel.ReplyCtx
	if p.threadTS != "" && p.threadTS != p.ts {
		reply = &channel.ReplyCtx{MessageID: p.threadTS, RootID: p.threadTS}
	}
	return channel.InboundMessage{
		EventID:        p.ts,
		MessageID:      p.ts,
		Type:           channel.MsgTypeText,
		Text:           cleanText(p.text, mentionRe),
		ReplyTo:        reply,
		AddressedToBot: p.addressed,
		Source: channel.Source{
			ChannelType: TypeSlack,
			ChatID:      p.channelID,
			ChatType:    p.chatType,
			SenderID:    p.userID,
			ThreadID:    p.threadTS,
		},
		Raw: raw,
	}
}

func cleanText(text string, mentionRe *regexp.Regexp) string {
	if mentionRe != nil {
		text = mentionRe.ReplaceAllString(text, "")
	}
	return strings.TrimSpace(text)
}

func mentionsBot(text string, mentionRe *regexp.Regexp) bool {
	return mentionRe != nil && mentionRe.MatchString(text)
}

func slackChatType(channelID, channelType string) channel.ChatType {
	switch channelType {
	case "im":
		return channel.ChatTypeP2P
	case "mpim", "channel", "group", "private_channel":
		return channel.ChatTypeGroup
	}
	if strings.HasPrefix(channelID, "D") {
		return channel.ChatTypeP2P
	}
	return channel.ChatTypeGroup
}

func isIngestableSubtype(subType string) bool {
	switch subType {
	case "", "thread_broadcast", "file_share":
		return true
	default:
		return false
	}
}
