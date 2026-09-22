package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/adanman/goosar/server/internal/analytics"
	"github.com/adanman/goosar/server/internal/auth"
	"github.com/adanman/goosar/server/internal/daemonws"
	"github.com/adanman/goosar/server/internal/deliveryprofile"
	"github.com/adanman/goosar/server/internal/events"
	"github.com/adanman/goosar/server/internal/handler"
	"github.com/adanman/goosar/server/internal/integrations/plugin"
	"github.com/adanman/goosar/server/internal/logger"
	obsmetrics "github.com/adanman/goosar/server/internal/metrics"
	"github.com/adanman/goosar/server/internal/realtime"
	"github.com/adanman/goosar/server/internal/scheduler"
	"github.com/adanman/goosar/server/internal/service"
	"github.com/adanman/goosar/server/internal/storage"
	"github.com/adanman/goosar/server/internal/util/secretbox"
	db "github.com/adanman/goosar/server/pkg/db/generated"
	"github.com/adanman/goosar/server/pkg/featureflag"
)

var (
	version = "dev"
	commit  = "unknown"
)

func newNamedRedisClient(base *redis.Options, suffix string) *redis.Client {
	opts := *base
	if envBool("REDIS_DISABLE_CLIENT_NAME", false) {
		opts.ClientName = ""
	} else {
		opts.ClientName = redisClientName(opts.ClientName, suffix)
	}
	return redis.NewClient(&opts)
}

func redisClientName(existing, suffix string) string {
	if suffix == "" {
		return existing
	}
	if existing != "" {
		return existing + ":" + suffix
	}
	return "goosar-api:" + suffix
}

func closeRedisClient(label string, client *redis.Client) {
	if client == nil {
		return
	}
	if err := client.Close(); err != nil {
		slog.Warn("redis client close failed", "client", label, "error", err)
	}
}

func shardedRelayConfigFromEnv() realtime.ShardedStreamRelayConfig {
	cfg := realtime.DefaultShardedStreamRelayConfig()
	cfg.Shards = envPositiveInt("REALTIME_RELAY_SHARDS", cfg.Shards)
	cfg.StreamMaxLen = envPositiveInt64("REALTIME_RELAY_STREAM_MAXLEN", cfg.StreamMaxLen)
	cfg.ReadCount = envPositiveInt64("REALTIME_RELAY_XREAD_COUNT", cfg.ReadCount)
	cfg.ReadBlock = envDuration("REALTIME_RELAY_XREAD_BLOCK", cfg.ReadBlock)
	cfg.ReplayGrace = envDuration("REALTIME_RELAY_REPLAY_GRACE", cfg.ReplayGrace)
	return cfg
}

func realtimeRelayModeFromEnv() string {
	const defaultMode = "sharded"
	raw := strings.ToLower(strings.TrimSpace(os.Getenv("REALTIME_RELAY_MODE")))
	if raw == "" {
		return defaultMode
	}
	switch raw {
	case "sharded", "dual", "legacy":
		return raw
	default:
		slog.Warn("invalid env var, using default", "name", "REALTIME_RELAY_MODE", "value", raw, "default", defaultMode)
		return defaultMode
	}
}

func envPositiveInt(name string, def int) int {
	raw := os.Getenv(name)
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		slog.Warn("invalid env var, using default", "name", name, "value", raw, "default", def, "error", err)
		return def
	}
	return v
}

func envPositiveInt64(name string, def int64) int64 {
	raw := os.Getenv(name)
	if raw == "" {
		return def
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v <= 0 {
		slog.Warn("invalid env var, using default", "name", name, "value", raw, "default", def, "error", err)
		return def
	}
	return v
}

func envDuration(name string, def time.Duration) time.Duration {
	raw := os.Getenv(name)
	if raw == "" {
		return def
	}
	v, err := time.ParseDuration(raw)
	if err != nil || v <= 0 {
		slog.Warn("invalid env var, using default", "name", name, "value", raw, "default", def.String(), "error", err)
		return def
	}
	return v
}

func envNonNegativeDuration(name string, def time.Duration) time.Duration {
	raw := os.Getenv(name)
	if raw == "" {
		return def
	}
	v, err := time.ParseDuration(raw)
	if err != nil || v < 0 {
		slog.Warn("invalid env var, using default", "name", name, "value", raw, "default", def.String(), "error", err)
		return def
	}
	return v
}

func holdBeforeShutdown(sig os.Signal, signals <-chan os.Signal, duration time.Duration) {
	if duration <= 0 {
		return
	}
	slog.Info("termination signal received; holding before shutdown",
		"signal", sig.String(),
		"duration", duration.String(),
	)
	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-timer.C:
		slog.Info("shutdown hold complete", "duration", duration.String())
	case interruptSig := <-signals:
		slog.Info("shutdown hold interrupted by signal",
			"signal", interruptSig.String(),
			"configured_duration", duration.String(),
		)
	}
}

