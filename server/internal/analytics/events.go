package analytics

import "strings"

const (
	EventSignup                        = "signup"
	EventWorkspaceCreated              = "workspace_created"
	EventRuntimeRegistered             = "runtime_registered"
	EventRuntimeReady                  = "runtime_ready"
	EventRuntimeFailed                 = "runtime_failed"
	EventRuntimeOffline                = "runtime_offline"
	EventIssueExecuted                 = "issue_executed"
	EventIssueCreated                  = "issue_created"
	EventChatMessageSent               = "chat_message_sent"
	EventAutopilotRunStarted           = "autopilot_run_started"
	EventAutopilotRunCompleted         = "autopilot_run_completed"
	EventAutopilotRunFailed            = "autopilot_run_failed"
	EventTeamInviteSent                = "team_invite_sent"
	EventTeamInviteAccepted            = "team_invite_accepted"
	EventOnboardingStarted             = "onboarding_started"
	EventOnboardingQuestionnaireSubmit = "onboarding_questionnaire_submitted"
	EventOnboardingSourceSubmit        = "onboarding_source_submitted"
	EventAgentCreated                  = "agent_created"
	EventOnboardingCompleted           = "onboarding_completed"
	EventCloudWaitlistJoined           = "cloud_waitlist_joined"
	EventFeedbackSubmitted             = "feedback_submitted"
	EventContactSalesSubmitted         = "contact_sales_submitted"
	EventSquadCreated                  = "squad_created"
	EventAutopilotCreated              = "autopilot_created"
)

const EventSchemaVersion = 2

var metricsOnlyEvents = map[string]struct{}{

	EventSignup:                        {},
	EventWorkspaceCreated:              {},
	EventIssueCreated:                  {},
	EventIssueExecuted:                 {},
	EventChatMessageSent:               {},
	EventTeamInviteSent:                {},
	EventTeamInviteAccepted:            {},
	EventOnboardingStarted:             {},
	EventOnboardingQuestionnaireSubmit: {},
	EventOnboardingSourceSubmit:        {},
	EventAgentCreated:                  {},
	EventOnboardingCompleted:           {},
	EventCloudWaitlistJoined:           {},
	EventFeedbackSubmitted:             {},
	EventContactSalesSubmitted:         {},
	EventSquadCreated:                  {},
	EventAutopilotCreated:              {},

	EventRuntimeRegistered:     {},
	EventRuntimeReady:          {},
	EventRuntimeFailed:         {},
	EventRuntimeOffline:        {},
	EventAutopilotRunStarted:   {},
	EventAutopilotRunCompleted: {},
	EventAutopilotRunFailed:    {},
}

func IsMetricsOnly(name string) bool {
	_, ok := metricsOnlyEvents[name]
	return ok
}

const (
	SourceOnboarding = "onboarding"
	SourceManual     = "manual"
	SourceChat       = "chat"
	SourceAutopilot  = "autopilot"
	SourceAPI        = "api"
)

type CoreProperties struct {
	UserID         string
	WorkspaceID    string
	AgentID        string
	TaskID         string
	IssueID        string
	ChatSessionID  string
	AutopilotRunID string
	Source         string
	RuntimeMode    string
	Provider       string
	IsDemo         bool
}

type TaskContext = CoreProperties

const (
	OnboardingPathFull           = "full"
	OnboardingPathRuntimeSkipped = "runtime_skipped"
	OnboardingPathCloudWaitlist  = "cloud_waitlist"
	OnboardingPathSkipExisting   = "skip_existing"
	OnboardingPathInviteAccept   = "invite_accept"
	OnboardingPathRoleJoin       = "role_join"
	OnboardingPathUnknown        = "unknown"
)

const (
	PlatformServer  = "server"
	PlatformWeb     = "web"
	PlatformDesktop = "desktop"
	PlatformCLI     = "cli"
)

func Signup(userID, email, signupSource string) Event {
	return Event{
		Name:       EventSignup,
		DistinctID: userID,
		Properties: map[string]any{
			"email_domain":  emailDomain(email),
			"signup_source": signupSource,
		},
		SetOnce: map[string]any{
			"signup_source": signupSource,
		},
	}
}

