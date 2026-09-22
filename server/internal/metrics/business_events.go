package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/adanman/goosar/server/internal/analytics"
)

var runtimeReadyBuckets = []float64{1, 2.5, 5, 10, 30, 60, 120, 300, 600}

var cloudRuntimeRequestBuckets = []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 20, 30}

var prMergeSecondsBuckets = []float64{
	300, 900, 1800,
	3600, 2 * 3600, 6 * 3600, 12 * 3600,
	24 * 3600, 2 * 24 * 3600, 7 * 24 * 3600, 30 * 24 * 3600,
}

type businessEventMetrics struct {
	signup                          *prometheus.CounterVec
	workspaceCreated                *prometheus.CounterVec
	teamInviteSent                  *prometheus.CounterVec
	teamInviteAccepted              *prometheus.CounterVec
	onboardingStarted               *prometheus.CounterVec
	onboardingQuestionnaireSubmit   *prometheus.CounterVec
	onboardingSourceSubmit          *prometheus.CounterVec
	onboardingCompleted             *prometheus.CounterVec
	cloudWaitlistJoined             *prometheus.CounterVec
	issueCreated                    *prometheus.CounterVec
	chatMessageSent                 *prometheus.CounterVec
	agentCreated                    *prometheus.CounterVec
	squadCreated                    *prometheus.CounterVec
	autopilotCreated                *prometheus.CounterVec
	issueExecuted                   *prometheus.CounterVec
	runtimeRegistered               *prometheus.CounterVec
	runtimeReady                    *prometheus.CounterVec
	runtimeReadySeconds             *prometheus.HistogramVec
	runtimeFailed                   *prometheus.CounterVec
	runtimeOffline                  *prometheus.CounterVec
	daemonWSMessageReceived         *prometheus.CounterVec
	autopilotRunStarted             *prometheus.CounterVec
	autopilotRunTerminal            *prometheus.CounterVec
	autopilotRunSkipped             *prometheus.CounterVec
	webhookDelivery                 *prometheus.CounterVec
	webhookRateLimited              *prometheus.CounterVec
	githubEventReceived             *prometheus.CounterVec
	githubPRReview                  *prometheus.CounterVec
	githubPRMergeSeconds            prometheus.Histogram
	cloudRuntimeRequest             *prometheus.CounterVec
	cloudRuntimeRequestDurationSecs *prometheus.HistogramVec
	feedbackSubmitted               *prometheus.CounterVec
	contactSalesSubmitted           *prometheus.CounterVec
	chatOutputLocalPath             *prometheus.CounterVec
}

