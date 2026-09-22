package metrics_test

import (
	"strings"
	"testing"

	"github.com/adanman/goosar/server/internal/analytics"
	"github.com/adanman/goosar/server/internal/metrics"
)

func TestNormalizePR3LabelsCollapseUnknownValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		fn       func(string) string
		input    string
		want     string
		fallback string
	}{
		{"platform_unknown", metrics.NormalizePlatform, "iphone-internal-build-9", "unknown", "unknown"},
		{"platform_known_web", metrics.NormalizePlatform, "web", "web", "unknown"},
		{"signup_source_unknown", metrics.NormalizeSignupSource, "rakuten-affiliate-program", "other", "other"},
		{"signup_source_empty", metrics.NormalizeSignupSource, "", "direct", "direct"},
		{"signup_source_json_utm", metrics.NormalizeSignupSource, `{"utm_source":"twitter","utm_medium":"social"}`, "twitter", "other"},
		{"signup_source_url_host", metrics.NormalizeSignupSource, "https://news.ycombinator.com/item?id=42", "hacker_news", "other"},
		{"onboarding_path_unknown", metrics.NormalizeOnboardingPath, "ab-experiment-123", "unknown", "unknown"},
		{"autopilot_cadence_unknown", metrics.NormalizeAutopilotCadence, "every_5_min", "unknown", "unknown"},
		{"autopilot_trigger_unknown", metrics.NormalizeAutopilotTrigger, "future_kind", "unknown", "unknown"},
		{"autopilot_skip_reason_unknown", metrics.NormalizeAutopilotSkipReason, "lunar_phase", "other", "other"},
		{"webhook_provider_unknown", metrics.NormalizeWebhookProvider, "internal-billing", "other", "other"},
		{"webhook_status_unknown", metrics.NormalizeWebhookDeliveryStatus, "exotic", "other", "other"},
		{"webhook_gate_unknown", metrics.NormalizeWebhookRateLimitGate, "exotic", "other", "other"},
		{"github_event_unknown", metrics.NormalizeGithubEventKind, "deploy_status", "other", "other"},
		{"github_action_empty", metrics.NormalizeGithubAction, "", "none", "none"},
		{"github_action_unknown", metrics.NormalizeGithubAction, "rerequested_by_user", "other", "other"},
		{"github_pr_review_unknown", metrics.NormalizeGithubPRReviewResult, "skipped", "other", "other"},
		{"cloudruntime_op_unknown", metrics.NormalizeCloudRuntimeOp, "lifecycle_audit", "other", "other"},
		{"cloudruntime_status_2xx_string", metrics.NormalizeCloudRuntimeStatus, "200", "ok", "error"},
		{"cloudruntime_status_5xx_string", metrics.NormalizeCloudRuntimeStatus, "503", "5xx", "error"},
		{"cloudruntime_status_garbage", metrics.NormalizeCloudRuntimeStatus, "lol", "error", "error"},
		{"daemon_ws_kind_unknown", metrics.NormalizeDaemonWSKind, "future_event", "other", "other"},
		{"feedback_kind_unknown", metrics.NormalizeFeedbackKind, "rant", "other", "other"},
		{"contact_sales_source_unknown", metrics.NormalizeContactSalesSource, "homepage_modal", "other", "other"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.fn(tt.input)
			if got != tt.want {
				t.Fatalf("normalize(%q) = %q, want %q (fallback bucket %q)", tt.input, got, tt.want, tt.fallback)
			}
		})
	}
}

func TestOnboardingStartedUnknownPlatformCollapses(t *testing.T) {
	t.Parallel()

	m := metrics.NewBusinessMetrics()

	metrics.RecordEvent(analytics.NoopClient{}, m, analytics.OnboardingStarted("user-1", "web"))

	metrics.RecordEvent(analytics.NoopClient{}, m, analytics.OnboardingStarted("user-2", "iphone-build-1234567890-abcdef"))

	families := metrics.GatherForTest(t, m)
	famName := "goosar_onboarding_started_total"
	fam, ok := families[famName]
	if !ok {
		t.Fatalf("metric family %s not present in registry output", famName)
	}
	seen := map[string]float64{}
	for _, child := range fam.GetMetric() {
		for _, lbl := range child.GetLabel() {
			if lbl.GetName() == "platform" {
				seen[lbl.GetValue()] += child.GetCounter().GetValue()
			}
		}
	}
	if seen["web"] != 1 {
		t.Errorf("expected platform=web count 1, got %v", seen["web"])
	}
	if seen["unknown"] != 1 {
		t.Errorf("expected platform=unknown count 1 (collapsed from raw header), got %v", seen["unknown"])
	}
	for v, count := range seen {
		if v == "web" || v == "unknown" {
			continue
		}
		t.Errorf("found unexpected platform label value %q (count=%v) — NormalizePlatform should have collapsed it", v, count)
	}

	for v := range seen {
		if len(v) > len("desktop") && !strings.EqualFold(v, "unknown") {
			t.Errorf("platform label %q is suspiciously long — likely raw header bleed", v)
		}
	}
}