func WorkspaceCreated(userID, workspaceID string) Event {
	return Event{
		Name:        EventWorkspaceCreated,
		DistinctID:  userID,
		WorkspaceID: workspaceID,
		Properties: withCoreProperties(nil, CoreProperties{
			UserID:      userID,
			WorkspaceID: workspaceID,
			Source:      SourceManual,
		}),
	}
}

func RuntimeRegistered(ownerID, workspaceID, runtimeID, daemonID, provider, runtimeVersion, cliVersion string) Event {
	distinct := ownerID
	if distinct == "" {

		distinct = "workspace:" + workspaceID
	}
	return Event{
		Name:        EventRuntimeRegistered,
		DistinctID:  distinct,
		WorkspaceID: workspaceID,
		Properties: withCoreProperties(map[string]any{
			"runtime_id":      runtimeID,
			"daemon_id":       daemonID,
			"provider":        provider,
			"runtime_mode":    "local",
			"runtime_version": runtimeVersion,
			"cli_version":     cliVersion,
		}, CoreProperties{
			UserID:      ownerID,
			WorkspaceID: workspaceID,
			Source:      SourceManual,
			RuntimeMode: "local",
			Provider:    provider,
		}),
	}
}

func RuntimeReady(ownerID, workspaceID, runtimeID, daemonID, provider string, readyDurationMS int64) Event {
	distinct := ownerID
	if distinct == "" {
		distinct = "workspace:" + workspaceID
	}
	props := map[string]any{
		"runtime_id": runtimeID,
		"daemon_id":  daemonID,
	}
	if readyDurationMS > 0 {
		props["ready_duration_ms"] = readyDurationMS
	}
	return Event{
		Name:        EventRuntimeReady,
		DistinctID:  distinct,
		WorkspaceID: workspaceID,
		Properties: withCoreProperties(props, CoreProperties{
			UserID:      ownerID,
			WorkspaceID: workspaceID,
			Source:      SourceManual,
			RuntimeMode: "local",
			Provider:    provider,
		}),
	}
}

func RuntimeFailed(ownerID, workspaceID, daemonID, provider, failureReason, errorType string, recoverable bool) Event {
	distinct := ownerID
	if distinct == "" && workspaceID != "" {
		distinct = "workspace:" + workspaceID
	}
	return Event{
		Name:        EventRuntimeFailed,
		DistinctID:  distinct,
		WorkspaceID: workspaceID,
		Properties: withCoreProperties(map[string]any{
			"daemon_id":      daemonID,
			"failure_reason": failureReason,
			"error_type":     errorType,
			"recoverable":    recoverable,
		}, CoreProperties{
			UserID:      ownerID,
			WorkspaceID: workspaceID,
			Source:      SourceManual,
			RuntimeMode: "local",
			Provider:    provider,
		}),
	}
}

func RuntimeOffline(ownerID, workspaceID, runtimeID, daemonID, provider string) Event {
	distinct := ownerID
	if distinct == "" {
		distinct = "workspace:" + workspaceID
	}
	return Event{
		Name:        EventRuntimeOffline,
		DistinctID:  distinct,
		WorkspaceID: workspaceID,
		Properties: withCoreProperties(map[string]any{
			"runtime_id": runtimeID,
			"daemon_id":  daemonID,
		}, CoreProperties{
			UserID:      ownerID,
			WorkspaceID: workspaceID,
			Source:      SourceManual,
			RuntimeMode: "local",
			Provider:    provider,
		}),
	}
}

func IssueExecuted(actorID, workspaceID, issueID, taskID, agentID, source, runtimeMode, provider string, taskDurationMS int64) Event {
	return Event{
		Name:        EventIssueExecuted,
		DistinctID:  actorID,
		WorkspaceID: workspaceID,
		Properties: withCoreProperties(map[string]any{
			"issue_id":         issueID,
			"task_id":          taskID,
			"agent_id":         agentID,
			"task_duration_ms": taskDurationMS,
			"duration_ms":      taskDurationMS,
		}, CoreProperties{
			UserID:      nonAgentUserID(actorID),
			WorkspaceID: workspaceID,
			AgentID:     agentID,
			TaskID:      taskID,
			IssueID:     issueID,
			Source:      source,
			RuntimeMode: runtimeMode,
			Provider:    provider,
		}),
	}
}

