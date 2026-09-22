package slack

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/slack-go/slack"

	"github.com/adanman/goosar/server/internal/integrations/channel/engine"
	db "github.com/adanman/goosar/server/pkg/db/generated"
)

const issueSlashCommand = "/issue"

const (
	slashUsageText           = "Tell me what to file, e.g. `/issue the login button does nothing on Safari`."
	slashQueuedText          = "✅ On it — I'm turning that into an issue. You'll get a Goosar notification when it's ready."
	slashNotMemberText       = "You're not a member of this Goosar workspace, so I can't file an issue for you."
	slashLinkAccountFallback = "Link your Slack account to Goosar first, then try `/issue` again."
	slashInternalErrorText   = "⚠️ Something went wrong creating the issue. Please try again."
	slashDisabledText        = "This Slack app isn't connected to Goosar (or was disconnected). Ask a workspace admin to reconnect it."
)

type slashQueries interface {
	GetChannelInstallationByAppID(ctx context.Context, arg db.GetChannelInstallationByAppIDParams) (db.ChannelInstallation, error)
	GetChannelUserBindingByUserID(ctx context.Context, arg db.GetChannelUserBindingByUserIDParams) (db.ChannelUserBinding, error)
	GetMemberByUserAndWorkspace(ctx context.Context, arg db.GetMemberByUserAndWorkspaceParams) (db.Member, error)
}

type quickCreateEnqueuer interface {
	EnqueueQuickCreateTask(ctx context.Context, workspaceID, requesterID, agentID, squadID pgtype.UUID, prompt, priority, dueDate string, projectID, parentIssueID pgtype.UUID, attachmentIDs []pgtype.UUID) (db.AgentTaskQueue, error)
}

type SlashCommandProcessor struct {
	q           slashQueries
	tasks       quickCreateEnqueuer
	binding     bindingMinter
	appURL      string
	bindingPath string
	logger      *slog.Logger

	respond func(ctx context.Context, responseURL, text string) error
}

type SlashCommandConfig struct {
	Queries     *db.Queries
	Tasks       quickCreateEnqueuer
	Binding     bindingMinter
	AppURL      string
	BindingPath string
	Logger      *slog.Logger
}

func NewSlashCommandProcessor(cfg SlashCommandConfig) *SlashCommandProcessor {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	bindingPath := cfg.BindingPath
	if bindingPath == "" {
		bindingPath = "/slack/bind"
	}
	if !strings.HasPrefix(bindingPath, "/") {
		bindingPath = "/" + bindingPath
	}
	p := &SlashCommandProcessor{
		q:           cfg.Queries,
		tasks:       cfg.Tasks,
		binding:     cfg.Binding,
		appURL:      strings.TrimRight(cfg.AppURL, "/"),
		bindingPath: bindingPath,
		logger:      logger,
	}
	p.respond = func(ctx context.Context, responseURL, text string) error {
		return slack.PostWebhookContext(ctx, responseURL, &slack.WebhookMessage{
			ResponseType: slack.ResponseTypeEphemeral,
			Text:         text,
		})
	}
	return p
}

func (p *SlashCommandProcessor) Handle(ctx context.Context, cmd slack.SlashCommand) {

	if !strings.EqualFold(strings.TrimSpace(cmd.Command), issueSlashCommand) {
		return
	}
	text := p.process(ctx, cmd)
	if text == "" || cmd.ResponseURL == "" {
		return
	}
	if err := p.respond(ctx, cmd.ResponseURL, text); err != nil {
		p.logger.WarnContext(ctx, "slack slash command: response_url reply failed",
			"app_id", cmd.APIAppID, "error", err)
	}
}

func (p *SlashCommandProcessor) process(ctx context.Context, cmd slack.SlashCommand) string {
	prompt := strings.TrimSpace(cmd.Text)
	if prompt == "" {
		return slashUsageText
	}

	inst, err := p.resolveInstallation(ctx, cmd.APIAppID, cmd.TeamID)
	if err != nil {
		if !errors.Is(err, engine.ErrInstallationNotFound) {
			p.logger.WarnContext(ctx, "slack slash command: resolve installation failed",
				"app_id", cmd.APIAppID, "error", err)
			return slashInternalErrorText
		}
		return slashDisabledText
	}
	if !inst.Active {
		return slashDisabledText
	}

	userID, err := p.resolveUser(ctx, inst, cmd.UserID)
	if err != nil {
		switch {
		case errors.Is(err, engine.ErrSenderUnbound):
			return p.bindingText(ctx, inst, cmd.UserID)
		case errors.Is(err, engine.ErrSenderNotMember):
			return slashNotMemberText
		default:
			p.logger.WarnContext(ctx, "slack slash command: resolve user failed",
				"app_id", cmd.APIAppID, "error", err)
			return slashInternalErrorText
		}
	}

	if _, err := p.tasks.EnqueueQuickCreateTask(
		ctx,
		inst.WorkspaceID,
		userID,
		inst.AgentID,
		pgtype.UUID{},
		prompt,
		"",
		"",
		pgtype.UUID{},
		pgtype.UUID{},
		nil,
	); err != nil {
		p.logger.WarnContext(ctx, "slack slash command: enqueue quick-create failed",
			"app_id", cmd.APIAppID, "error", err)
		return slashInternalErrorText
	}
	return slashQueuedText
}

func (p *SlashCommandProcessor) resolveInstallation(ctx context.Context, appID, teamID string) (engine.ResolvedInstallation, error) {
	inst, err := p.q.GetChannelInstallationByAppID(ctx, db.GetChannelInstallationByAppIDParams{
		ChannelType: string(TypeSlack),
		AppID:       appID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return engine.ResolvedInstallation{}, engine.ErrInstallationNotFound
		}
		return engine.ResolvedInstallation{}, err
	}
	if !installationServesTeam(inst.Config, teamID) {
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

func (p *SlashCommandProcessor) resolveUser(ctx context.Context, inst engine.ResolvedInstallation, slackUserID string) (pgtype.UUID, error) {
	binding, err := p.q.GetChannelUserBindingByUserID(ctx, db.GetChannelUserBindingByUserIDParams{
		InstallationID: inst.ID,
		ChannelUserID:  slackUserID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return pgtype.UUID{}, engine.ErrSenderUnbound
		}
		return pgtype.UUID{}, err
	}
	if _, err := p.q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      binding.GoosarUserID,
		WorkspaceID: inst.WorkspaceID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return pgtype.UUID{}, engine.ErrSenderNotMember
		}
		return pgtype.UUID{}, err
	}
	return binding.GoosarUserID, nil
}

func (p *SlashCommandProcessor) bindingText(ctx context.Context, inst engine.ResolvedInstallation, slackUserID string) string {
	if p.binding == nil || p.appURL == "" {
		return slashLinkAccountFallback
	}
	token, err := p.binding.Mint(ctx, inst.WorkspaceID, inst.ID, slackUserID)
	if err != nil {
		p.logger.WarnContext(ctx, "slack slash command: mint binding token failed",
			"installation_id", inst.ID, "error", err)
		return slashLinkAccountFallback
	}
	bindURL := p.appURL + p.bindingPath + "?token=" + url.QueryEscape(token.Raw)

	return "👋 To file issues, link your Slack account to Goosar: <" +
		bindURL + "|link your account>\n(This link expires in 15 minutes.)"
}