func newBusinessEventMetrics() *businessEventMetrics {
	return &businessEventMetrics{
		signup: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "goosar_signup_total",
			Help: "Total user signups (account creations).",
		}, metricLabels("goosar_signup_total")),
		workspaceCreated: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "goosar_workspace_created_total",
			Help: "Total workspaces created.",
		}, metricLabels("goosar_workspace_created_total")),
		teamInviteSent: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "goosar_team_invite_sent_total",
			Help: "Total workspace invitations sent.",
		}, metricLabels("goosar_team_invite_sent_total")),
		teamInviteAccepted: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "goosar_team_invite_accepted_total",
			Help: "Total workspace invitations accepted.",
		}, metricLabels("goosar_team_invite_accepted_total")),
		onboardingStarted: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "goosar_onboarding_started_total",
			Help: "Total onboarding flows started.",
		}, metricLabels("goosar_onboarding_started_total")),
		onboardingQuestionnaireSubmit: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "goosar_onboarding_questionnaire_submitted_total",
			Help: "Total onboarding questionnaires submitted.",
		}, metricLabels("goosar_onboarding_questionnaire_submitted_total")),
		onboardingSourceSubmit: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "goosar_onboarding_source_submitted_total",
			Help: "Total acquisition-source answers or declines recorded (workspace backfill prompt).",
		}, metricLabels("goosar_onboarding_source_submitted_total")),
		onboardingCompleted: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "goosar_onboarding_completed_total",
			Help: "Total onboarding flows completed.",
		}, metricLabels("goosar_onboarding_completed_total")),
		cloudWaitlistJoined: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "goosar_cloud_waitlist_joined_total",
			Help: "Total users that joined the cloud waitlist.",
		}, metricLabels("goosar_cloud_waitlist_joined_total")),
		issueCreated: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "goosar_issue_created_total",
			Help: "Total issues created (any source).",
		}, metricLabels("goosar_issue_created_total")),
		chatMessageSent: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "goosar_chat_message_sent_total",
			Help: "Total user chat messages sent (excludes agent replies).",
		}, metricLabels("goosar_chat_message_sent_total")),
		agentCreated: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "goosar_agent_created_total",
			Help: "Total agents created.",
		}, metricLabels("goosar_agent_created_total")),
		squadCreated: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "goosar_squad_created_total",
			Help: "Total squads created.",
		}, metricLabels("goosar_squad_created_total")),
		autopilotCreated: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "goosar_autopilot_created_total",
			Help: "Total autopilots created.",
		}, metricLabels("goosar_autopilot_created_total")),
		issueExecuted: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "goosar_issue_executed_total",
			Help: "First task completion per issue (per-issue exactly-once activation keystone).",
		}, metricLabels("goosar_issue_executed_total")),
		runtimeRegistered: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "goosar_runtime_registered_total",
			Help: "Total first-time runtime registrations.",
		}, metricLabels("goosar_runtime_registered_total")),
		runtimeReady: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "goosar_runtime_ready_total",
			Help: "Total runtimes that reached ready state.",
		}, metricLabels("goosar_runtime_ready_total")),
		runtimeReadySeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "goosar_runtime_ready_seconds",
			Help:    "Time from runtime registration to ready (seconds).",
			Buckets: runtimeReadyBuckets,
		}, metricLabels("goosar_runtime_ready_seconds")),
		runtimeFailed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "goosar_runtime_failed_total",
			Help: "Total runtime failures by canonical reason.",
		}, metricLabels("goosar_runtime_failed_total")),
		runtimeOffline: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "goosar_runtime_offline_total",
			Help: "Total runtime offline transitions.",
		}, metricLabels("goosar_runtime_offline_total")),
		daemonWSMessageReceived: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "goosar_daemon_ws_message_received_total",
			Help: "Total daemon WebSocket inbound messages by handler kind.",
		}, metricLabels("goosar_daemon_ws_message_received_total")),
		autopilotRunStarted: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "goosar_autopilot_run_started_total",
			Help: "Total autopilot runs started.",
		}, metricLabels("goosar_autopilot_run_started_total")),
		autopilotRunTerminal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "goosar_autopilot_run_terminal_total",
			Help: "Total autopilot runs that reached a terminal status.",
		}, metricLabels("goosar_autopilot_run_terminal_total")),
		autopilotRunSkipped: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "goosar_autopilot_run_skipped_total",
			Help: "Total autopilot runs that admission-skipped (concurrency / cooldown / other).",
		}, metricLabels("goosar_autopilot_run_skipped_total")),
		webhookDelivery: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "goosar_webhook_delivery_total",
			Help: "Total inbound webhook deliveries by provider and outcome.",
		}, metricLabels("goosar_webhook_delivery_total")),
		webhookRateLimited: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "goosar_webhook_rate_limited_total",
			Help: "Total webhook admissions or worker dispatches delayed by a bounded safety gate.",
		}, metricLabels("goosar_webhook_rate_limited_total")),
		githubEventReceived: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "goosar_github_event_received_total",
			Help: "Total GitHub webhook events received by event kind and action.",
		}, metricLabels("goosar_github_event_received_total")),
		githubPRReview: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "goosar_github_pr_review_total",
			Help: "Total GitHub pull request reviews observed by result.",
		}, metricLabels("goosar_github_pr_review_total")),
		githubPRMergeSeconds: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "goosar_github_pr_merge_seconds",
			Help:    "Time from PR opened to merged (seconds).",
			Buckets: prMergeSecondsBuckets,
		}),
		cloudRuntimeRequest: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "goosar_cloudruntime_request_total",
			Help: "Total outbound cloud runtime requests by op and status bucket.",
		}, metricLabels("goosar_cloudruntime_request_total")),
		cloudRuntimeRequestDurationSecs: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "goosar_cloudruntime_request_duration_seconds",
			Help:    "Outbound cloud runtime request duration (seconds).",
			Buckets: cloudRuntimeRequestBuckets,
		}, metricLabels("goosar_cloudruntime_request_duration_seconds")),
		feedbackSubmitted: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "goosar_feedback_submitted_total",
			Help: "Total in-app feedback submissions.",
		}, metricLabels("goosar_feedback_submitted_total")),
		contactSalesSubmitted: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "goosar_contact_sales_submitted_total",
			Help: "Total contact-sales inquiries submitted.",
		}, metricLabels("goosar_contact_sales_submitted_total")),
		chatOutputLocalPath: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "goosar_chat_output_local_path_total",
			Help: "Total agent chat replies that referenced a runtime-local path, by evidence kind. Observation only — the reply is still delivered.",
		}, metricLabels("goosar_chat_output_local_path_total")),
	}
}