func IssueCreated(actorID, workspaceID, issueID, agentID, taskID, autopilotRunID, source, platform string) Event {
	props := map[string]any{}
	if platform != "" {
		props["platform"] = platform
	}
	return Event{
		Name:        EventIssueCreated,
		DistinctID:  actorID,
		WorkspaceID: workspaceID,
		Properties: withCoreProperties(props, CoreProperties{
			UserID:         nonAgentUserID(actorID),
			WorkspaceID:    workspaceID,
			AgentID:        agentID,
			TaskID:         taskID,
			IssueID:        issueID,
			AutopilotRunID: autopilotRunID,
			Source:         source,
		}),
	}
}

func ChatMessageSent(userID, workspaceID, chatSessionID, taskID, agentID, runtimeMode, provider, platform string) Event {
	props := map[string]any{}
	if platform != "" {
		props["platform"] = platform
	}
	return Event{
		Name:        EventChatMessageSent,
		DistinctID:  userID,
		WorkspaceID: workspaceID,
		Properties: withCoreProperties(props, CoreProperties{
			UserID:        userID,
			WorkspaceID:   workspaceID,
			AgentID:       agentID,
			TaskID:        taskID,
			ChatSessionID: chatSessionID,
			Source:        SourceChat,
			RuntimeMode:   runtimeMode,
			Provider:      provider,
		}),
	}
}

type AutopilotAssignee struct {
	AgentID      string
	AssigneeType string
	SquadID      string
}

func AutopilotRunStarted(actorID, workspaceID, autopilotID, runID, cadence string, assignee AutopilotAssignee, triggerSource string) Event {
	return autopilotRunEvent(EventAutopilotRunStarted, actorID, workspaceID, autopilotID, runID, cadence, assignee, triggerSource, nil)
}

func AutopilotRunCompleted(actorID, workspaceID, autopilotID, runID, cadence string, assignee AutopilotAssignee, triggerSource string, durationMS int64) Event {
	return autopilotRunEvent(EventAutopilotRunCompleted, actorID, workspaceID, autopilotID, runID, cadence, assignee, triggerSource, map[string]any{
		"duration_ms": durationMS,
	})
}

func AutopilotRunFailed(actorID, workspaceID, autopilotID, runID, cadence string, assignee AutopilotAssignee, triggerSource, failureReason, errorType string, willRetry bool, durationMS int64) Event {
	return autopilotRunEvent(EventAutopilotRunFailed, actorID, workspaceID, autopilotID, runID, cadence, assignee, triggerSource, map[string]any{
		"duration_ms":    durationMS,
		"failure_reason": failureReason,
		"error_type":     errorType,
		"will_retry":     willRetry,
	})
}

func TeamInviteSent(inviterID, workspaceID, invitedEmail, inviteMethod string) Event {
	return Event{
		Name:        EventTeamInviteSent,
		DistinctID:  inviterID,
		WorkspaceID: workspaceID,
		Properties: map[string]any{
			"invited_email_domain": emailDomain(invitedEmail),
			"invite_method":        inviteMethod,
		},
	}
}

func TeamInviteAccepted(inviteeID, workspaceID string, daysSinceInvite int64) Event {
	return Event{
		Name:        EventTeamInviteAccepted,
		DistinctID:  inviteeID,
		WorkspaceID: workspaceID,
		Properties: map[string]any{
			"days_since_invite": daysSinceInvite,
		},
	}
}

func OnboardingStarted(userID, platform string) Event {
	props := map[string]any{}
	if platform != "" {
		props["platform"] = platform
	}
	return Event{
		Name:       EventOnboardingStarted,
		DistinctID: userID,
		Properties: withCoreProperties(props, CoreProperties{
			UserID: userID,
			Source: SourceOnboarding,
		}),
	}
}

