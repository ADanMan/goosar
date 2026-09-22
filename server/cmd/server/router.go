package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/adanman/goosar/server/internal/analytics"
	"github.com/adanman/goosar/server/internal/auth"
	"github.com/adanman/goosar/server/internal/cloudruntime"
	"github.com/adanman/goosar/server/internal/daemonws"
	"github.com/adanman/goosar/server/internal/deliveryprofile"
	"github.com/adanman/goosar/server/internal/deploymentprofile"
	"github.com/adanman/goosar/server/internal/events"
	"github.com/adanman/goosar/server/internal/featureflags"
	"github.com/adanman/goosar/server/internal/handler"
	"github.com/adanman/goosar/server/internal/integrations/channel"
	"github.com/adanman/goosar/server/internal/integrations/channel/engine"
	composiointeg "github.com/adanman/goosar/server/internal/integrations/composio"
	"github.com/adanman/goosar/server/internal/integrations/slack"
	obsmetrics "github.com/adanman/goosar/server/internal/metrics"
	"github.com/adanman/goosar/server/internal/middleware"
	"github.com/adanman/goosar/server/internal/perimeterpolicy"
	"github.com/adanman/goosar/server/internal/provisioning"
	"github.com/adanman/goosar/server/internal/realtime"
	"github.com/adanman/goosar/server/internal/service"
	"github.com/adanman/goosar/server/internal/skillsources"
	"github.com/adanman/goosar/server/internal/storage"
	"github.com/adanman/goosar/server/internal/util"
	"github.com/adanman/goosar/server/internal/util/secretbox"
	composiosdk "github.com/adanman/goosar/server/pkg/composio"
	db "github.com/adanman/goosar/server/pkg/db/generated"
	"github.com/adanman/goosar/server/pkg/featureflag"
)

var defaultOrigins = []string{
	"http://localhost:3000",
	"http://localhost:5173",
	"http://localhost:5174",
}

var corsAllowedHeaders = []string{
	"Accept",
	"Authorization",
	"Content-Type",
	"X-Workspace-ID",
	"X-Workspace-Slug",
	"X-Request-ID",
	"X-Agent-ID",
	"X-Task-ID",
	"X-CSRF-Token",
	"X-Client-Platform",
	"X-Client-Version",
	"X-Client-OS",
	"X-Client-Capabilities",
}

func allowedOrigins() []string {
	raw := strings.TrimSpace(os.Getenv("CORS_ALLOWED_ORIGINS"))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN"))
	}
	if raw == "" {
		return defaultOrigins
	}

	parts := strings.Split(raw, ",")
	origins := make([]string, 0, len(parts))
	for _, part := range parts {
		origin := strings.TrimSpace(part)
		if origin != "" {
			origins = append(origins, origin)
		}
	}
	if len(origins) == 0 {
		return defaultOrigins
	}
	return origins
}

func appURLFromEnv() string {
	if v := strings.TrimRight(strings.TrimSpace(os.Getenv("GOOSAR_APP_URL")), "/"); v != "" {
		return v
	}
	return strings.TrimRight(strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN")), "/")
}

func parseTrustedProxies(raw string) []netip.Prefix {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var out []netip.Prefix
	for _, part := range strings.Split(raw, ",") {
		s := strings.TrimSpace(part)
		if s == "" {
			continue
		}
		p, err := netip.ParsePrefix(s)
		if err != nil {
			slog.Warn("GOOSAR_TRUSTED_PROXIES: ignoring invalid CIDR",
				"value", s, "error", err)
			continue
		}
		out = append(out, p)
	}
	return out
}

func normalizeServerVersion(v string) string {
	if v == "dev" {
		return ""
	}
	return v
}

func deliveryProfileFromEnv() deliveryprofile.Profile {
	profile, err := deliveryprofile.FromEnv()
	if err != nil {
		slog.Error("delivery profile configuration invalid", "error", err)
		os.Exit(1)
	}

	if _, err := skillsources.FromEnv(); err != nil {
		slog.Error("skill sources configuration invalid", "error", err)
		os.Exit(1)
	}
	return profile
}

func deploymentProfileFromEnv() deploymentprofile.Profile {
	profile, err := deploymentprofile.FromEnv()
	if err != nil {
		slog.Error("deployment profile configuration invalid", "error", err)
		os.Exit(1)
	}
	return profile
}

func allowedProvidersFromEnv(profile deliveryprofile.Profile) *perimeterpolicy.ProviderPolicy {
	policy, err := perimeterpolicy.ProviderPolicyFromEnv(profile)
	if err != nil {
		slog.Error("allowed provider policy configuration invalid", "error", err)
		os.Exit(1)
	}
	return policy
}

func provisioningStoreFromEnv(backend storage.Storage) provisioning.PackageStore {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("GOOSAR_PROVISIONING_STORE"))) {
	case "local":
		if backend == nil {
			slog.Error("GOOSAR_PROVISIONING_STORE=local requires a configured storage backend (S3 or LOCAL_UPLOAD_DIR); provisioning disabled")
			return nil
		}
		local := provisioning.NewLocalPackageStore(backend, strings.TrimSpace(os.Getenv("GOOSAR_PROVISIONING_LOCAL_PREFIX")))

		ctx, cancel := context.WithTimeout(context.Background(), provisioningSyncBudget())
		defer cancel()
		if _, err := local.List(ctx); errors.Is(err, provisioning.ErrCatalogNotFound) {
			slog.Warn(provisioning.KitMissingHint)
		}
		return local
	case "oci":

		store := provisioning.NewOCIPackageStoreFromEnv()
		if store == nil {
			slog.Error("GOOSAR_PROVISIONING_STORE=oci requires GOOSAR_PROVISIONING_OCI_URL; provisioning disabled")
			return nil
		}

		ctx, cancel := context.WithTimeout(context.Background(), provisioningSyncBudget())
		defer cancel()
		return provisioning.EnsureLocalKit(ctx, store, localProvisioningStore(backend), provisioningRefsFromEnv(), slog.Default())
	case "":
		return nil
	default:
		slog.Error("unrecognized GOOSAR_PROVISIONING_STORE value; provisioning disabled", "value", os.Getenv("GOOSAR_PROVISIONING_STORE"))
		return nil
	}
}

func provisioningSyncBudget() time.Duration {
	return envDuration("GOOSAR_PROVISIONING_SYNC_TIMEOUT", 3*time.Minute)
}

func localProvisioningStore(backend storage.Storage) *provisioning.LocalPackageStore {
	if backend == nil {
		return nil
	}
	return provisioning.NewLocalPackageStore(backend, strings.TrimSpace(os.Getenv("GOOSAR_PROVISIONING_LOCAL_PREFIX")))
}

func provisioningRefsFromEnv() []provisioning.PackageRef {
	raw := strings.TrimSpace(os.Getenv("GOOSAR_PROVISIONING_OCI_PACKAGES"))
	if raw == "" {
		return nil
	}
	refs, err := provisioning.ParsePackageRefs(raw)
	if err != nil {
		slog.Error("GOOSAR_PROVISIONING_OCI_PACKAGES is malformed; falling back to registry enumeration", "error", err)
		return nil
	}
	return refs
}

func NewRouter(pool *pgxpool.Pool, hub *realtime.Hub, bus *events.Bus, analyticsClient analytics.Client, rdb *redis.Client) chi.Router {
	r, _ := NewRouterWithOptions(pool, hub, bus, analyticsClient, rdb, RouterOptions{})
	return r
}

