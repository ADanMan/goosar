package analytics

import "testing"

func TestSignupNeverCarriesFullEmail(t *testing.T) {

	ev := Signup("user-1", "alice@example.com", "utm-bundle")

	for bagName, bag := range map[string]map[string]any{
		"Properties": ev.Properties,
		"SetOnce":    ev.SetOnce,
		"Set":        ev.Set,
	} {
		if _, ok := bag["email"]; ok {
			t.Fatalf("%s must not contain the full email", bagName)
		}
		for key, value := range bag {
			if s, ok := value.(string); ok && s == "alice@example.com" {
				t.Fatalf("%s[%q] carries the raw email address", bagName, key)
			}
		}
	}

	if got := ev.Properties["email_domain"]; got != "example.com" {
		t.Fatalf("email_domain = %v, want example.com", got)
	}
	if got := ev.SetOnce["signup_source"]; got != "utm-bundle" {
		t.Fatalf("SetOnce signup_source = %v, want utm-bundle", got)
	}
}

func TestRuntimeReadyOmitsUnmeasuredDuration(t *testing.T) {
	ev := RuntimeReady("user-1", "workspace-1", "runtime-1", "daemon-1", "codex", 0)
	if _, ok := ev.Properties["ready_duration_ms"]; ok {
		t.Fatalf("ready_duration_ms should be omitted until it is measured")
	}

	ev = RuntimeReady("user-1", "workspace-1", "runtime-1", "daemon-1", "codex", 123)
	if got := ev.Properties["ready_duration_ms"]; got != int64(123) {
		t.Fatalf("ready_duration_ms = %v, want 123", got)
	}
}

func TestFailedEventsUseWillRetry(t *testing.T) {
	runEv := AutopilotRunFailed("user-1", "workspace-1", "autopilot-1", "run-1", "manual", AutopilotAssignee{AgentID: "agent-1", AssigneeType: "agent"}, "manual", "task failed", "task_error", false, 10)
	if got := runEv.Properties["will_retry"]; got != false {
		t.Fatalf("autopilot will_retry = %v, want false", got)
	}
	if _, ok := runEv.Properties["recoverable"]; ok {
		t.Fatalf("autopilot failure should not emit recoverable")
	}
}

func TestIsMetricsOnly(t *testing.T) {

	for _, name := range []string{

		EventRuntimeRegistered, EventRuntimeReady, EventRuntimeFailed, EventRuntimeOffline,
		EventAutopilotRunStarted, EventAutopilotRunCompleted, EventAutopilotRunFailed,

		EventSignup, EventWorkspaceCreated, EventIssueCreated, EventIssueExecuted,
		EventChatMessageSent, EventTeamInviteSent, EventTeamInviteAccepted,
		EventOnboardingStarted, EventOnboardingQuestionnaireSubmit, EventOnboardingSourceSubmit,
		EventAgentCreated,
		EventOnboardingCompleted, EventCloudWaitlistJoined, EventFeedbackSubmitted,
		EventContactSalesSubmitted, EventSquadCreated, EventAutopilotCreated,
	} {
		if !IsMetricsOnly(name) {
			t.Errorf("IsMetricsOnly(%q) = false, want true (server events stay out of PostHog since MUL-4127)", name)
		}
	}

	if IsMetricsOnly("$exception") {
		t.Errorf("IsMetricsOnly(%q) = true, want false (frontend-only event)", "$exception")
	}
}

func TestOnboardingSourceSubmittedSetOnlyWhenAnswered(t *testing.T) {
	answered := OnboardingSourceSubmitted("u1", []string{"search"}, false, false)
	if answered.Properties["source_skipped"] != false {
		t.Fatalf("answered: source_skipped = %v, want false", answered.Properties["source_skipped"])
	}
	if answered.Set == nil || answered.Set["source"] == nil {
		t.Fatalf("answered: expected $set source, got %v", answered.Set)
	}

	declined := OnboardingSourceSubmitted("u1", nil, true, false)
	if declined.Properties["source_skipped"] != true {
		t.Fatalf("declined: source_skipped = %v, want true", declined.Properties["source_skipped"])
	}
	if declined.Set != nil {
		t.Fatalf("declined: a skip has nothing to mirror — expected nil Set, got %v", declined.Set)
	}

	if src, ok := declined.Properties["acquisition_source"].([]string); !ok || src == nil {
		t.Fatalf("declined: acquisition_source property = %#v, want empty []string", declined.Properties["acquisition_source"])
	}
}