func OnboardingQuestionnaireSubmitted(userID string, source []string, role string, useCase []string, sourceSkipped, roleSkipped, useCaseSkipped, sourceHasOther, roleHasOther, useCaseHasOther bool) Event {

	if source == nil {
		source = []string{}
	}
	if useCase == nil {
		useCase = []string{}
	}
	return Event{
		Name:       EventOnboardingQuestionnaireSubmit,
		DistinctID: userID,
		Properties: withCoreProperties(map[string]any{
			"source":             source,
			"role":               role,
			"use_case":           useCase,
			"source_skipped":     sourceSkipped,
			"role_skipped":       roleSkipped,
			"use_case_skipped":   useCaseSkipped,
			"source_has_other":   sourceHasOther,
			"role_has_other":     roleHasOther,
			"use_case_has_other": useCaseHasOther,
		}, CoreProperties{
			UserID: userID,
			Source: SourceOnboarding,
		}),
		Set: map[string]any{
			"source":   source,
			"role":     role,
			"use_case": useCase,
		},
	}
}

func OnboardingSourceSubmitted(userID string, source []string, skipped, hasOther bool) Event {
	if source == nil {
		source = []string{}
	}

	ev := Event{
		Name:       EventOnboardingSourceSubmit,
		DistinctID: userID,
		Properties: withCoreProperties(map[string]any{
			"acquisition_source": source,
			"source_skipped":     skipped,
			"source_has_other":   hasOther,
		}, CoreProperties{
			UserID: userID,
			Source: SourceOnboarding,
		}),
	}
	if len(source) > 0 {
		ev.Set = map[string]any{"source": source}
	}
	return ev
}

func AgentCreated(actorID, workspaceID, agentID, provider, runtimeMode, template string, isFirstAgentInWorkspace bool) Event {
	return Event{
		Name:        EventAgentCreated,
		DistinctID:  actorID,
		WorkspaceID: workspaceID,
		Properties: withCoreProperties(map[string]any{
			"agent_id":                    agentID,
			"provider":                    provider,
			"runtime_mode":                runtimeMode,
			"template":                    template,
			"is_first_agent_in_workspace": isFirstAgentInWorkspace,
		}, CoreProperties{
			UserID:      actorID,
			WorkspaceID: workspaceID,
			AgentID:     agentID,
			Source:      SourceManual,
			RuntimeMode: runtimeMode,
			Provider:    provider,
		}),
	}
}

func OnboardingCompleted(userID, workspaceID, completionPath, onboardedAt string, joinedCloudWaitlist bool) Event {
	return Event{
		Name:        EventOnboardingCompleted,
		DistinctID:  userID,
		WorkspaceID: workspaceID,
		Properties: withCoreProperties(map[string]any{
			"completion_path":       completionPath,
			"joined_cloud_waitlist": joinedCloudWaitlist,
		}, CoreProperties{
			UserID:      userID,
			WorkspaceID: workspaceID,
			Source:      SourceOnboarding,
		}),
		SetOnce: map[string]any{
			"onboarded_at": onboardedAt,
		},
	}
}

func CloudWaitlistJoined(userID string, hasReason bool) Event {
	return Event{
		Name:       EventCloudWaitlistJoined,
		DistinctID: userID,
		Properties: withCoreProperties(map[string]any{
			"has_reason": hasReason,
		}, CoreProperties{
			UserID: userID,
			Source: SourceOnboarding,
		}),
	}
}

func FeedbackSubmitted(userID, workspaceID, kind string, messageLen int, hasImages bool, platform, appVersion string) Event {
	props := map[string]any{
		"message_length_bucket": feedbackLengthBucket(messageLen),
		"has_images":            hasImages,
	}
	if kind != "" {
		props["kind"] = kind
	}
	if platform != "" {
		props["platform"] = platform
	}
	if appVersion != "" {
		props["app_version"] = appVersion
	}
	return Event{
		Name:        EventFeedbackSubmitted,
		DistinctID:  userID,
		WorkspaceID: workspaceID,
		Properties: withCoreProperties(props, CoreProperties{
			UserID:      userID,
			WorkspaceID: workspaceID,
			Source:      "ops_feedback",
		}),
	}
}

