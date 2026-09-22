package metrics

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/adanman/goosar/server/pkg/agent/runtimeregistry"
	"github.com/adanman/goosar/server/pkg/taskfailure"
)

const (
	labelSource         = "source"
	labelRuntimeMode    = "runtime_mode"
	labelProvider       = "provider"
	labelTerminalStatus = "terminal_status"
	labelFailureReason  = "failure_reason"
	labelTokenType      = "token_type"
	labelModel          = "model"
	labelModelAlias     = "model_alias"

	labelSignupSource = "signup_source"
	labelPlatform     = "platform"
	labelPath         = "path"
	labelCadence      = "cadence"
	labelTriggerKind  = "trigger_kind"
	labelReason       = "reason"
	labelRecoverable  = "recoverable"
	labelKind         = "kind"
	labelStatus       = "status"
	labelEventKind    = "event_kind"
	labelAction       = "action"
	labelResult       = "result"
	labelOp           = "op"
	labelGate         = "gate"
)

var businessMetricLabels = map[string][]string{
	"goosar_agent_task_enqueued_total":     {labelSource, labelRuntimeMode},
	"goosar_agent_task_dispatched_total":   {labelSource, labelRuntimeMode},
	"goosar_agent_task_started_total":      {labelSource, labelRuntimeMode, labelProvider},
	"goosar_agent_task_terminal_total":     {labelSource, labelRuntimeMode, labelTerminalStatus},
	"goosar_agent_task_failed_total":       {labelSource, labelRuntimeMode, labelFailureReason},
	"goosar_agent_task_queue_wait_seconds": {labelSource, labelRuntimeMode},
	"goosar_agent_task_run_seconds":        {labelSource, labelRuntimeMode, labelTerminalStatus},
	"goosar_agent_task_total_seconds":      {labelSource, labelRuntimeMode, labelTerminalStatus},
	"goosar_agent_task_in_progress":        {labelSource, labelRuntimeMode},
	"goosar_agent_task_iteration_count":    {labelSource, labelTerminalStatus},
	"goosar_llm_tokens_total":              {labelProvider, labelModel, labelTokenType, labelRuntimeMode, labelSource},
	"goosar_llm_cost_usd_total":            {labelProvider, labelModel, labelTokenType, labelRuntimeMode, labelSource},
	"goosar_llm_unpriced_tokens_total":     {labelProvider, labelModelAlias, labelTokenType},
	"goosar_llm_request_total":             {labelProvider, labelModel, labelRuntimeMode},
	"goosar_task_queued_expired_total":     {labelSource, labelRuntimeMode},
	"goosar_task_lease_expired_total":      {labelSource},

	"goosar_signup_total":                             {labelSignupSource},
	"goosar_workspace_created_total":                  {labelSource},
	"goosar_team_invite_sent_total":                   {},
	"goosar_team_invite_accepted_total":               {},
	"goosar_onboarding_started_total":                 {labelPlatform},
	"goosar_onboarding_questionnaire_submitted_total": {},
	"goosar_onboarding_source_submitted_total":        {},
	"goosar_onboarding_completed_total":               {labelPath},
	"goosar_cloud_waitlist_joined_total":              {},
	"goosar_issue_created_total":                      {labelSource, labelPlatform},
	"goosar_chat_message_sent_total":                  {labelPlatform},
	"goosar_agent_created_total":                      {labelRuntimeMode, labelSource},
	"goosar_squad_created_total":                      {},
	"goosar_autopilot_created_total":                  {labelCadence},
	"goosar_issue_executed_total":                     {labelSource},
	"goosar_runtime_registered_total":                 {labelRuntimeMode, labelProvider},
	"goosar_runtime_ready_total":                      {labelRuntimeMode, labelProvider},
	"goosar_runtime_ready_seconds":                    {labelRuntimeMode, labelProvider},
	"goosar_runtime_failed_total":                     {labelRuntimeMode, labelProvider, labelFailureReason, labelRecoverable},
	"goosar_runtime_offline_total":                    {labelRuntimeMode, labelProvider},
	"goosar_daemon_ws_message_received_total":         {labelKind},
	"goosar_autopilot_run_started_total":              {labelCadence, labelTriggerKind},
	"goosar_autopilot_run_terminal_total":             {labelCadence, labelTriggerKind, labelTerminalStatus},
	"goosar_autopilot_run_skipped_total":              {labelCadence, labelReason},
	"goosar_webhook_delivery_total":                   {labelProvider, labelStatus},
	"goosar_webhook_rate_limited_total":               {labelGate},
	"goosar_github_event_received_total":              {labelEventKind, labelAction},
	"goosar_github_pr_review_total":                   {labelResult},
	"goosar_cloudruntime_request_total":               {labelOp, labelStatus},
	"goosar_cloudruntime_request_duration_seconds":    {labelOp},
	"goosar_feedback_submitted_total":                 {labelKind, labelPlatform},
	"goosar_contact_sales_submitted_total":            {labelSource},
	"goosar_chat_output_local_path_total":             {labelKind},
}