func (e *businessEventMetrics) collectors() []prometheus.Collector {
	if e == nil {
		return nil
	}
	return []prometheus.Collector{
		e.signup,
		e.workspaceCreated,
		e.teamInviteSent,
		e.teamInviteAccepted,
		e.onboardingStarted,
		e.onboardingQuestionnaireSubmit,
		e.onboardingSourceSubmit,
		e.onboardingCompleted,
		e.cloudWaitlistJoined,
		e.issueCreated,
		e.chatMessageSent,
		e.agentCreated,
		e.squadCreated,
		e.autopilotCreated,
		e.issueExecuted,
		e.runtimeRegistered,
		e.runtimeReady,
		e.runtimeReadySeconds,
		e.runtimeFailed,
		e.runtimeOffline,
		e.daemonWSMessageReceived,
		e.autopilotRunStarted,
		e.autopilotRunTerminal,
		e.autopilotRunSkipped,
		e.webhookDelivery,
		e.webhookRateLimited,
		e.githubEventReceived,
		e.githubPRReview,
		e.githubPRMergeSeconds,
		e.cloudRuntimeRequest,
		e.cloudRuntimeRequestDurationSecs,
		e.feedbackSubmitted,
		e.contactSalesSubmitted,
		e.chatOutputLocalPath,
	}
}

func RecordEvent(client analytics.Client, m *BusinessMetrics, ev analytics.Event) {
	if client != nil && !analytics.IsMetricsOnly(ev.Name) {
		client.Capture(ev)
	}
	if m != nil {
		m.IncForEvent(ev)
	}
}

