package slack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"github.com/adanman/goosar/server/internal/integrations/channel"
)

type slackChannel struct {
	appID     string
	botUserID string
	appToken  string
	botAPI    *slack.Client
	handler   channel.InboundHandler
	slash     *SlashCommandProcessor
	logger    *slog.Logger
}

const slashCommandTimeout = 10 * time.Second

func (c *slackChannel) Type() channel.Type { return TypeSlack }

func (c *slackChannel) Capabilities() channel.Capability {
	return channel.CapText | channel.CapThreadReply
}

func (c *slackChannel) Disconnect(ctx context.Context) error { return nil }

func (c *slackChannel) Send(ctx context.Context, out channel.OutboundMessage) (channel.SendResult, error) {
	return newSlackSender(credentials{BotUserID: c.botUserID}, c.botAPI, c.logger).Send(ctx, out)
}

func (c *slackChannel) Connect(ctx context.Context) error {
	if c.handler == nil {
		return errors.New("slack: inbound handler not configured")
	}
	if c.appToken == "" {
		return errors.New("slack: app-level token not configured")
	}

	api := slack.New("", slack.OptionAppLevelToken(c.appToken))
	sm := socketmode.New(api)

	runCtx, runCancel := context.WithCancel(ctx)
	runErr := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		runErr <- sm.RunContext(runCtx)
		close(done)
	}()
	defer func() {
		runCancel()
		<-done
	}()

	mentionRe := compileMentionRe(c.botUserID)
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-runErr:
			if ctx.Err() != nil {
				return nil
			}
			if err != nil {
				return err
			}
			return errors.New("slack: socket mode connection closed")
		case evt, ok := <-sm.Events:
			if !ok {
				if ctx.Err() != nil {
					return nil
				}
				return errors.New("slack: socket mode event stream closed")
			}
			if err := c.handleSocketEvent(ctx, sm, evt, mentionRe); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
		}
	}
}

func (c *slackChannel) handleSocketEvent(ctx context.Context, sm *socketmode.Client, evt socketmode.Event, mentionRe *regexp.Regexp) error {
	switch evt.Type {
	case socketmode.EventTypeEventsAPI:
		eventsAPI, ok := evt.Data.(slackevents.EventsAPIEvent)
		if !ok {
			return nil
		}

		if evt.Request != nil {
			if err := sm.Ack(*evt.Request); err != nil {
				c.logger.WarnContext(ctx, "slack: ack failed", "error", err)
			}
		}
		return c.dispatchEventsAPI(ctx, eventsAPI, mentionRe)
	case socketmode.EventTypeSlashCommand:

		if evt.Request != nil {
			if err := sm.Ack(*evt.Request); err != nil {
				c.logger.WarnContext(ctx, "slack: ack slash command failed", "error", err)
			}
		}
		cmd, ok := evt.Data.(slack.SlashCommand)
		if ok {
			c.dispatchSlashCommand(cmd)
		}
		return nil
	case socketmode.EventTypeConnecting, socketmode.EventTypeConnected, socketmode.EventTypeHello:
		c.logger.DebugContext(ctx, "slack: socket mode", "event", evt.Type, "app_id", c.appID)
	case socketmode.EventTypeIncomingError, socketmode.EventTypeErrorBadMessage:
		c.logger.WarnContext(ctx, "slack: socket mode error", "event", evt.Type, "app_id", c.appID)
	default:
		if evt.Request != nil {
			_ = sm.Ack(*evt.Request)
		}
	}
	return nil
}

func (c *slackChannel) dispatchEventsAPI(ctx context.Context, e slackevents.EventsAPIEvent, mentionRe *regexp.Regexp) error {
	var (
		msg channel.InboundMessage
		ok  bool
	)
	switch inner := e.InnerEvent.Data.(type) {
	case *slackevents.AppMentionEvent:
		msg, ok = inboundFromAppMention(e, inner, c.botUserID, mentionRe)
	case *slackevents.MessageEvent:
		msg, ok = inboundFromMessage(e, inner, c.botUserID, mentionRe)
	default:
		return nil
	}
	if !ok {
		return nil
	}
	return c.handler(ctx, msg)
}

func (c *slackChannel) dispatchSlashCommand(cmd slack.SlashCommand) {
	if c.slash == nil {
		c.logger.Warn("slack: slash command received but no processor configured",
			"command", cmd.Command, "app_id", c.appID)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), slashCommandTimeout)
		defer cancel()
		c.slash.Handle(ctx, cmd)
	}()
}

type ChannelDeps struct {
	Decrypt Decrypter
	Logger  *slog.Logger

	Slash *SlashCommandProcessor
}

func RegisterSlack(reg *channel.Registry, deps ChannelDeps) {
	reg.Register(TypeSlack, newSlackFactory(deps))
}

func newSlackFactory(deps ChannelDeps) channel.Factory {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return func(cfg channel.Config) (channel.Channel, error) {
		var ic installConfig
		if err := json.Unmarshal(cfg.Raw, &ic); err != nil {
			return nil, fmt.Errorf("slack: decode installation config: %w", err)
		}
		appToken, err := decryptToken(ic.AppTokenEncrypted, deps.Decrypt)
		if err != nil {
			return nil, fmt.Errorf("slack: decrypt app token: %w", err)
		}
		if appToken == "" {
			return nil, errors.New("slack: installation has no app-level token")
		}
		botToken, err := decryptToken(ic.BotTokenEncrypted, deps.Decrypt)
		if err != nil {
			return nil, fmt.Errorf("slack: decrypt bot token: %w", err)
		}
		return &slackChannel{
			appID:     ic.AppID,
			botUserID: ic.BotUserID,
			appToken:  appToken,
			botAPI:    slack.New(botToken),
			handler:   cfg.Handler,
			slash:     deps.Slash,
			logger:    logger,
		}, nil
	}
}