func envBool(name string, def bool) bool {
	raw := os.Getenv(name)
	if raw == "" {
		return def
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		slog.Warn("invalid env var, using default", "name", name, "value", raw, "default", def, "error", err)
		return def
	}
	return v
}

func main() {
	logger.Init()

	if err := auth.ValidateJWTSecret(); err != nil {
		slog.Error("refusing to start: invalid JWT configuration", "error", err)
		os.Exit(1)
	}
	if err := storage.ValidateS3Env(); err != nil {
		slog.Error("refusing to start: invalid S3 storage configuration", "error", err)
		os.Exit(1)
	}
	if err := secretbox.ValidateKeyEnv("GOOSAR_MCP_SECRET_KEY"); err != nil {
		slog.Error("refusing to start: invalid MCP secret key configuration", "error", err)
		os.Exit(1)
	}

	if err := service.ValidateEmailConfig(); err != nil {
		slog.Error("refusing to start: invalid email configuration", "error", err)
		os.Exit(1)
	}

	if auth.UsingDevJWTSecret() {
		slog.Warn("JWT_SECRET is unset or a known placeholder — using an INSECURE development signing secret. Anyone can forge sessions. Set JWT_SECRET to a strong random value (e.g. `openssl rand -hex 32`); APP_ENV=production refuses to start in this state.")
	}

	if auth.UsingPreviousJWTSecrets() {
		slog.Warn("JWT_SECRET_PREVIOUS is set — retired signing secrets are still accepted for verification. This is a rotation grace window, not a steady state: a leaked retired secret forges sessions for as long as it is listed. Empty JWT_SECRET_PREVIOUS and restart once one auth-token TTL has passed.")
	}
	if os.Getenv("RESEND_API_KEY") == "" && strings.TrimSpace(os.Getenv("SMTP_HOST")) == "" {
		if service.NewEmailService().Transport() == service.TransportNone {
			slog.Error("no email backend configured (RESEND_API_KEY and SMTP_HOST both empty) on a production/perimeter deployment — sign-in is REFUSED with email_not_configured until SMTP_HOST or RESEND_API_KEY is set.")
		} else {
			slog.Warn("no email backend configured (RESEND_API_KEY and SMTP_HOST both empty) — verification codes will be printed to the log instead of emailed.")
		}
	}
	if os.Getenv("GOOSAR_DEV_VERIFICATION_CODE") != "" {
		if strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "production") {
			slog.Warn("GOOSAR_DEV_VERIFICATION_CODE is set but ignored because APP_ENV=production.")
		} else {
			slog.Warn("GOOSAR_DEV_VERIFICATION_CODE is enabled. Use it only for local development or private test instances.")
		}
	}

	if strings.TrimSpace(os.Getenv("GOOSAR_MCP_SECRET_KEY")) == "" {
		profile, _ := deliveryprofile.FromEnv()
		if err := handler.RequireSecretKeyForPerimeter(profile, false); err != nil {
			slog.Error("refusing to start", "error", err)
			os.Exit(1)
		}
		slog.Warn(handler.MissingSecretKeyWarning())
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}
	shutdownHoldDuration := envNonNegativeDuration("GOOSAR_SHUTDOWN_HOLD_DURATION", 0)

	flags, err := featureflag.NewServiceFromEnv(featureflag.WithLogger(slog.Default()))
	if err != nil {
		slog.Error("feature flag configuration failed to load", "error", err)
		os.Exit(1)
	}
	_ = flags

	if err := checkReplicaTopology(os.Getenv(replicasEnvVar), os.Getenv("REDIS_URL")); err != nil {
		slog.Error("refusing to start", "error", err)
		os.Exit(1)
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://goosar:goosar@localhost:5432/goosar?sslmode=disable"
	}

	ctx := context.Background()
	pool, err := newDBPool(ctx, dbURL)
	if err != nil {
		slog.Error("unable to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		slog.Error("unable to ping database", "error", err)
		os.Exit(1)
	}
	slog.Info("connected to database")
	logPoolConfig(pool)

	bus := events.New()
	hub := realtime.NewHub()
	go hub.Run()
	daemonHub := daemonws.NewHub()
	var daemonWakeup service.TaskWakeupNotifier = daemonHub

	relayCtx, relayCancel := context.WithCancel(context.Background())
	var broadcaster realtime.Broadcaster = hub
	var storeRedis *redis.Client
	var relayWriteRedis *redis.Client
	var relayReadRedis *redis.Client
	var shardedReadRedis *redis.Client
	var legacyReadRedis *redis.Client
	var relay realtime.ManagedRelay
	defer func() {
		if relay != nil {
			relay.Stop()
		}
		relayCancel()
		if relay != nil {
			relay.Wait()
		}
		closeRedisClient("realtime-read-legacy", legacyReadRedis)
		closeRedisClient("realtime-read-sharded", shardedReadRedis)
		closeRedisClient("realtime-read", relayReadRedis)
		closeRedisClient("realtime-write", relayWriteRedis)
		closeRedisClient("store", storeRedis)
	}()
	if redisURL := os.Getenv("REDIS_URL"); redisURL != "" {
		opts, err := redis.ParseURL(redisURL)
		if err != nil {
			slog.Error("invalid REDIS_URL — falling back to in-memory hub", "error", err)
		} else {
			if envBool("REDIS_DISABLE_CLIENT_NAME", false) {
				slog.Info("redis: CLIENT SETNAME disabled (REDIS_DISABLE_CLIENT_NAME=true) for managed Redis compatibility")
			}
			storeRedis = newNamedRedisClient(opts, "store")
			relayWriteRedis = newNamedRedisClient(opts, "realtime-write")

			relayMode := realtimeRelayModeFromEnv()
			relayConfig := shardedRelayConfigFromEnv()
			switch relayMode {
			case "legacy":
				relayReadRedis = newNamedRedisClient(opts, "realtime-read")
				relay = realtime.NewRedisRelayWithClients(hub, relayWriteRedis, relayReadRedis)
				slog.Info("daemon websocket wakeup: Redis fanout disabled in legacy realtime relay mode")
			case "dual":
				shardedReadRedis = newNamedRedisClient(opts, "realtime-read-sharded")
				legacyReadRedis = newNamedRedisClient(opts, "realtime-read-legacy")
				sharded := realtime.NewShardedStreamRelay(hub, relayWriteRedis, shardedReadRedis, relayConfig)
				sharded.SetDaemonRuntimeDeliverer(daemonHub)
				legacy := realtime.NewRedisRelayWithClients(hub, relayWriteRedis, legacyReadRedis)
				relay = realtime.NewMirroredRelay(sharded, legacy)
				daemonWakeup = daemonws.NewRelayNotifier(daemonHub, sharded)
			default:
				relayReadRedis = newNamedRedisClient(opts, "realtime-read")
				sharded := realtime.NewShardedStreamRelay(hub, relayWriteRedis, relayReadRedis, relayConfig)
				sharded.SetDaemonRuntimeDeliverer(daemonHub)
				relay = sharded
				daemonWakeup = daemonws.NewRelayNotifier(daemonHub, sharded)
			}
			relay.Start(relayCtx)
			broadcaster = realtime.NewDualWriteBroadcaster(hub, relay)
			slog.Info(
				"realtime: Redis relay enabled",
				"node_id", relay.NodeID(),
				"mode", relayMode,
				"shards", relayConfig.Shards,
				"stream_max_len", relayConfig.StreamMaxLen,
				"xread_count", relayConfig.ReadCount,
				"xread_block", relayConfig.ReadBlock.String(),
				"store_pool_size", opts.PoolSize,
				"realtime_write_pool_size", opts.PoolSize,
				"realtime_read_pool_size", opts.PoolSize,
			)
		}
	} else {
		slog.Info("realtime: REDIS_URL not set — using in-memory hub (single-node mode)")
	}
	registerListeners(bus, broadcaster)

	analyticsClient := analytics.NewFromEnv()
	defer analyticsClient.Close()

	queries := db.New(pool)

	if err := handler.SeedDeploymentAdmins(ctx, pool, queries, os.Getenv(handler.DeploymentAdminEmailsEnvVar)); err != nil {
		slog.Error("deployment admin bootstrap failed — deployment admin surface may be unreachable until the next restart", "error", err)
	}

	if err := handler.RequireSecretKeyForAdminSurface(ctx, queries,
		os.Getenv(handler.DeploymentAdminEmailsEnvVar),
		strings.TrimSpace(os.Getenv("GOOSAR_MCP_SECRET_KEY")) != ""); err != nil {
		slog.Error("refusing to start", "error", err)
		os.Exit(1)
	}

	roleWorkspacesMode, err := handler.ParseRoleWorkspacesMode(os.Getenv(handler.RoleWorkspacesEnvVar))
	if err != nil {
		slog.Error("refusing to start", "error", err)
		os.Exit(1)
	}
	if roleWorkspacesMode == handler.RoleWorkspacesAuto {

		if _, err := handler.ProvisionRoleWorkspaces(ctx, pool, queries, handler.MCPSecretBoxFromEnv()); err != nil {
			slog.Error("role workspace provisioning failed — roles may be missing until the next restart or `goosar_admin provision-roles`", "error", err)
		}
	} else {
		slog.Info("role workspace provisioning disabled", "env_var", handler.RoleWorkspacesEnvVar, "value", roleWorkspacesMode)
	}
	hub.SetAuthorizer(newScopeAuthorizer(queries))

	registerSubscriberListeners(bus, queries)
	registerActivityListeners(bus, queries)
	registerNotificationListeners(bus, queries)

	health := newServerHealth(pool)

	metricsConfig := obsmetrics.ConfigFromEnv()
	var metricsServer *http.Server
	var httpMetrics *obsmetrics.HTTPMetrics
	var businessMetrics *obsmetrics.BusinessMetrics
	var samplerPool *pgxpool.Pool
	var channelMediaMetrics *obsmetrics.ChannelMediaReconcilerMetrics
	if metricsConfig.Enabled() {

		var err error
		samplerPool, err = newSamplerDBPool(ctx, dbURL)
		if err != nil {
			slog.Warn("metrics: failed to build sampler pgxpool; sampler disabled", "error", err)
			samplerPool = nil
		}

		metricsRegistry := obsmetrics.NewRegistry(obsmetrics.RegistryOptions{
			Pool:      pool,
			Realtime:  realtime.M,
			DaemonWS:  daemonws.M,
			Version:   version,
			Commit:    commit,
			Readiness: health.readinessGauges,
			BusinessSampler: func() *obsmetrics.BusinessSamplerOptions {
				if samplerPool == nil {
					return nil
				}
				return &obsmetrics.BusinessSamplerOptions{Pool: samplerPool}
			}(),
		})
		httpMetrics = metricsRegistry.HTTP
		businessMetrics = metricsRegistry.Business
		channelMediaMetrics = metricsRegistry.ChannelMedia

		if daemonHub != nil {
			daemonHub.SetMessageKindRecorder(businessMetrics)
		}
		metricsServer = obsmetrics.NewServer(metricsConfig.Addr, metricsRegistry.Gatherer)
		if !obsmetrics.IsLoopbackAddr(metricsConfig.Addr) {
			slog.Warn(
				"metrics listener is not loopback-only; restrict access with private networking, allowlists, or proxy auth",
				"addr", metricsConfig.Addr,
			)
		}
	}
	if samplerPool != nil {
		defer samplerPool.Close()
	}

	heartbeatScheduler := handler.NewBatchedHeartbeatScheduler(queries, handler.DefaultHeartbeatBatchInterval)

	r, h := NewRouterWithOptions(pool, hub, bus, analyticsClient, storeRedis, RouterOptions{
		Health:             health,
		HTTPMetrics:        httpMetrics,
		BusinessMetrics:    businessMetrics,
		DaemonHub:          daemonHub,
		DaemonWakeup:       daemonWakeup,
		FeatureFlags:       flags,
		HeartbeatScheduler: heartbeatScheduler,
	})
	if relay != nil {

		h.Disconnector = realtime.NewRelayDisconnector(hub, relay)
	}

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: r,
	}

	sweepCtx, sweepCancel := context.WithCancel(context.Background())
	autopilotCtx, autopilotCancel := context.WithCancel(context.Background())
	taskSvc := service.NewTaskService(queries, pool, hub, bus, daemonWakeup)
	taskSvc.Analytics = analyticsClient
	taskSvc.Metrics = businessMetrics
	autopilotSvc := service.NewAutopilotService(queries, pool, bus, taskSvc)
	registerAutopilotListeners(bus, autopilotSvc)

	var liveness handler.LivenessStore = handler.NewNoopLivenessStore()
	if storeRedis != nil {
		liveness = handler.NewRedisLivenessStore(storeRedis)
	}

	go runRuntimeSweeper(sweepCtx, queries, liveness, taskSvc, bus)

	go runHygieneSweeper(sweepCtx, pool, queries, h.Storage)
	go heartbeatScheduler.Run(sweepCtx)
	go runAutopilotFailureMonitor(autopilotCtx, queries, bus, envFailureMonitorConfig())
	go runDBStatsLogger(sweepCtx, pool)
	if h.WebhookDeliveryWorker != nil {
		go h.WebhookDeliveryWorker.Run(sweepCtx)
	}

	h.PRRefresh.Start(sweepCtx)

	if h.ChannelSupervisor != nil {
		go h.ChannelSupervisor.Run(sweepCtx)
	}

	if h.ChannelMediaReconciler != nil {
		h.ChannelMediaReconciler.Metrics = channelMediaMetrics
		go h.ChannelMediaReconciler.Run(sweepCtx)
	}

	schedulerMgr := scheduler.NewManager(pool, scheduler.Options{})
	if err := schedulerMgr.Register(scheduler.TaskUsageHourlyJob(pool)); err != nil {
		slog.Warn("scheduler: failed to register task_usage_hourly rollup job", "error", err)
	}

	if err := schedulerMgr.Register(scheduler.AutopilotScheduleDispatchJob(pool, queries, autopilotSvc)); err != nil {
		slog.Warn("scheduler: failed to register autopilot_schedule_dispatch job", "error", err)
	}
	go func() {
		_ = schedulerMgr.Run(sweepCtx)
	}()

	if metricsServer != nil {
		go func() {
			slog.Info("metrics server starting", "addr", metricsConfig.Addr)
			if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Error("metrics server disabled after startup error", "error", err)
			}
		}()
	}

	startedPlugins := startPlugins(sweepCtx, plugin.Registered(), plugin.Host{Logger: slog.Default(), DB: pool})

	go func() {
		slog.Info("server starting", "port", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	holdBeforeShutdown(sig, quit, shutdownHoldDuration)

	signal.Stop(quit)

	slog.Info("shutting down server")
	autopilotCancel()

	apiShutdownCtx, apiShutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := srv.Shutdown(apiShutdownCtx); err != nil {
		apiShutdownCancel()
		slog.Error("server forced to shutdown", "error", err)
		os.Exit(1)
	}
	apiShutdownCancel()

	pluginStopCtx, pluginStopCancel := context.WithTimeout(context.Background(), 10*time.Second)
	stopPlugins(pluginStopCtx, startedPlugins)
	pluginStopCancel()

	sweepCancel()
	heartbeatScheduler.Stop()
	if h.WebhookDeliveryWorker != nil && !h.WebhookDeliveryWorker.WaitWithTimeout(5*time.Second) {
		slog.Warn("webhook delivery worker did not exit within shutdown timeout")
	}

	if h.ChannelSupervisor != nil {
		if !h.ChannelSupervisor.WaitWithTimeout(h.ChannelSupervisor.ShutdownTimeout()) {
			slog.Warn("channel supervisor: connections did not exit within shutdown timeout; proceeding",
				"timeout", h.ChannelSupervisor.ShutdownTimeout().String(),
			)
		}
		if h.ChannelRouter != nil {
			drainCtx, drainCancel := context.WithTimeout(context.Background(), 10*time.Second)
			if !h.ChannelRouter.Drain(drainCtx) {
				slog.Warn("channel router: drain deadline reached; deferred media fallback remains durable")
			}
			drainCancel()
		}
	}

	if metricsServer != nil {
		metricsShutdownCtx, metricsShutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
		if err := metricsServer.Shutdown(metricsShutdownCtx); err != nil {
			slog.Error("metrics server forced to shutdown", "error", err)
		}
		metricsShutdownCancel()
	}
	slog.Info("server stopped")
}