type RouterOptions struct {
	HTTPMetrics     *obsmetrics.HTTPMetrics
	BusinessMetrics *obsmetrics.BusinessMetrics
	DaemonHub       *daemonws.Hub
	DaemonWakeup    service.TaskWakeupNotifier
	FeatureFlags    *featureflag.Service

	HeartbeatScheduler handler.HeartbeatScheduler

	Health *serverHealth
}

func NewRouterWithOptions(pool *pgxpool.Pool, hub *realtime.Hub, bus *events.Bus, analyticsClient analytics.Client, rdb *redis.Client, opts RouterOptions) (chi.Router, *handler.Handler) {
	queries := db.New(pool)
	emailSvc := service.NewEmailService()
	daemonHub := opts.DaemonHub
	if daemonHub == nil {
		daemonHub = daemonws.NewHub()
	}

	store := storage.FromEnv()

	cfSigner := auth.NewCloudFrontSignerFromEnv()
	origins := allowedOrigins()

	deliveryProfile := deliveryProfileFromEnv()
	signupConfig := handler.Config{
		AllowSignup:              os.Getenv("ALLOW_SIGNUP") != "false",
		AllowedEmails:            splitAndTrim(os.Getenv("ALLOWED_EMAILS")),
		AllowedEmailDomains:      splitAndTrim(os.Getenv("ALLOWED_EMAIL_DOMAINS")),
		DisableWorkspaceCreation: os.Getenv("DISABLE_WORKSPACE_CREATION") == "true",
		VCSIntegrationEnabled:    os.Getenv("GOOSAR_VCS_INTEGRATION_ENABLED") == "true",
		PublicURL:                strings.TrimRight(strings.TrimSpace(os.Getenv("GOOSAR_PUBLIC_URL")), "/"),
		TrustedProxies:           parseTrustedProxies(os.Getenv("GOOSAR_TRUSTED_PROXIES")),
		CloudRuntimeFleetURL:     cloudRuntimeFleetURLFromEnv(),
		CloudRuntimeFleetTimeout: envDuration("GOOSAR_CLOUD_FLEET_TIMEOUT", 35*time.Second),
		AttachmentDownloadMode:   os.Getenv("ATTACHMENT_DOWNLOAD_MODE"),
		AttachmentDownloadURLTTL: envDuration("ATTACHMENT_DOWNLOAD_URL_TTL", 30*time.Minute),
		AttachmentFrameAncestors: origins,
		LLMAPIKey:                strings.TrimSpace(os.Getenv("GOOSAR_LLM_API_KEY")),
		LLMBaseURL:               strings.TrimSpace(os.Getenv("GOOSAR_LLM_BASE_URL")),
		LLMDefaultModel:          strings.TrimSpace(os.Getenv("GOOSAR_LLM_DEFAULT_MODEL")),
		ServerVersion:            normalizeServerVersion(version),
		DeliveryProfile:          deliveryProfile,
		DeploymentProfile:        deploymentProfileFromEnv(),
		MCPPolicy:                perimeterpolicy.MCPPolicyFromEnv(deliveryProfile),
		AllowedProviders:         allowedProvidersFromEnv(deliveryProfile),
	}
	h := handler.New(queries, pool, hub, bus, emailSvc, store, cfSigner, analyticsClient, signupConfig, daemonHub)
	h.ProvisioningStore = provisioningStoreFromEnv(store)
	wireCorporateAuth(h)
	h.Metrics = opts.BusinessMetrics
	h.FeatureFlags = opts.FeatureFlags
	h.TaskService.FeatureFlags = opts.FeatureFlags
	h.TaskService.Metrics = opts.BusinessMetrics
	h.IssueService.Metrics = opts.BusinessMetrics
	if opts.BusinessMetrics != nil {

		if client, ok := h.CloudRuntime.(*cloudruntime.Client); ok {
			client.SetRecorder(opts.BusinessMetrics)
		}
	}
	if opts.DaemonWakeup != nil {
		h.TaskService.Wakeup = opts.DaemonWakeup
		if notifier, ok := opts.DaemonWakeup.(handler.RuntimeProfileRefreshNotifier); ok {
			h.DaemonProfileRefresh = notifier
		}
		if notifier, ok := opts.DaemonWakeup.(handler.WorkspaceSetRefreshNotifier); ok {
			h.DaemonWorkspaceRefresh = notifier
		}
	}
	if rdb != nil {
		h.UpdateStore = handler.NewRedisUpdateStore(rdb)
		h.ModelListStore = handler.NewRedisModelListStore(rdb)
		h.LocalSkillListStore = handler.NewRedisLocalSkillListStore(rdb)
		h.LocalSkillImportStore = handler.NewRedisLocalSkillImportStore(rdb)
		h.LivenessStore = handler.NewRedisLivenessStore(rdb)
		h.WebhookRateLimiter = handler.NewRedisWebhookRateLimiter(rdb, handler.DefaultWebhookRateLimit())
		h.WebhookIPRateLimiter = handler.NewRedisWebhookIPRateLimiter(rdb, handler.DefaultWebhookIPRateLimit())
		h.WebhookAbsoluteIPRateLimiter = handler.NewRedisWebhookAbsoluteIPRateLimiter(rdb, handler.DefaultWebhookAbsoluteIPRateLimit())
	}

	channelRegistry := channel.NewRegistry()
	channelRouter := engine.NewRouter(h.IssueService, h.TaskService, queries, engine.RouterConfig{Logger: slog.Default()})

	channelRouter.EnableRunBatching(engine.DefaultChatRunBatchWindow)
	h.ChannelRouter = channelRouter

	if store != nil {
		h.ChannelMediaReconciler = &service.ChannelMediaReconciler{
			Queries: queries,
			Storage: store,
			Logger:  slog.Default(),
		}
	}
	if _, err := secretbox.LoadKey("GOOSAR_SLACK_SECRET_KEY"); err == nil {
		box, err := secretbox.FromEnv("GOOSAR_SLACK_SECRET_KEY")
		if err != nil {
			slog.Error("slack: secret key ring invalid (check GOOSAR_SLACK_SECRET_KEY_PREVIOUS); slack integration disabled", "error", err)
		} else {

			slackBindingSvc := slack.NewBindingTokenService(queries, pool)
			h.SlackBindingTokens = slackBindingSvc
			slackReplier := slack.NewOutboundReplier(slack.OutboundReplierConfig{
				Binding: slackBindingSvc,
				Decrypt: box.Open,

				AppURL: appURLFromEnv(),
				Logger: slog.Default(),
			})

			slackTyping := slack.NewTypingIndicatorManager(queries, box.Open, slog.Default())
			slackTyping.Register(bus)
			channelRouter.Register(slack.TypeSlack, slack.NewSlackResolverSet(queries, pool, slackReplier, slackTyping))
			slack.NewOutbound(queries, box.Open, slog.Default()).Register(bus)

			h.SlackHistory = slack.NewHistory(queries, box.Open, slog.Default())

			slackSlash := slack.NewSlashCommandProcessor(slack.SlashCommandConfig{
				Queries: queries,
				Tasks:   h.TaskService,
				Binding: slackBindingSvc,
				AppURL:  appURLFromEnv(),
				Logger:  slog.Default(),
			})

			slack.RegisterSlack(channelRegistry, slack.ChannelDeps{Decrypt: box.Open, Logger: slog.Default(), Slash: slackSlash})

			installSvc, ierr := slack.NewInstallService(queries, pool, box, slog.Default())
			if ierr != nil {
				slog.Error("slack: InstallService init failed; install disabled", "error", ierr)
			} else {
				h.SlackInstall = installSvc
			}
			slog.Info("slack integration enabled (BYO per-installation socket mode)")
		}
	} else {
		slog.Info("slack integration disabled (GOOSAR_SLACK_SECRET_KEY not set)")
	}

	if composioAPIKey := strings.TrimSpace(os.Getenv("COMPOSIO_API_KEY")); composioAPIKey != "" {
		if !featureflags.ComposioMCPAppsEnabled(context.Background(), opts.FeatureFlags) {
			slog.Info("composio integration disabled (feature flag off)")
		} else {
			sdkClient, err := composiosdk.NewClient(composiosdk.Options{APIKey: composioAPIKey})
			if err != nil {
				slog.Error("composio: SDK client init failed; composio integration disabled", "error", err)
			} else {
				stateSecret := composioStateSecret()
				callbackBase := composioCallbackBaseURL(signupConfig.PublicURL)
				switch {
				case len(stateSecret) == 0:
					slog.Error("composio: no state secret (set COMPOSIO_STATE_SECRET or JWT_SECRET); composio integration disabled")
				case callbackBase == "":
					slog.Error("composio: no callback base url (set COMPOSIO_CALLBACK_BASE_URL or GOOSAR_PUBLIC_URL); composio integration disabled")
				default:
					svc, serr := composiointeg.NewService(sdkClient, queries, composiointeg.Config{
						StateSecret:     stateSecret,
						CallbackBaseURL: callbackBase,
						FrontendBaseURL: appURLFromEnv(),
					})
					if serr != nil {
						slog.Error("composio: service init failed; composio integration disabled", "error", serr)
					} else {
						h.Composio = svc

						if h.TaskService != nil {
							h.TaskService.Composio = svc
						}
						slog.Info("composio integration enabled")
					}
				}
			}
		}
	} else {
		slog.Info("composio integration disabled (COMPOSIO_API_KEY not set)")
	}

	if _, err := secretbox.LoadKey("GOOSAR_VCS_SECRET_KEY"); err == nil {
		box, err := secretbox.FromEnv("GOOSAR_VCS_SECRET_KEY")
		if err != nil {
			slog.Error("vcs: secret key ring invalid (check GOOSAR_VCS_SECRET_KEY_PREVIOUS); vcs integration disabled", "error", err)
		} else {
			h.VCSSecretBox = box
			slog.Info("vcs integration enabled")
		}
	} else {
		slog.Info("vcs integration disabled (GOOSAR_VCS_SECRET_KEY not set)")
	}

	if _, err := secretbox.LoadKey("GOOSAR_MCP_SECRET_KEY"); err == nil {
		box, err := secretbox.FromEnv("GOOSAR_MCP_SECRET_KEY")
		if err != nil {
			slog.Error("mcp: secret key ring invalid (check GOOSAR_MCP_SECRET_KEY_PREVIOUS); agent mcp_config will be stored in PLAINTEXT", "error", err)
		} else {
			h.MCPSecretBox = box
			slog.Info("agent mcp_config at-rest encryption enabled")

			if pool != nil {
				go func() {
					if _, err := h.BackfillSealedMcpConfigs(context.Background()); err != nil {
						slog.Error("mcp_config encryption backfill failed", "error", err)
					}
				}()
			}
		}
	} else if errors.Is(err, secretbox.ErrKeyNotSet) {
		slog.Warn("agent mcp_config at-rest encryption disabled (GOOSAR_MCP_SECRET_KEY not set); mcp_config values, which may contain MCP server credentials, are stored in PLAINTEXT")
	} else {

		slog.Error("agent mcp_config at-rest encryption disabled: GOOSAR_MCP_SECRET_KEY is set but invalid (a rotation typo?); mcp_config values, which may contain MCP server credentials, are stored in PLAINTEXT", "error", err)
	}

	if pool != nil {
		h.StartAuditRetentionJob(context.Background())
	}

	if opts.HeartbeatScheduler != nil {
		h.HeartbeatScheduler = opts.HeartbeatScheduler
	}

	patCache := auth.NewPATCache(rdb)
	daemonTokenCache := auth.NewDaemonTokenCache(rdb)
	h.PATCache = patCache
	h.DaemonTokenCache = daemonTokenCache
	h.MembershipCache = auth.NewMembershipCache(rdb)

	cloudPATVerifier := auth.NewCloudPATVerifier(auth.CloudPATVerifierConfig{
		FleetBaseURL: signupConfig.CloudRuntimeFleetURL,
		Redis:        rdb,
	})

	h.TaskService.EmptyClaim = service.NewEmptyClaimCache(rdb)

	daemonHub.SetHeartbeatHandler(h.HandleDaemonWSHeartbeat)

	daemonHub.SetRPCHandler(h.DaemonRPCHandler)
	health := opts.Health
	if health == nil {
		health = newServerHealth(pool)
	}

	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.ClientMetadata)
	r.Use(middleware.RequestLogger)
	if opts.HTTPMetrics != nil {
		r.Use(opts.HTTPMetrics.Middleware)
	}
	r.Use(chimw.Recoverer)
	r.Use(middleware.ContentSecurityPolicy)

	realtime.SetAllowedOrigins(origins)

	realtime.SetTrustedProxies(signupConfig.TrustedProxies)

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: origins,
		AllowedMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders: corsAllowedHeaders,

		ExposedHeaders:   []string{"X-Request-ID"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	r.Get("/health", health.liveHandler)
	r.Get("/readyz", health.readyHandler)
	r.Get("/healthz", health.readyHandler)

	r.Get("/health/realtime", realtimeMetricsHandler(os.Getenv("REALTIME_METRICS_TOKEN")))

	mc := &membershipChecker{queries: queries}
	pr := &patResolver{queries: queries, cache: patCache}
	slugResolver := realtime.SlugResolver(func(ctx context.Context, slug string) (string, error) {
		ws, err := queries.GetWorkspaceBySlug(ctx, slug)
		if err != nil {
			return "", err
		}
		return util.UUIDToString(ws.ID), nil
	})
	r.Get("/ws", func(w http.ResponseWriter, r *http.Request) {
		realtime.HandleWebSocket(hub, mc, pr, slugResolver, w, r)
	})

	if _, ok := store.(*storage.LocalStorage); ok {
		r.Get("/uploads/*", h.ServeLocalUpload)
	}

	rlStore := middleware.NewRateLimitStore(rdb)
	trustedProxies := middleware.TrustedProxiesFromEnv()
	limitAuth := envPositiveInt("RATE_LIMIT_AUTH", 5)
	limitAuthVerify := envPositiveInt("RATE_LIMIT_AUTH_VERIFY", 20)
	limitAuthEmail := envPositiveInt("RATE_LIMIT_AUTH_EMAIL", 10)
	limitTokens := envPositiveInt("RATE_LIMIT_TOKEN", 20)

	limitMFAVerify := envPositiveInt("RATE_LIMIT_MFA_VERIFY", 10)
	limitAPI := envPositiveInt("RATE_LIMIT_API", 600)
	slog.Info("rate limiting enabled",
		"backend", rlStore.Backend(),
		"auth_send_code_per_ip_per_min", limitAuth,
		"auth_verify_per_ip_per_min", limitAuthVerify,
		"auth_per_email_per_min", limitAuthEmail,
		"token_issuance_per_user_per_hour", limitTokens,
		"api_per_user_per_min", limitAPI,
		"trusted_proxies", len(trustedProxies),
	)
	authRL := middleware.RateLimit(rlStore, limitAuth, time.Minute, trustedProxies)
	authVerifyRL := middleware.RateLimit(rlStore, limitAuthVerify, time.Minute, trustedProxies)

	authEmailRL := middleware.RateLimitByJSONField(rlStore, "email", limitAuthEmail, time.Minute)
	contactSalesRL := middleware.RateLimit(rlStore, envPositiveInt("RATE_LIMIT_CONTACT_SALES", 5), time.Hour, trustedProxies)

	apiRL := middleware.RateLimitByUserOrIP(rlStore, "api", limitAPI, time.Minute, trustedProxies)

	tokenRL := middleware.RateLimitByUserOrIP(rlStore, "token", limitTokens, time.Hour, trustedProxies)

	exportRL := middleware.RateLimitByUserOrIP(rlStore, "export",
		envPositiveInt("RATE_LIMIT_EXPORT", 3), time.Hour, trustedProxies)

	joinRL := middleware.RateLimitByUserOrIP(rlStore, "join",
		envPositiveInt("RATE_LIMIT_JOIN", 20), time.Hour, trustedProxies)
	r.With(authRL, authEmailRL).Post("/auth/send-code", h.SendCode)
	r.With(authVerifyRL, authEmailRL).Post("/auth/verify-code", h.VerifyCode)

	r.With(authVerifyRL).Post("/auth/verify-link", h.VerifyLink)
	r.Post("/auth/logout", h.Logout)

	r.With(authVerifyRL, middleware.RateLimitByJSONFieldHashed(
		rlStore, "mfa_token", limitMFAVerify, handler.MFAPendingTTL),
	).Post("/api/auth/mfa/verify", h.VerifyMFA)

	r.With(authVerifyRL).Get("/api/auth/methods", h.GetAuthMethods)
	r.With(authVerifyRL).Get("/api/auth/oidc/start", h.StartOIDC)
	r.With(authVerifyRL).Get("/api/auth/oidc/callback", h.CallbackOIDC)

	r.With(authRL, middleware.RateLimitByJSONField(rlStore, "username", limitAuthEmail, time.Minute)).
		Post("/api/auth/ldap/login", h.LoginLDAP)

	r.Get("/api/config", h.GetConfig)
	r.With(contactSalesRL).Post("/api/contact-sales", h.CreateContactSales)

	r.Post("/api/webhooks/autopilots/{token}", h.HandleAutopilotWebhook)

	r.Post("/api/webhooks/github", h.HandleGitHubWebhook)
	r.Get("/api/github/setup", h.GitHubSetupCallback)

	r.Post("/api/webhooks/vcs/{connectionId}", h.HandleVCSWebhook)

	r.Post("/api/webhooks/stripe", h.HandleCloudBillingStripeWebhook)

	r.Get("/api/integrations/composio/callback", h.ComposioCallback)

	r.Route("/api/daemon", func(r chi.Router) {
		r.Use(middleware.DaemonAuth(queries, patCache, daemonTokenCache, cloudPATVerifier))

		r.Post("/register", h.DaemonRegister)
		r.Post("/deregister", h.DaemonDeregister)
		r.Post("/heartbeat", h.DaemonHeartbeat)
		r.Get("/ws", h.DaemonWebSocket)
		r.Get("/workspaces", h.ListDaemonWorkspaces)
		r.Get("/workspaces/{workspaceId}/repos", h.GetDaemonWorkspaceRepos)
		r.Get("/workspaces/{workspaceId}/runtime-profiles", h.DaemonListRuntimeProfiles)

		r.With(handler.RequireMinDaemonVersion).Group(func(r chi.Router) {
			r.Post("/runtimes/{runtimeId}/tasks/claim", h.ClaimTaskByRuntime)

			r.Post("/tasks/claim", h.ClaimTasksByRuntime)
			r.Post("/claim", h.ClaimTasksByRuntime)
		})
		r.Post("/runtimes/{runtimeId}/tasks/{taskId}/prepare-lease", h.ExtendTaskPrepareLease)
		r.Post("/runtimes/{runtimeId}/tasks/{taskId}/skill-bundles/resolve", h.ResolveTaskSkillBundles)
		r.Get("/runtimes/{runtimeId}/tasks/pending", h.ListPendingTasksByRuntime)
		r.Post("/runtimes/{runtimeId}/update/{updateId}/result", h.ReportUpdateResult)
		r.Post("/runtimes/{runtimeId}/models/{requestId}/result", h.ReportModelListResult)
		r.Post("/runtimes/{runtimeId}/local-skills/{requestId}/result", h.ReportLocalSkillListResult)
		r.Post("/runtimes/{runtimeId}/local-skills/import/{requestId}/result", h.ReportLocalSkillImportResult)

		r.Get("/tasks/{taskId}/status", h.GetTaskStatus)
		r.Post("/tasks/{taskId}/start", h.StartTask)
		r.Post("/tasks/{taskId}/wait-local-directory", h.MarkTaskWaitingLocalDirectory)
		r.Post("/tasks/{taskId}/progress", h.ReportTaskProgress)
		r.Post("/tasks/{taskId}/complete", h.CompleteTask)
		r.Post("/tasks/{taskId}/fail", h.FailTask)
		r.Post("/tasks/{taskId}/usage", h.ReportTaskUsage)
		r.Post("/tasks/{taskId}/messages", h.ReportTaskMessages)
		r.Get("/tasks/{taskId}/messages", h.ListTaskMessages)
		r.Post("/tasks/{taskId}/cancel-ack", h.AckTaskCancelled)

		r.Post("/workspaces/{workspaceId}/issues/gc-check", h.BatchIssueGCCheck)
		r.Get("/issues/{issueId}/gc-check", h.GetIssueGCCheck)
		r.Get("/chat-sessions/{sessionId}/gc-check", h.GetChatSessionGCCheck)
		r.Get("/autopilot-runs/{runId}/gc-check", h.GetAutopilotRunGCCheck)
		r.Get("/tasks/{taskId}/gc-check", h.GetTaskGCCheck)

		r.Post("/runtimes/{runtimeId}/recover-orphans", h.RecoverOrphanedTasks)
		r.Post("/tasks/{taskId}/session", h.PinTaskSession)
	})

	r.With(middleware.AttachmentDownloadAuth(
		middleware.Auth(queries, patCache, cloudPATVerifier, h),
		middleware.RefreshCloudFrontCookies(cfSigner),
	)).Get("/api/attachments/{id}/download", h.DownloadAttachment)

	r.Group(func(r chi.Router) {
		r.Use(middleware.Auth(queries, patCache, cloudPATVerifier, h))
		r.Use(middleware.RefreshCloudFrontCookies(cfSigner))

		r.Use(apiRL)

		r.With(handler.RequireHumanActor).Get("/api/auth/mfa", h.GetMFAStatus)
		r.With(handler.RequireHumanActor, tokenRL).Post("/api/auth/mfa/totp/enroll", h.EnrollTOTP)
		r.With(handler.RequireHumanActor, tokenRL).Post("/api/auth/mfa/totp/confirm", h.ConfirmTOTP)
		r.With(handler.RequireHumanActor, tokenRL).Post("/api/auth/mfa/totp/disable", h.DisableTOTP)
		r.With(handler.RequireHumanActor, tokenRL).Post("/api/auth/mfa/recovery-codes", h.RegenerateRecoveryCodes)
		r.With(handler.RequireHumanActor).Get("/api/auth/sessions", h.ListMySessions)
		r.With(handler.RequireHumanActor).Delete("/api/auth/sessions/{sessionId}", h.RevokeMySession)
		r.With(handler.RequireHumanActor).Post("/api/auth/sessions/revoke-all", h.RevokeAllMySessions)

		r.Get("/api/me", h.GetMe)
		r.Patch("/api/me", h.UpdateMe)
		r.Patch("/api/me/onboarding", h.PatchOnboarding)
		r.Post("/api/me/onboarding/complete", h.CompleteOnboarding)
		r.Post("/api/me/onboarding/cloud-waitlist", h.JoinCloudWaitlist)

		r.With(handler.RequireHumanActor, exportRL).Get("/api/me/export", h.ExportMyData)

		r.Post("/api/me/onboarding/runtime-bootstrap", h.BootstrapOnboardingRuntime)
		r.Post("/api/me/onboarding/no-runtime-bootstrap", h.BootstrapOnboardingNoRuntime)

		r.With(handler.RequireHumanActor, tokenRL).Post("/api/cli-token", h.IssueCliToken)
		r.Post("/api/upload-file", h.UploadFile)
		r.Post("/api/feedback", h.CreateFeedback)
		r.With(handler.RequireHumanActor).Post("/api/client-usage", h.UpsertClientUsage)

		r.Get("/api/workspace-templates", h.ListWorkspaceTemplates)

		r.Route("/api/workspaces", func(r chi.Router) {
			r.Get("/", h.ListWorkspaces)

			r.With(handler.RequireHumanActor).Post("/", h.CreateWorkspace)
			r.Route("/{id}", func(r chi.Router) {

				r.Group(func(r chi.Router) {
					r.Use(middleware.RequireWorkspaceMemberFromURL(queries, "id"))
					r.Get("/", h.GetWorkspace)

					r.Get("/capabilities", h.GetWorkspaceCapabilities)
					r.Get("/members", h.ListMembersWithUser)
					r.Post("/leave", h.LeaveWorkspace)
					r.Get("/invitations", h.ListWorkspaceInvitations)

					r.Get("/github/installations", h.ListGitHubInstallations)

					r.Get("/vcs/connections", h.ListVCSConnections)

					r.Get("/runtime-profiles", h.ListRuntimeProfiles)
					r.Get("/runtime-profiles/{profileId}", h.GetRuntimeProfile)
				})

				r.Group(func(r chi.Router) {
					r.Use(middleware.RequireWorkspaceRoleFromURL(queries, "id", "owner", "admin"))
					r.Put("/", h.UpdateWorkspace)
					r.Patch("/", h.UpdateWorkspace)
					r.Post("/members", h.CreateInvitation)
					r.Route("/members/{memberId}", func(r chi.Router) {
						r.Patch("/", h.UpdateMember)
						r.Delete("/", h.DeleteMember)
					})
					r.Delete("/invitations/{invitationId}", h.RevokeInvitation)

					r.Post("/runtime-profiles", h.CreateRuntimeProfile)
					r.Patch("/runtime-profiles/{profileId}", h.UpdateRuntimeProfile)
					r.Put("/runtime-profiles/{profileId}", h.UpdateRuntimeProfile)
					r.Delete("/runtime-profiles/{profileId}", h.DeleteRuntimeProfile)
				})

				r.With(middleware.RequireWorkspaceRoleFromURL(queries, "id", "owner")).Delete("/", h.DeleteWorkspace)

				r.Route("/export", func(r chi.Router) {
					r.Use(middleware.RequireWorkspaceRoleFromURL(queries, "id", "owner"))
					r.Use(handler.RequireHumanActor)
					r.Post("/", h.StartWorkspaceExport)
					r.Get("/{jobId}", h.GetWorkspaceExport)
					r.Get("/{jobId}/download", h.DownloadWorkspaceExport)
				})

				r.Group(func(r chi.Router) {
					r.Use(middleware.RequireWorkspaceRoleFromURL(queries, "id", "owner", "admin"))
					r.Get("/github/connect", h.GitHubConnect)
					r.Get("/github/installations/{installationId}/repositories", h.ListGitHubInstallationRepositories)
					r.Delete("/github/installations/{installationId}", h.DeleteGitHubInstallation)

					r.Post("/vcs/connections", h.ConnectVCS)
					r.Post("/vcs/connections/{connectionId}/rotate-webhook", h.RotateVCSConnectionWebhook)
					r.Delete("/vcs/connections/{connectionId}", h.DeleteVCSConnection)
				})

				r.Group(func(r chi.Router) {
					r.Use(middleware.RequireWorkspaceMemberFromURL(queries, "id"))
					r.Get("/slack/installations", h.ListSlackInstallations)
				})
				r.Group(func(r chi.Router) {
					r.Use(middleware.RequireWorkspaceRoleFromURL(queries, "id", "owner", "admin"))
					r.Delete("/slack/installations/{installationId}", h.RevokeSlackInstallation)
					r.Post("/slack/install/byo", h.RegisterSlackBYO)
				})
			})
		})

		r.Post("/api/slack/binding/redeem", h.RedeemSlackBindingToken)

		r.Route("/api/integrations/composio", func(r chi.Router) {
			r.Post("/connect/init", h.ComposioConnectInit)
			r.Get("/toolkits", h.ListComposioToolkits)
			r.Get("/connections", h.ListComposioConnections)
			r.Delete("/connections/{id}", h.DeleteComposioConnection)
		})

		r.Get("/api/invitations", h.ListMyInvitations)
		r.Get("/api/invitations/{id}", h.GetMyInvitation)
		r.Post("/api/invitations/{id}/accept", h.AcceptInvitation)
		r.Post("/api/invitations/{id}/decline", h.DeclineInvitation)

		r.Route("/api/tokens", func(r chi.Router) {
			r.Use(handler.RequireHumanActor)

			r.Use(tokenRL)
			r.Get("/", h.ListPersonalAccessTokens)
			r.Post("/", h.CreatePersonalAccessToken)
			r.Post("/current/renew", h.RenewCurrentPersonalAccessToken)
			r.Delete("/{id}", h.RevokePersonalAccessToken)
		})

		r.Route("/api/cloud-billing", func(r chi.Router) {
			r.Use(handler.RequireHumanActor)

			r.Get("/balance", h.GetCloudBillingBalance)
			r.Get("/transactions", h.ListCloudBillingTransactions)
			r.Get("/batches", h.ListCloudBillingBatches)
			r.Get("/topups", h.ListCloudBillingTopups)
			r.Get("/price-tiers", h.ListCloudBillingPriceTiers)
			r.Post("/checkout-sessions", h.CreateCloudBillingCheckoutSession)
			r.Get("/checkout-sessions/{sessionId}", h.GetCloudBillingCheckoutSession)
			r.Post("/portal-sessions", h.CreateCloudBillingPortalSession)
		})

		r.Route("/api/deployment", func(r chi.Router) {
			r.Use(handler.RequireHumanActor)
			r.Get("/admins", h.ListDeploymentAdmins)
			r.Post("/admins", h.AddDeploymentAdmin)
			r.Delete("/admins/{userId}", h.RemoveDeploymentAdmin)

			r.Get("/admins/pending", h.ListDeploymentAdminPending)
			r.Get("/audit", h.ListDeploymentAdminAudit)

			r.Post("/users/{userId}/deactivate", h.DeactivateDeploymentUser)
			r.Post("/users/{userId}/reactivate", h.ReactivateDeploymentUser)

			r.Post("/users/{userId}/revoke-sessions", h.RevokeDeploymentUserSessions)

			r.Delete("/users/{userId}", h.DeleteDeploymentUser)

			r.Route("/mcp-servers", func(r chi.Router) {
				r.Get("/", h.ListDeploymentMcpServers)
				r.Post("/", h.CreateDeploymentMcpServer)
				r.Put("/{serverId}", h.UpdateDeploymentMcpServer)
				r.Delete("/{serverId}", h.DeleteDeploymentMcpServer)
			})
			r.Get("/policy", h.GetDeploymentPolicyAsAdmin)
			r.Put("/policy", h.PutDeploymentPolicy)

			r.Get("/workspaces", h.ListDeploymentWorkspaces)

			r.Get("/join-targets", h.ListJoinTargets)
			r.With(joinRL).Post("/join-targets/{workspaceId}/join", h.JoinTarget)

			r.Get("/fleet", h.ListDeploymentFleet)

			r.Route("/workspaces/{workspaceId}", func(r chi.Router) {
				r.Use(handler.WorkspaceIDFromPathParam)
				r.Get("/members", h.ListDeploymentWorkspaceMembers)

				r.Patch("/", h.SetDeploymentWorkspaceOpenJoin)
				r.Get("/config", h.GetWorkspaceConfig)
				r.Put("/config", h.PutWorkspaceConfig)

				r.Get("/config/overrides", h.ListWorkspaceUserConfigOverrides)
				r.Route("/config/overrides/{userId}", func(r chi.Router) {
					r.Get("/", h.GetWorkspaceUserConfigOverride)
					r.Put("/", h.PutWorkspaceUserConfigOverride)
					r.Delete("/", h.DeleteWorkspaceUserConfigOverride)
				})
			})
		})

		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireWorkspaceMember(queries))

			r.Get("/api/assignee-frequency", h.GetAssigneeFrequency)

			r.Get("/api/status", h.GetGoosarStatus)

			r.Route("/api/provisioning", func(r chi.Router) {
				r.Get("/manifest", h.GetProvisioningManifest)
				r.Get("/blob/{name}/{version}", h.GetProvisioningBlob)

				r.With(handler.RequireHumanActor).Get("/catalog", h.GetProvisioningCatalog)
				r.Route("/pins", func(r chi.Router) {
					r.Use(handler.RequireHumanActor)
					r.Get("/", h.GetProvisioningPins)
					r.Put("/", h.PutProvisioningPins)
				})
			})

			r.Route("/api/workspace-config", func(r chi.Router) {
				r.Use(handler.RequireHumanActor)
				r.Get("/", h.GetWorkspaceConfig)
				r.Put("/", h.PutWorkspaceConfig)
				r.Route("/overrides/{userId}", func(r chi.Router) {
					r.Get("/", h.GetWorkspaceUserConfigOverride)
					r.Put("/", h.PutWorkspaceUserConfigOverride)
					r.Delete("/", h.DeleteWorkspaceUserConfigOverride)
				})
			})

			r.Route("/api/workspace-mcp-servers", func(r chi.Router) {
				r.Use(handler.RequireHumanActor)
				r.Get("/", h.ListWorkspaceMcpServers)
				r.Post("/", h.CreateWorkspaceMcpServer)
				r.Put("/{serverId}", h.UpdateWorkspaceMcpServer)
				r.Delete("/{serverId}", h.DeleteWorkspaceMcpServer)

				r.Put("/{serverId}/credentials", h.SetWorkspaceMcpCredentials)
				r.Delete("/{serverId}/credentials", h.DeleteWorkspaceMcpCredentials)
			})

			r.Route("/api/deployment-mcp-servers", func(r chi.Router) {
				r.Use(handler.RequireHumanActor)
				r.Get("/", h.ListWorkspaceDeploymentMcpServers)
				r.Put("/{serverId}/enabled", h.SetWorkspaceDeploymentMcpServerEnabled)
			})
			r.With(handler.RequireHumanActor).Get("/api/deployment-policy", h.GetDeploymentPolicy)

			r.With(handler.RequireHumanActor).Get("/api/effective-config", h.GetEffectiveConfigView)

			r.With(handler.RequireHumanActor).Get("/api/deployment/client-secrets", h.GetDeploymentClientSecrets)

			r.With(handler.RequireHumanActor).Get("/api/llm/health", h.GetLLMHealth)

			r.Route("/api/issues", func(r chi.Router) {
				r.Post("/table/groups", h.ListIssueTableGroups)
				r.Post("/table/rows", h.ListIssueTableRows)
				r.Post("/table/facets", h.ListIssueTableFacets)
				r.Get("/search", h.SearchIssues)
				r.Get("/child-progress", h.ChildIssueProgress)
				r.Get("/children", h.ListChildrenByParents)
				r.Get("/grouped", h.ListGroupedIssues)
				r.Get("/", h.ListIssues)

				r.Post("/query", h.QueryIssues)
				r.Post("/", h.CreateIssue)
				r.Post("/quick-create", h.QuickCreateIssue)
				r.Post("/preview-trigger", h.PreviewIssueTrigger)
				r.Post("/batch-update", h.BatchUpdateIssues)
				r.Post("/batch-delete", h.BatchDeleteIssues)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", h.GetIssue)
					r.Put("/", h.UpdateIssue)
					r.Post("/move", h.MoveIssue)
					r.Delete("/", h.DeleteIssue)
					r.Post("/comments/trigger-preview", h.PreviewCommentTriggers)
					r.Post("/comments", h.CreateComment)
					r.Get("/comments", h.ListComments)
					r.Get("/timeline", h.ListTimeline)
					r.Get("/subscribers", h.ListIssueSubscribers)
					r.Post("/subscribe", h.SubscribeToIssue)
					r.Post("/unsubscribe", h.UnsubscribeFromIssue)
					r.Get("/active-task", h.GetActiveTaskForIssue)
					r.Post("/tasks/{taskId}/cancel", h.CancelTask)
					r.Post("/rerun", h.RerunIssue)
					r.Get("/task-runs", h.ListTasksByIssue)
					r.Get("/usage", h.GetIssueUsage)
					r.Post("/reactions", h.AddIssueReaction)
					r.Delete("/reactions", h.RemoveIssueReaction)
					r.Get("/attachments", h.ListAttachments)
					r.Get("/children", h.ListChildIssues)
					r.Get("/labels", h.ListLabelsForIssue)
					r.Post("/labels", h.AttachLabel)
					r.Delete("/labels/{labelId}", h.DetachLabel)
					r.Get("/metadata", h.ListIssueMetadata)
					r.Put("/metadata/{key}", h.SetIssueMetadataKey)
					r.Delete("/metadata/{key}", h.DeleteIssueMetadataKey)
					r.Put("/properties/{propertyId}", h.SetIssueProperty)
					r.Delete("/properties/{propertyId}", h.DeleteIssueProperty)
					r.Get("/pull-requests", h.ListPullRequestsForIssue)
				})
			})

			r.Get("/api/tasks/{taskId}/messages", h.ListTaskMessagesByUser)

			r.Route("/api/properties", func(r chi.Router) {
				r.Get("/", h.ListProperties)
				r.Post("/", h.CreateProperty)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", h.GetProperty)
					r.Patch("/", h.UpdateProperty)
				})
			})

			r.Route("/api/labels", func(r chi.Router) {
				r.Get("/", h.ListLabels)
				r.Post("/", h.CreateLabel)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", h.GetLabel)
					r.Put("/", h.UpdateLabel)
					r.Delete("/", h.DeleteLabel)
				})
			})

			r.Route("/api/projects", func(r chi.Router) {
				r.Get("/search", h.SearchProjects)
				r.Get("/", h.ListProjects)
				r.Post("/", h.CreateProject)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", h.GetProject)
					r.Put("/", h.UpdateProject)
					r.Delete("/", h.DeleteProject)
					r.Get("/resources", h.ListProjectResources)
					r.Post("/resources", h.CreateProjectResource)
					r.Put("/resources/{resourceId}", h.UpdateProjectResource)
					r.Delete("/resources/{resourceId}", h.DeleteProjectResource)
				})
			})

			r.Route("/api/squads", func(r chi.Router) {
				r.Get("/", h.ListSquads)
				r.Post("/", h.CreateSquad)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", h.GetSquad)
					r.Put("/", h.UpdateSquad)
					r.Delete("/", h.DeleteSquad)
					r.Get("/members", h.ListSquadMembers)
					r.Get("/members/status", h.ListSquadMemberStatus)
					r.Post("/members", h.AddSquadMember)
					r.Delete("/members", h.RemoveSquadMember)
					r.Patch("/members/role", h.UpdateSquadMemberRole)
				})
			})

			r.Post("/api/issues/{id}/squad-evaluated", h.RecordSquadLeaderEvaluation)

			r.Route("/api/autopilots", func(r chi.Router) {
				r.Get("/", h.ListAutopilots)

				r.With(handler.RequireHumanActor).Post("/", h.CreateAutopilot)
				r.Get("/cron-preview", h.CronPreview)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", h.GetAutopilot)
					r.With(handler.RequireHumanActor).Patch("/", h.UpdateAutopilot)
					r.With(handler.RequireHumanActor).Delete("/", h.DeleteAutopilot)

					r.Post("/trigger", h.TriggerAutopilot)
					r.Get("/runs", h.ListAutopilotRuns)
					r.Get("/runs/{runId}", h.GetAutopilotRun)
					r.Get("/deliveries", h.ListAutopilotDeliveries)
					r.Get("/deliveries/{deliveryId}", h.GetAutopilotDelivery)
					r.Post("/deliveries/{deliveryId}/replay", h.ReplayAutopilotDelivery)

					r.With(handler.RequireHumanActor).Post("/triggers", h.CreateAutopilotTrigger)
					r.Route("/triggers/{triggerId}", func(r chi.Router) {
						r.Use(handler.RequireHumanActor)
						r.Patch("/", h.UpdateAutopilotTrigger)
						r.Delete("/", h.DeleteAutopilotTrigger)
						r.Post("/rotate-webhook-token", h.RotateAutopilotTriggerWebhookToken)
						r.Put("/signing-secret", h.SetAutopilotTriggerSigningSecret)
					})
					r.With(handler.RequireHumanActor).Post("/collaborators", h.AddAutopilotCollaborator)
					r.With(handler.RequireHumanActor).Delete("/collaborators/{userId}", h.RemoveAutopilotCollaborator)
				})
			})

			r.Route("/api/pins", func(r chi.Router) {
				r.Get("/", h.ListPins)
				r.Post("/", h.CreatePin)
				r.Put("/reorder", h.ReorderPins)
				r.Delete("/{itemType}/{itemId}", h.DeletePin)
			})

			r.Get("/api/attachments/{id}", h.GetAttachmentByID)

			r.Get("/api/attachments/{id}/content", h.GetAttachmentContent)
			r.Delete("/api/attachments/{id}", h.DeleteAttachment)

			r.Route("/api/comments/{commentId}", func(r chi.Router) {
				r.Put("/", h.UpdateComment)
				r.Delete("/", h.DeleteComment)
				r.Post("/resolve", h.ResolveComment)
				r.Delete("/resolve", h.UnresolveComment)
				r.Post("/reactions", h.AddReaction)
				r.Delete("/reactions", h.RemoveReaction)
			})

			r.Route("/api/agents", func(r chi.Router) {
				r.Get("/", h.ListAgents)
				r.Post("/", h.CreateAgent)

				r.Post("/from-template", h.CreateAgentFromTemplate)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", h.GetAgent)
					r.Put("/", h.UpdateAgent)
					r.Post("/archive", h.ArchiveAgent)
					r.Post("/restore", h.RestoreAgent)
					r.Post("/cancel-tasks", h.CancelAgentTasks)
					r.Get("/tasks", h.ListAgentTasks)
					r.Get("/skills", h.ListAgentSkills)
					r.Put("/skills", h.SetAgentSkills)
					r.Post("/skills/add", h.AddAgentSkills)
					r.Get("/labels", h.ListLabelsForAgent)
					r.Post("/labels", h.AttachLabelToAgent)
					r.Delete("/labels/{labelId}", h.DetachLabelFromAgent)
					r.Put("/skills/{skillId}/enabled", h.SetAgentSkillEnabled)
					r.Put("/runtime-skills/enabled", h.SetAgentRuntimeSkillEnabled)
					r.Delete("/skills/{skillId}", h.RemoveAgentSkill)

					r.Get("/env", h.GetAgentEnv)
					r.Put("/env", h.UpdateAgentEnv)

					r.Get("/mcp-servers", h.ListAgentMcpServers)
					r.Post("/mcp-servers", h.AddAgentMcpServer)
					r.Put("/mcp-servers/{serverId}/enabled", h.SetAgentMcpServerEnabled)
					r.Delete("/mcp-servers/{serverId}", h.RemoveAgentMcpServer)
				})
			})

			r.Route("/api/agent-templates", func(r chi.Router) {
				r.Get("/", h.ListAgentTemplates)
				r.Get("/{slug}", h.GetAgentTemplate)
			})
			r.Route("/api/agent-builder/sessions", func(r chi.Router) {
				r.Post("/", h.CreateAgentBuilderSession)
				r.Patch("/{sessionId}/runtime", h.SwitchAgentBuilderRuntime)
			})

			r.Route("/api/skills", func(r chi.Router) {
				r.Get("/", h.ListSkills)
				r.Post("/", h.CreateSkill)
				r.Get("/search", h.SearchSkills)
				r.Post("/import", h.ImportSkill)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", h.GetSkill)
					r.Put("/", h.UpdateSkill)
					r.Delete("/", h.DeleteSkill)
					r.Get("/labels", h.ListLabelsForSkill)
					r.Post("/labels", h.AttachLabelToSkill)
					r.Delete("/labels/{labelId}", h.DetachLabelFromSkill)
					r.Get("/files", h.ListSkillFiles)
					r.Put("/files", h.UpsertSkillFile)
					r.Delete("/files/{fileId}", h.DeleteSkillFile)
				})
			})

			r.Route("/api/dashboard", func(r chi.Router) {
				r.Get("/usage/daily", h.GetDashboardUsageDaily)
				r.Get("/usage/by-agent", h.GetDashboardUsageByAgent)
				r.Get("/agent-runtime", h.GetDashboardAgentRunTime)
				r.Get("/runtime/daily", h.GetDashboardRunTimeDaily)
				r.Get("/failures/daily", h.GetDashboardFailuresDaily)
				r.Get("/failures/by-agent", h.GetDashboardFailuresByAgent)
			})

			r.Route("/api/runtimes", func(r chi.Router) {
				r.Get("/", h.ListAgentRuntimes)
				r.Route("/{runtimeId}", func(r chi.Router) {
					r.Patch("/", h.UpdateAgentRuntime)

					r.Post("/mcp-verified", h.ReportMcpVerified)
					r.Get("/usage", h.GetRuntimeUsage)
					r.Get("/usage/by-agent", h.GetRuntimeUsageByAgent)
					r.Get("/usage/by-hour", h.GetRuntimeUsageByHour)
					r.Get("/activity", h.GetRuntimeTaskActivity)
					r.Post("/update", h.InitiateUpdate)
					r.Get("/update/{updateId}", h.GetUpdate)
					r.Post("/models", h.InitiateListModels)
					r.Get("/models/{requestId}", h.GetModelListRequest)
					r.Post("/local-skills", h.InitiateListLocalSkills)
					r.Get("/local-skills/{requestId}", h.GetLocalSkillListRequest)
					r.Post("/local-skills/import", h.InitiateImportLocalSkill)
					r.Get("/local-skills/import/{requestId}", h.GetLocalSkillImportRequest)
					r.Delete("/", h.DeleteAgentRuntime)

					r.Post("/archive-agents-and-delete", h.ArchiveAgentsAndDeleteRuntime)
				})
			})

			r.Route("/api/cloud-runtime", func(r chi.Router) {
				r.Get("/", h.GetCloudRuntimeService)
				r.Get("/healthz", h.GetCloudRuntimeHealth)
				r.Get("/readyz", h.GetCloudRuntimeReady)
				r.Get("/nodes", h.ListCloudRuntimeNodes)
				r.Post("/nodes", h.CreateCloudRuntimeNode)
				r.Delete("/nodes", h.DeleteCloudRuntimeNode)
				r.Post("/nodes/start", h.StartCloudRuntimeNode)
				r.Post("/nodes/stop", h.StopCloudRuntimeNode)
				r.Post("/nodes/reboot", h.RebootCloudRuntimeNode)
				r.Post("/nodes/status", h.GetCloudRuntimeNodeStatus)
				r.Post("/nodes/exec", h.ExecCloudRuntimeNode)
			})

			r.Post("/api/tasks/{taskId}/cancel", h.CancelTaskByUser)

			r.Get("/api/agent-task-snapshot", h.ListWorkspaceAgentTaskSnapshot)

			r.Get("/api/working-agents", h.ListWorkspaceWorkingAgents)

			r.Get("/api/agent-activity-30d", h.GetWorkspaceAgentActivity30d)

			r.Get("/api/agent-run-counts", h.GetWorkspaceAgentRunCounts)

			r.Route("/api/chat/sessions", func(r chi.Router) {
				r.Post("/", h.CreateChatSession)
				r.Get("/", h.ListChatSessions)
				r.Route("/{sessionId}", func(r chi.Router) {
					r.Get("/", h.GetChatSession)
					r.Patch("/", h.UpdateChatSession)
					r.Patch("/pin", h.SetChatSessionPinned)
					r.Patch("/archive", h.SetChatSessionArchived)
					r.Delete("/", h.DeleteChatSession)
					r.Post("/messages", h.SendChatMessage)
					r.Get("/messages", h.ListChatMessages)
					r.Get("/messages/page", h.ListChatMessagesPage)
					r.Get("/pending-task", h.GetPendingChatTask)
					r.Post("/read", h.MarkChatSessionRead)

					r.Get("/draft-restores", h.ListChatDraftRestores)
					r.Delete("/draft-restores/{restoreId}", h.ConsumeChatDraftRestore)
				})
			})
			r.Get("/api/chat/pending-tasks", h.ListPendingChatTasks)
			r.Get("/api/chat/pending-tasks/has-any", h.HasPendingChatTasks)

			r.Get("/api/chat/pinned-agents", h.ListChatPinnedAgents)
			r.Post("/api/chat/pinned-agents", h.PinChatAgent)
			r.Delete("/api/chat/pinned-agents/{agentId}", h.UnpinChatAgent)

			r.Get("/api/chat/history", h.GetChatChannelHistory)
			r.Get("/api/chat/thread", h.GetChatThread)

			r.Route("/api/inbox", func(r chi.Router) {
				r.Get("/", h.ListInbox)

				r.Get("/archived", h.ListArchivedInbox)
				r.Get("/unread-count", h.CountUnreadInbox)

				r.Get("/unread-summary", h.UnreadInboxSummary)
				r.Post("/mark-all-read", h.MarkAllInboxRead)
				r.Post("/archive-all", h.ArchiveAllInbox)
				r.Post("/archive-all-read", h.ArchiveAllReadInbox)
				r.Post("/archive-completed", h.ArchiveCompletedInbox)
				r.Post("/{id}/read", h.MarkInboxRead)
				r.Post("/{id}/archive", h.ArchiveInboxItem)
				r.Post("/{id}/unarchive", h.UnarchiveInboxItem)
			})

			r.Route("/api/notification-preferences", func(r chi.Router) {
				r.Get("/", h.GetNotificationPreferences)
				r.Patch("/", h.PatchNotificationPreferences)
				r.Put("/", h.UpdateNotificationPreferences)
			})
		})
	})

	return r, h
}

