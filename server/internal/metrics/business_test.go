package metrics

import (
	"math"
	"strconv"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/adanman/goosar/server/internal/analytics"
	"github.com/adanman/goosar/server/pkg/taskfailure"
)

func TestBusinessMetricsLifecycleCountersAndGauge(t *testing.T) {
	m := NewBusinessMetrics()

	m.RecordTaskEnqueued("issue", "local")
	for i := 0; i < 100; i++ {
		m.RecordTaskDispatched("task-"+strconv.Itoa(i), "issue", "local", 2.5)
	}
	m.RecordTaskStarted("issue", "local", "runtime-e")
	m.RecordTaskTerminal("task-0", "issue", "local", "completed", 10, 20, 1)

	if got := testutil.ToFloat64(m.taskEnqueued.WithLabelValues("issue", "local")); got != 1 {
		t.Fatalf("enqueued counter = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.taskDispatched.WithLabelValues("issue", "local")); got != 100 {
		t.Fatalf("dispatched counter = %v, want 100", got)
	}
	if got := testutil.ToFloat64(m.taskStarted.WithLabelValues("issue", "local", "runtime-e")); got != 1 {
		t.Fatalf("started counter = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.taskTerminal.WithLabelValues("issue", "local", "completed")); got != 1 {
		t.Fatalf("terminal counter = %v, want 1", got)
	}
	if got := testutil.CollectAndCount(m.taskInProgress); got != 1 {
		t.Fatalf("in_progress series count = %d, want 1 despite 100 task ids", got)
	}
	if got := testutil.ToFloat64(m.taskInProgress.WithLabelValues("issue", "local")); got != 99 {
		t.Fatalf("in_progress gauge = %v, want 99", got)
	}
	if got := testutil.CollectAndCount(m.taskQueueWait); got != 1 {
		t.Fatalf("queue wait series count = %d, want 1", got)
	}
	if got := testutil.CollectAndCount(m.taskRunSeconds); got != 1 {
		t.Fatalf("run seconds series count = %d, want 1", got)
	}
	if got := testutil.CollectAndCount(m.taskTotalSeconds); got != 1 {
		t.Fatalf("total seconds series count = %d, want 1", got)
	}
}

func TestBusinessMetricsFailureReasonUsesCanonicalClassifier(t *testing.T) {
	m := NewBusinessMetrics()

	rawError := `API Error: 429 {"error":"overloaded"}`
	m.RecordTaskFailed("issue", "local", rawError)

	wantReason := taskfailure.ReasonAgentProviderCapacityOrRateLimit.String()
	if got := testutil.ToFloat64(m.taskFailed.WithLabelValues("issue", "local", wantReason)); got != 1 {
		t.Fatalf("classified failure counter = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.taskFailed.WithLabelValues("issue", "local", taskfailure.ReasonAgentUnknown.String())); got != 0 {
		t.Fatalf("unknown failure counter = %v, want 0", got)
	}
}

func TestBusinessMetricsLLMPricingAndUnpricedTokens(t *testing.T) {
	m := NewBusinessMetrics()

	m.RecordLLMUsage("chat", "cloud", "runtime-e", "gpt-5.4", 1_000_000, 2_000_000, 3_000_000, 4_000_000, 0)

	if got := testutil.ToFloat64(m.llmTokens.WithLabelValues("openai", "gpt-5.4", "input", "cloud", "chat")); got != 1_000_000 {
		t.Fatalf("priced input tokens = %v, want 1000000", got)
	}
	if got := testutil.ToFloat64(m.llmTokens.WithLabelValues("openai", "gpt-5.4", "output", "cloud", "chat")); got != 2_000_000 {
		t.Fatalf("priced output tokens = %v, want 2000000", got)
	}
	if got := testutil.ToFloat64(m.llmCostUSD.WithLabelValues("openai", "gpt-5.4", "input", "cloud", "chat")); got != 2.5 {
		t.Fatalf("priced input cost = %v, want 2.5", got)
	}
	if got := testutil.ToFloat64(m.llmCostUSD.WithLabelValues("openai", "gpt-5.4", "output", "cloud", "chat")); got != 30 {
		t.Fatalf("priced output cost = %v, want 30", got)
	}
	if got := testutil.ToFloat64(m.llmRequests.WithLabelValues("openai", "gpt-5.4", "cloud")); got != 1 {
		t.Fatalf("priced request counter = %v, want 1", got)
	}

	m.RecordLLMUsage("issue", "local", "custom-provider", "Free Model!!", 7, 0, 0, 0, 0)
	if got := testutil.ToFloat64(m.llmUnpricedTokens.WithLabelValues("other", "free_model_", "input")); got != 7 {
		t.Fatalf("unpriced input tokens = %v, want 7", got)
	}
	if got := testutil.ToFloat64(m.llmRequests.WithLabelValues("other", "unknown", "local")); got != 1 {
		t.Fatalf("unpriced request counter = %v, want 1", got)
	}
}

func TestBusinessMetricsRegistryExposesAllFamilies(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := NewBusinessMetrics()
	registry.MustRegister(m.Collectors()...)

	m.RecordTaskEnqueued("issue", "local")
	m.RecordTaskDispatched("task-1", "issue", "local", 1)
	m.RecordTaskStarted("issue", "local", "runtime-e")
	m.RecordTaskTerminal("task-1", "issue", "local", "completed", 2, 3, 1)
	m.RecordTaskFailed("issue", "local", taskfailure.ReasonTimeout.String())
	m.RecordTaskQueuedExpired("issue", "local")
	m.RecordTaskLeaseExpired("issue")
	m.RecordLLMUsage("issue", "local", "runtime-e", "gpt-5.4", 1, 1, 1, 1, 0)
	m.RecordLLMUsage("issue", "local", "custom-provider", "custom-model", 1, 0, 0, 0, 0)

	exerciseEvent(m, analytics.EventSignup, map[string]any{"signup_source": "test"})
	exerciseEvent(m, analytics.EventWorkspaceCreated, map[string]any{"source": "manual"})
	exerciseEvent(m, analytics.EventTeamInviteSent, nil)
	exerciseEvent(m, analytics.EventTeamInviteAccepted, nil)
	exerciseEvent(m, analytics.EventOnboardingStarted, map[string]any{"platform": "web"})
	exerciseEvent(m, analytics.EventOnboardingQuestionnaireSubmit, nil)
	exerciseEvent(m, analytics.EventOnboardingSourceSubmit, nil)
	exerciseEvent(m, analytics.EventOnboardingCompleted, map[string]any{"completion_path": "full"})
	exerciseEvent(m, analytics.EventCloudWaitlistJoined, nil)
	exerciseEvent(m, analytics.EventIssueCreated, map[string]any{"source": "manual", "platform": "web"})
	exerciseEvent(m, analytics.EventChatMessageSent, map[string]any{"platform": "web"})
	exerciseEvent(m, analytics.EventAgentCreated, map[string]any{"runtime_mode": "local", "source": "manual"})
	exerciseEvent(m, analytics.EventSquadCreated, nil)
	exerciseEvent(m, analytics.EventAutopilotCreated, map[string]any{"cadence": "manual"})
	exerciseEvent(m, analytics.EventIssueExecuted, map[string]any{"source": "manual"})
	exerciseEvent(m, analytics.EventRuntimeRegistered, map[string]any{"runtime_mode": "local", "provider": "runtime-c"})
	exerciseEvent(m, analytics.EventRuntimeReady, map[string]any{"runtime_mode": "local", "provider": "runtime-c", "ready_duration_ms": int64(1000)})
	exerciseEvent(m, analytics.EventRuntimeFailed, map[string]any{"runtime_mode": "local", "provider": "runtime-c", "failure_reason": "timeout", "recoverable": true})
	exerciseEvent(m, analytics.EventRuntimeOffline, map[string]any{"runtime_mode": "local", "provider": "runtime-c"})
	exerciseEvent(m, analytics.EventAutopilotRunStarted, map[string]any{"cadence": "manual", "trigger_kind": "manual"})
	exerciseEvent(m, analytics.EventAutopilotRunCompleted, map[string]any{"cadence": "manual", "trigger_kind": "manual"})
	exerciseEvent(m, analytics.EventAutopilotRunFailed, map[string]any{"cadence": "manual", "trigger_kind": "manual"})
	exerciseEvent(m, analytics.EventFeedbackSubmitted, map[string]any{"kind": "general", "platform": "web"})
	exerciseEvent(m, analytics.EventContactSalesSubmitted, map[string]any{"form_source": "page"})

	m.RecordAutopilotRunSkipped("manual", "throttled")
	m.RecordWebhookDelivery("github", "dispatched")
	m.RecordWebhookRateLimited("absolute_ip")
	m.RecordGithubEventReceived("pull_request", "opened")
	m.RecordGithubPRReview("approved")
	m.ObserveGithubPRMergeSeconds(120)
	m.RecordCloudRuntimeRequest("provision", "ok", 0.5)
	m.RecordDaemonWSMessageReceived("heartbeat")
	m.RecordChatOutputLocalPath("file_url")

	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	seen := make(map[string]bool, len(families))
	for _, family := range families {
		seen[family.GetName()] = true
	}
	for metric := range businessMetricLabels {
		if !seen[metric] {
			t.Fatalf("registry did not expose metric family %s", metric)
		}
	}
}

func exerciseEvent(m *BusinessMetrics, name string, props map[string]any) {
	if props == nil {
		props = map[string]any{}
	}
	m.IncForEvent(analytics.Event{Name: name, Properties: props})
}

func TestBusinessMetricsPrefersProviderReportedCost(t *testing.T) {
	m := NewBusinessMetrics()

	const actualUSD = 16.0
	m.RecordLLMUsage("issue", "local", "runtime-i", "grok-4.5",
		1_000_000, 1_000_000, 0, 0, int64(actualUSD*CostUSDTicksPerUSD))

	input := testutil.ToFloat64(m.llmCostUSD.WithLabelValues("xai", "grok-4.5", "input", "local", "issue"))
	output := testutil.ToFloat64(m.llmCostUSD.WithLabelValues("xai", "grok-4.5", "output", "local", "issue"))
	if total := input + output; math.Abs(total-actualUSD) > 1e-9 {
		t.Fatalf("recorded cost = %v, want %v (the provider's own charge)", total, actualUSD)
	}

	if math.Abs(input-4) > 1e-9 {
		t.Errorf("input cost = %v, want 4 (a quarter of the charge)", input)
	}
	if math.Abs(output-12) > 1e-9 {
		t.Errorf("output cost = %v, want 12 (three quarters of the charge)", output)
	}

	if got := testutil.ToFloat64(m.llmTokens.WithLabelValues("xai", "grok-4.5", "input", "local", "issue")); got != 1_000_000 {
		t.Errorf("input tokens = %v, want 1000000", got)
	}
}

func TestBusinessMetricsFallsBackToRateTableWithoutProviderCost(t *testing.T) {
	m := NewBusinessMetrics()

	m.RecordLLMUsage("issue", "local", "runtime-i", "grok-4.5", 1_000_000, 1_000_000, 0, 0, 0)

	input := testutil.ToFloat64(m.llmCostUSD.WithLabelValues("xai", "grok-4.5", "input", "local", "issue"))
	output := testutil.ToFloat64(m.llmCostUSD.WithLabelValues("xai", "grok-4.5", "output", "local", "issue"))
	if math.Abs(input-2) > 1e-9 || math.Abs(output-6) > 1e-9 {
		t.Fatalf("estimated cost = (%v, %v), want (2, 6) from the rate table", input, output)
	}
}

func TestDistributeAuthoritativeCost(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		actual    float64
		estimated [4]float64
		want      [4]float64
	}{
		{
			name:      "scales the estimate to the real charge",
			actual:    16,
			estimated: [4]float64{2, 6, 0, 0},
			want:      [4]float64{4, 12, 0, 0},
		},
		{
			name:      "scales down when the estimate was too high",
			actual:    4,
			estimated: [4]float64{2, 6, 0, 0},
			want:      [4]float64{1, 3, 0, 0},
		},
		{

			name:      "keeps the charge when there is nothing to scale",
			actual:    7,
			estimated: [4]float64{0, 0, 0, 0},
			want:      [4]float64{7, 0, 0, 0},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := distributeAuthoritativeCost(tc.actual, tc.estimated)
			var sum float64
			for i := range got {
				if math.Abs(got[i]-tc.want[i]) > 1e-9 {
					t.Errorf("bucket %d = %v, want %v", i, got[i], tc.want[i])
				}
				sum += got[i]
			}
			if math.Abs(sum-tc.actual) > 1e-9 {
				t.Errorf("buckets sum to %v, want the actual charge %v", sum, tc.actual)
			}
		})
	}
}

func TestBusinessMetricsRecordsProviderCostForUnpricedModel(t *testing.T) {
	m := NewBusinessMetrics()

	const actualUSD = 1.23456789
	m.RecordLLMUsage("issue", "local", "runtime-i", "grok-composer-2.5-fast",
		500, 100, 0, 0, int64(actualUSD*CostUSDTicksPerUSD))

	got := testutil.ToFloat64(m.llmCostUSD.WithLabelValues(
		"runtime-i", "grok-composer-2.5-fast", "input", "local", "issue"))
	if math.Abs(got-actualUSD) > 1e-9 {
		t.Fatalf("recorded cost = %v, want %v", got, actualUSD)
	}

	if got := testutil.ToFloat64(m.llmUnpricedTokens.WithLabelValues("runtime-i", "grok-composer-2.5-fast", "input")); got != 500 {
		t.Errorf("unpriced input tokens = %v, want 500", got)
	}
}

func TestBusinessMetricsUnpricedModelWithoutCostStaysAtZero(t *testing.T) {
	m := NewBusinessMetrics()

	m.RecordLLMUsage("issue", "local", "runtime-i", "grok-composer-2.5-fast", 500, 100, 0, 0, 0)

	if got := testutil.ToFloat64(m.llmCostUSD.WithLabelValues(
		"runtime-i", "grok-composer-2.5-fast", "input", "local", "issue")); got != 0 {
		t.Fatalf("recorded cost = %v, want 0", got)
	}
}