func (m *BusinessMetrics) IncForEvent(ev analytics.Event) {
	if m == nil || m.events == nil {
		return
	}
	switch ev.Name {
	case analytics.EventSignup:
		m.events.signup.WithLabelValues(NormalizeSignupSource(stringProp(ev.Properties, "signup_source"))).Inc()
	case analytics.EventWorkspaceCreated:
		m.events.workspaceCreated.WithLabelValues(NormalizeTaskSource(stringProp(ev.Properties, "source"))).Inc()
	case analytics.EventTeamInviteSent:
		m.events.teamInviteSent.WithLabelValues().Inc()
	case analytics.EventTeamInviteAccepted:
		m.events.teamInviteAccepted.WithLabelValues().Inc()
	case analytics.EventOnboardingStarted:
		m.events.onboardingStarted.WithLabelValues(NormalizePlatform(stringProp(ev.Properties, "platform"))).Inc()
	case analytics.EventOnboardingQuestionnaireSubmit:
		m.events.onboardingQuestionnaireSubmit.WithLabelValues().Inc()
	case analytics.EventOnboardingSourceSubmit:
		m.events.onboardingSourceSubmit.WithLabelValues().Inc()
	case analytics.EventOnboardingCompleted:
		m.events.onboardingCompleted.WithLabelValues(NormalizeOnboardingPath(stringProp(ev.Properties, "completion_path"))).Inc()
	case analytics.EventCloudWaitlistJoined:
		m.events.cloudWaitlistJoined.WithLabelValues().Inc()
	case analytics.EventIssueCreated:
		m.events.issueCreated.WithLabelValues(
			NormalizeTaskSource(stringProp(ev.Properties, "source")),
			NormalizePlatform(stringProp(ev.Properties, "platform")),
		).Inc()
	case analytics.EventChatMessageSent:
		m.events.chatMessageSent.WithLabelValues(NormalizePlatform(stringProp(ev.Properties, "platform"))).Inc()
	case analytics.EventAgentCreated:
		m.events.agentCreated.WithLabelValues(
			NormalizeRuntimeMode(stringProp(ev.Properties, "runtime_mode")),
			NormalizeTaskSource(stringProp(ev.Properties, "source")),
		).Inc()
	case analytics.EventSquadCreated:
		m.events.squadCreated.WithLabelValues().Inc()
	case analytics.EventAutopilotCreated:
		m.events.autopilotCreated.WithLabelValues(NormalizeAutopilotCadence(stringProp(ev.Properties, "cadence"))).Inc()
	case analytics.EventIssueExecuted:
		m.events.issueExecuted.WithLabelValues(NormalizeTaskSource(stringProp(ev.Properties, "source"))).Inc()
	case analytics.EventRuntimeRegistered:
		m.events.runtimeRegistered.WithLabelValues(
			NormalizeRuntimeMode(stringProp(ev.Properties, "runtime_mode")),
			NormalizeRuntimeProvider(stringProp(ev.Properties, "provider")),
		).Inc()
	case analytics.EventRuntimeReady:
		runtimeMode := NormalizeRuntimeMode(stringProp(ev.Properties, "runtime_mode"))
		provider := NormalizeRuntimeProvider(stringProp(ev.Properties, "provider"))
		m.events.runtimeReady.WithLabelValues(runtimeMode, provider).Inc()
		if d := int64Prop(ev.Properties, "ready_duration_ms"); d > 0 {
			m.events.runtimeReadySeconds.WithLabelValues(runtimeMode, provider).Observe(float64(d) / 1000.0)
		}
	case analytics.EventRuntimeFailed:
		m.events.runtimeFailed.WithLabelValues(
			NormalizeRuntimeMode(stringProp(ev.Properties, "runtime_mode")),
			NormalizeRuntimeProvider(stringProp(ev.Properties, "provider")),
			NormalizeFailureReason(stringProp(ev.Properties, "failure_reason")),
			boolLabel(boolProp(ev.Properties, "recoverable")),
		).Inc()
	case analytics.EventRuntimeOffline:
		m.events.runtimeOffline.WithLabelValues(
			NormalizeRuntimeMode(stringProp(ev.Properties, "runtime_mode")),
			NormalizeRuntimeProvider(stringProp(ev.Properties, "provider")),
		).Inc()
	case analytics.EventAutopilotRunStarted:
		m.events.autopilotRunStarted.WithLabelValues(
			NormalizeAutopilotCadence(stringProp(ev.Properties, "cadence")),
			NormalizeAutopilotTrigger(stringProp(ev.Properties, "trigger_kind")),
		).Inc()
	case analytics.EventAutopilotRunCompleted:
		m.events.autopilotRunTerminal.WithLabelValues(
			NormalizeAutopilotCadence(stringProp(ev.Properties, "cadence")),
			NormalizeAutopilotTrigger(stringProp(ev.Properties, "trigger_kind")),
			"completed",
		).Inc()
	case analytics.EventAutopilotRunFailed:
		m.events.autopilotRunTerminal.WithLabelValues(
			NormalizeAutopilotCadence(stringProp(ev.Properties, "cadence")),
			NormalizeAutopilotTrigger(stringProp(ev.Properties, "trigger_kind")),
			"failed",
		).Inc()
	case analytics.EventFeedbackSubmitted:
		m.events.feedbackSubmitted.WithLabelValues(
			NormalizeFeedbackKind(stringProp(ev.Properties, "kind")),
			NormalizePlatform(stringProp(ev.Properties, "platform")),
		).Inc()
	case analytics.EventContactSalesSubmitted:
		m.events.contactSalesSubmitted.WithLabelValues(NormalizeContactSalesSource(stringProp(ev.Properties, "form_source"))).Inc()
	default:

	}
}