func ContactSalesSubmitted(inquiryID, companySize, countryRegion, useCase, formSource string, hasGoals bool) Event {
	props := map[string]any{
		"inquiry_id":     inquiryID,
		"company_size":   companySize,
		"country_region": countryRegion,
		"use_case":       useCase,
		"has_goals":      hasGoals,
	}
	if formSource != "" {
		props["form_source"] = formSource
	}
	return Event{
		Name:       EventContactSalesSubmitted,
		DistinctID: inquiryID,
		Properties: withCoreProperties(props, CoreProperties{
			Source: "marketing_contact_sales",
		}),
	}
}

func SquadCreated(actorID, workspaceID, squadID string, memberCount int) Event {
	return Event{
		Name:        EventSquadCreated,
		DistinctID:  actorID,
		WorkspaceID: workspaceID,
		Properties: withCoreProperties(map[string]any{
			"squad_id":     squadID,
			"member_count": int64(memberCount),
		}, CoreProperties{
			UserID:      nonAgentUserID(actorID),
			WorkspaceID: workspaceID,
			Source:      SourceManual,
		}),
	}
}

func AutopilotCreated(actorID, workspaceID, autopilotID, cadence, triggerKind string) Event {
	return Event{
		Name:        EventAutopilotCreated,
		DistinctID:  actorID,
		WorkspaceID: workspaceID,
		Properties: withCoreProperties(map[string]any{
			"autopilot_id": autopilotID,
			"cadence":      cadence,
			"trigger_kind": triggerKind,
		}, CoreProperties{
			UserID:      nonAgentUserID(actorID),
			WorkspaceID: workspaceID,
			Source:      SourceManual,
		}),
	}
}

func autopilotRunEvent(name, actorID, workspaceID, autopilotID, runID, cadence string, assignee AutopilotAssignee, triggerSource string, extra map[string]any) Event {
	if extra == nil {
		extra = map[string]any{}
	}
	extra["trigger_source"] = triggerSource
	extra["trigger_kind"] = triggerSource
	if cadence != "" {
		extra["cadence"] = cadence
	}
	props := withCoreProperties(extra, CoreProperties{
		UserID:         nonAgentUserID(actorID),
		WorkspaceID:    workspaceID,
		AgentID:        assignee.AgentID,
		AutopilotRunID: runID,
		Source:         SourceAutopilot,
	})
	props["autopilot_id"] = autopilotID
	if assignee.AssigneeType != "" {
		props["assignee_type"] = assignee.AssigneeType
	}
	if assignee.SquadID != "" {
		props["squad_id"] = assignee.SquadID
	}
	return Event{
		Name:        name,
		DistinctID:  actorID,
		WorkspaceID: workspaceID,
		Properties:  props,
	}
}

func withCoreProperties(props map[string]any, core CoreProperties) map[string]any {
	if props == nil {
		props = map[string]any{}
	}
	if core.UserID != "" {
		props["user_id"] = core.UserID
	}
	if core.AgentID != "" {
		props["agent_id"] = core.AgentID
	}
	if core.TaskID != "" {
		props["task_id"] = core.TaskID
	}
	if core.IssueID != "" {
		props["issue_id"] = core.IssueID
	}
	if core.ChatSessionID != "" {
		props["chat_session_id"] = core.ChatSessionID
	}
	if core.AutopilotRunID != "" {
		props["autopilot_run_id"] = core.AutopilotRunID
	}
	if core.Source != "" {
		props["source"] = core.Source
	}
	if core.RuntimeMode != "" {
		props["runtime_mode"] = core.RuntimeMode
	}
	if core.Provider != "" {
		props["provider"] = core.Provider
	}
	props["is_demo"] = core.IsDemo
	return props
}

func nonAgentUserID(distinct string) string {
	if distinct == "" || strings.Contains(distinct, ":") {
		return ""
	}
	return distinct
}

func feedbackLengthBucket(n int) string {
	switch {
	case n < 100:
		return "0-100"
	case n < 500:
		return "100-500"
	case n < 2000:
		return "500-2000"
	default:
		return "2000+"
	}
}

func emailDomain(email string) string {
	at := strings.LastIndex(email, "@")
	if at < 0 || at == len(email)-1 {
		return ""
	}
	return strings.ToLower(email[at+1:])
}