type membershipChecker struct {
	queries *db.Queries
}

func (mc *membershipChecker) IsMember(ctx context.Context, userID, workspaceID string, claimedVersion *int32) bool {

	uid, err := util.ParseUUID(userID)
	if err != nil {
		return false
	}
	wsID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return false
	}

	state, err := mc.queries.GetUserAuthState(ctx, uid)
	if err != nil || state.DeactivatedAt.Valid {
		return false
	}
	if claimedVersion != nil && *claimedVersion != state.TokenVersion {
		return false
	}
	_, err = mc.queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      uid,
		WorkspaceID: wsID,
	})
	return err == nil
}

type patResolver struct {
	queries *db.Queries
	cache   *auth.PATCache
}

func (pr *patResolver) ResolveToken(ctx context.Context, token string) (string, bool) {
	hash := auth.HashToken(token)

	if userID, ok := pr.cache.Get(ctx, hash); ok {
		return userID, true
	}

	pat, err := pr.queries.GetPersonalAccessTokenByHash(ctx, hash)
	if err != nil {
		return "", false
	}

	userID := util.UUIDToString(pat.UserID)

	var expiresAt time.Time
	if pat.ExpiresAt.Valid {
		expiresAt = pat.ExpiresAt.Time
	}
	pr.cache.Set(ctx, hash, userID, auth.TTLForExpiry(time.Now(), expiresAt))

	go pr.queries.UpdatePersonalAccessTokenLastUsed(context.Background(), pat.ID)

	return userID, true
}