func (m *BusinessMetrics) RecordAutopilotRunSkipped(cadence, reason string) {
	if m == nil || m.events == nil {
		return
	}
	m.events.autopilotRunSkipped.WithLabelValues(
		NormalizeAutopilotCadence(cadence),
		NormalizeAutopilotSkipReason(reason),
	).Inc()
}

func (m *BusinessMetrics) RecordWebhookDelivery(provider, status string) {
	if m == nil || m.events == nil {
		return
	}
	m.events.webhookDelivery.WithLabelValues(
		NormalizeWebhookProvider(provider),
		NormalizeWebhookDeliveryStatus(status),
	).Inc()
}

func (m *BusinessMetrics) RecordWebhookRateLimited(gate string) {
	if m == nil || m.events == nil {
		return
	}
	m.events.webhookRateLimited.WithLabelValues(NormalizeWebhookRateLimitGate(gate)).Inc()
}

func (m *BusinessMetrics) RecordGithubEventReceived(eventKind, action string) {
	if m == nil || m.events == nil {
		return
	}
	m.events.githubEventReceived.WithLabelValues(
		NormalizeGithubEventKind(eventKind),
		NormalizeGithubAction(action),
	).Inc()
}

func (m *BusinessMetrics) RecordGithubPRReview(result string) {
	if m == nil || m.events == nil {
		return
	}
	m.events.githubPRReview.WithLabelValues(NormalizeGithubPRReviewResult(result)).Inc()
}

func (m *BusinessMetrics) ObserveGithubPRMergeSeconds(seconds float64) {
	if m == nil || m.events == nil || seconds <= 0 {
		return
	}
	m.events.githubPRMergeSeconds.Observe(seconds)
}

func (m *BusinessMetrics) RecordCloudRuntimeRequest(op, status string, durationSeconds float64) {
	if m == nil || m.events == nil {
		return
	}
	op = NormalizeCloudRuntimeOp(op)
	status = NormalizeCloudRuntimeStatus(status)
	m.events.cloudRuntimeRequest.WithLabelValues(op, status).Inc()
	if durationSeconds >= 0 {
		m.events.cloudRuntimeRequestDurationSecs.WithLabelValues(op).Observe(durationSeconds)
	}
}

func (m *BusinessMetrics) RecordChatOutputLocalPath(kind string) {
	if m == nil || m.events == nil {
		return
	}
	m.events.chatOutputLocalPath.WithLabelValues(NormalizeChatOutputLocalPathKind(kind)).Inc()
}

func (m *BusinessMetrics) RecordDaemonWSMessageReceived(kind string) {
	if m == nil || m.events == nil {
		return
	}
	m.events.daemonWSMessageReceived.WithLabelValues(NormalizeDaemonWSKind(kind)).Inc()
}

func stringProp(props map[string]any, key string) string {
	if props == nil {
		return ""
	}
	v, ok := props[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func int64Prop(props map[string]any, key string) int64 {
	if props == nil {
		return 0
	}
	v, ok := props[key]
	if !ok || v == nil {
		return 0
	}
	switch x := v.(type) {
	case int64:
		return x
	case int32:
		return int64(x)
	case int:
		return int64(x)
	case float64:
		return int64(x)
	}
	return 0
}

func boolProp(props map[string]any, key string) bool {
	if props == nil {
		return false
	}
	v, ok := props[key]
	if !ok || v == nil {
		return false
	}
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}

func boolLabel(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