var forbiddenMetricLabels = map[string]struct{}{
	"workspace_id": {},
	"user_id":      {},
	"agent_id":     {},
	"task_id":      {},
	"issue_id":     {},
	"runtime_id":   {},
	"session_id":   {},
	"ip":           {},
}

var (
	knownSources = map[string]string{
		"issue":           "issue",
		"chat":            "chat",
		"autopilot":       "autopilot",
		"autopilot_issue": "autopilot_issue",
		"quick_create":    "quick_create",
		"manual":          "manual",
		"api":             "api",
		"other":           "other",
	}
	knownRuntimeModes = map[string]string{
		"local":   "local",
		"cloud":   "cloud",
		"unknown": "unknown",
	}

	knownRuntimeProviders = buildKnownRuntimeProviders()
	knownTerminalStatuses = map[string]string{
		"completed": "completed",
		"failed":    "failed",
		"cancelled": "cancelled",
		"blocked":   "blocked",
		"other":     "other",
	}
	knownTokenTypes = map[string]string{
		"input":       "input",
		"output":      "output",
		"cache_read":  "cache_read",
		"cache_write": "cache_write",
	}
	knownFailureReasons = map[string]string{}
	modelAliasUnsafeRe  = regexp.MustCompile(`[^a-z0-9._:/+-]+`)
)

func init() {
	for _, reason := range taskfailure.AllReasons() {
		knownFailureReasons[reason.String()] = reason.String()
	}
}

func buildKnownRuntimeProviders() map[string]string {
	reg, err := runtimeregistry.LoadDefault()
	if err != nil {
		panic(fmt.Sprintf("metrics: load runtime registry: %v", err))
	}
	codes := reg.Codes()
	out := make(map[string]string, len(codes)+2)
	for _, code := range codes {
		out[code] = code
	}

	out["goosar_agent"] = "goosar_agent"
	out["other"] = "other"
	return out
}

func validateBusinessMetricLabels() {
	for metric, labels := range businessMetricLabels {
		for _, label := range labels {
			if _, forbidden := forbiddenMetricLabels[label]; forbidden {
				panic("forbidden high-cardinality label " + label + " on " + metric)
			}
		}
	}
}

func metricLabels(metric string) []string {
	labels, ok := businessMetricLabels[metric]
	if !ok {
		panic("missing business metric label definition for " + metric)
	}
	return labels
}

func NormalizeTaskSource(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if normalized, ok := knownSources[value]; ok {
		return normalized
	}
	return "other"
}

func NormalizeRuntimeMode(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if normalized, ok := knownRuntimeModes[value]; ok {
		return normalized
	}
	return "unknown"
}

func NormalizeRuntimeProvider(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if normalized, ok := knownRuntimeProviders[value]; ok {
		return normalized
	}
	return "other"
}

func NormalizeTerminalStatus(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if normalized, ok := knownTerminalStatuses[value]; ok {
		return normalized
	}
	return "other"
}

func NormalizeFailureReason(value string) string {
	value = strings.TrimSpace(value)
	if normalized, ok := knownFailureReasons[value]; ok {
		return normalized
	}
	return taskfailure.Classify(value).String()
}

func NormalizeTokenType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if normalized, ok := knownTokenTypes[value]; ok {
		return normalized
	}
	return "input"
}

func NormalizeModelAlias(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "unknown"
	}
	value = modelAliasUnsafeRe.ReplaceAllString(value, "_")
	if len(value) > 128 {
		return value[:128]
	}
	return value
}
