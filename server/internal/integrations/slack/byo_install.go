package slack

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/slack-go/slack"

	db "github.com/adanman/goosar/server/pkg/db/generated"
)

var (
	ErrInvalidBotToken = errors.New("slack: bot token must start with xoxb-")
	ErrInvalidAppToken = errors.New("slack: app-level token must start with xapp- and embed an app id")

	ErrTokenAppMismatch = errors.New("slack: the bot token and app-level token are from different Slack apps")
)

type RegisterBYOParams struct {
	WorkspaceID pgtype.UUID
	AgentID     pgtype.UUID
	InitiatorID pgtype.UUID
	BotToken    string
	AppToken    string
}

func (s *InstallService) RegisterBYO(ctx context.Context, p RegisterBYOParams) (db.ChannelInstallation, error) {
	botToken := strings.TrimSpace(p.BotToken)
	appToken := strings.TrimSpace(p.AppToken)
	if !strings.HasPrefix(botToken, "xoxb-") {
		return db.ChannelInstallation{}, ErrInvalidBotToken
	}
	appID, err := parseSlackAppID(appToken)
	if err != nil {
		return db.ChannelInstallation{}, err
	}

	auth, err := s.authTest(ctx, botToken)
	if err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("slack auth.test: %w", err)
	}
	if auth.TeamID == "" || auth.UserID == "" || auth.BotID == "" {
		return db.ChannelInstallation{}, errors.New("slack auth.test: response missing team_id / user_id / bot_id")
	}

	botAppID, err := s.botAppID(ctx, botToken, auth.BotID)
	if err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("slack bots.info: %w", err)
	}
	if botAppID != appID {
		return db.ChannelInstallation{}, ErrTokenAppMismatch
	}

	if err := s.validateAppToken(ctx, appToken); err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("slack apps.connections.open: %w", err)
	}

	sealedBot, err := s.box.Seal([]byte(botToken))
	if err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("encrypt slack bot token: %w", err)
	}
	sealedApp, err := s.box.Seal([]byte(appToken))
	if err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("encrypt slack app token: %w", err)
	}
	cfgJSON, err := json.Marshal(installConfig{
		AppID:             appID,
		TeamID:            auth.TeamID,
		BotUserID:         auth.UserID,
		BotTokenEncrypted: base64.StdEncoding.EncodeToString(sealedBot),
		AppTokenEncrypted: base64.StdEncoding.EncodeToString(sealedApp),
	})
	if err != nil {
		return db.ChannelInstallation{}, fmt.Errorf("encode slack installation config: %w", err)
	}

	return s.persistInstall(ctx, installPersist{
		wsID:        p.WorkspaceID,
		agentID:     p.AgentID,
		installerID: p.InitiatorID,
		appIDKey:    appID,
		configJSON:  cfgJSON,
	})
}

func (s *InstallService) slackOpts() []slack.Option {
	httpClient := s.httpClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	opts := []slack.Option{slack.OptionHTTPClient(httpClient)}
	if s.apiURL != "" {
		base := s.apiURL
		if !strings.HasSuffix(base, "/") {
			base += "/"
		}
		opts = append(opts, slack.OptionAPIURL(base))
	}
	return opts
}

func (s *InstallService) authTest(ctx context.Context, botToken string) (*slack.AuthTestResponse, error) {
	return slack.New(botToken, s.slackOpts()...).AuthTestContext(ctx)
}

func (s *InstallService) botAppID(ctx context.Context, botToken, botID string) (string, error) {
	bot, err := slack.New(botToken, s.slackOpts()...).GetBotInfoContext(ctx, slack.GetBotInfoParameters{Bot: botID})
	if err != nil {
		return "", err
	}
	return bot.AppID, nil
}

func (s *InstallService) validateAppToken(ctx context.Context, appToken string) error {
	api := slack.New("", append(s.slackOpts(), slack.OptionAppLevelToken(appToken))...)
	_, _, err := api.StartSocketModeContext(ctx)
	return err
}

func parseSlackAppID(appToken string) (string, error) {
	if !strings.HasPrefix(appToken, "xapp-") {
		return "", ErrInvalidAppToken
	}
	parts := strings.SplitN(appToken, "-", 5)
	if len(parts) < 4 || parts[2] == "" || !strings.HasPrefix(parts[2], "A") {
		return "", ErrInvalidAppToken
	}
	return parts[2], nil
}