func parseUUID(s string) pgtype.UUID {
	return util.MustParseUUID(s)
}

func optionalUUID(s string) pgtype.UUID {
	if s == "" {
		return pgtype.UUID{}
	}
	return util.MustParseUUID(s)
}

func splitAndTrim(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	res := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			res = append(res, trimmed)
		}
	}
	return res
}

func cloudRuntimeFleetURLFromEnv() string {
	if url := strings.TrimSpace(os.Getenv("GOOSAR_CLOUD_FLEET_URL")); url != "" {
		return url
	}
	return strings.TrimSpace(os.Getenv("GOOSAR_FLEET_URL"))
}

func composioStateSecret() []byte {
	if v := strings.TrimSpace(os.Getenv("COMPOSIO_STATE_SECRET")); v != "" {
		return []byte(v)
	}
	if v := strings.TrimSpace(os.Getenv("JWT_SECRET")); v != "" {
		sum := sha256.Sum256([]byte("composio-state:" + v))
		return sum[:]
	}
	return nil
}

func composioCallbackBaseURL(publicURL string) string {
	if v := strings.TrimRight(strings.TrimSpace(os.Getenv("COMPOSIO_CALLBACK_BASE_URL")), "/"); v != "" {
		return v
	}
	if publicURL != "" {
		return publicURL
	}
	return appURLFromEnv()
}
