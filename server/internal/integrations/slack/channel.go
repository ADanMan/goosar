package slack

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/slack-go/slack"

	"github.com/adanman/goosar/server/internal/integrations/channel"
)

const TypeSlack channel.Type = "slack"

const maxMessageRunes = 38000

type slackSender struct {
	creds  credentials
	api    *slack.Client
	logger *slog.Logger
}

func (c *slackSender) Send(ctx context.Context, out channel.OutboundMessage) (channel.SendResult, error) {
	if c.api == nil {
		return channel.SendResult{}, errors.New("slack: api client not configured")
	}
	threadTS := outboundThreadTS(out)
	var lastTS string

	for _, chunk := range chunkMessage(formatMrkdwn(out.Text), maxMessageRunes) {
		opts := []slack.MsgOption{
			slack.MsgOptionText(chunk, false),
			slack.MsgOptionDisableLinkUnfurl(),
		}
		if threadTS != "" {
			opts = append(opts, slack.MsgOptionTS(threadTS))
		}
		_, ts, err := c.api.PostMessageContext(ctx, out.ChatID, opts...)
		if err != nil {
			return channel.SendResult{}, fmt.Errorf("slack: chat.postMessage: %w", err)
		}
		lastTS = ts
	}
	return channel.SendResult{MessageID: lastTS}, nil
}

func newSlackSender(creds credentials, api *slack.Client, logger *slog.Logger) *slackSender {
	if logger == nil {
		logger = slog.Default()
	}
	return &slackSender{creds: creds, api: api, logger: logger}
}

func outboundThreadTS(out channel.OutboundMessage) string {
	if out.ReplyTo != "" {
		return out.ReplyTo
	}
	return out.ThreadID
}

func chunkMessage(text string, maxRunes int) []string {
	if maxRunes <= 0 || len([]rune(text)) <= maxRunes {
		return []string{text}
	}
	runes := []rune(text)
	var chunks []string
	for len(runes) > 0 {
		n := maxRunes
		if n > len(runes) {
			n = len(runes)
		}
		chunks = append(chunks, string(runes[:n]))
		runes = runes[n:]
	}
	return chunks
}
