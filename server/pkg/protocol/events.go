package protocol

const (
	EventIssueCreated         = "issue:created"
	EventIssueUpdated         = "issue:updated"
	EventIssueDeleted         = "issue:deleted"
	EventIssueMetadataChanged = "issue_metadata:changed"

	EventCommentCreated       = "comment:created"
	EventCommentUpdated       = "comment:updated"
	EventCommentDeleted       = "comment:deleted"
	EventCommentResolved      = "comment:resolved"
	EventCommentUnresolved    = "comment:unresolved"
	EventReactionAdded        = "reaction:added"
	EventReactionRemoved      = "reaction:removed"
	EventIssueReactionAdded   = "issue_reaction:added"
	EventIssueReactionRemoved = "issue_reaction:removed"

	EventAgentStatus   = "agent:status"
	EventAgentCreated  = "agent:created"
	EventAgentArchived = "agent:archived"
	EventAgentRestored = "agent:restored"

	EventTaskQueued                = "task:queued"
	EventTaskDispatch              = "task:dispatch"
	EventTaskRunning               = "task:running"
	EventTaskWaitingLocalDirectory = "task:waiting_local_directory"
	EventTaskProgress              = "task:progress"
	EventTaskCompleted             = "task:completed"
	EventTaskFailed                = "task:failed"
	EventTaskMessage               = "task:message"
	EventTaskCancelled             = "task:cancelled"

	EventInboxNew           = "inbox:new"
	EventInboxRead          = "inbox:read"
	EventInboxArchived      = "inbox:archived"
	EventInboxUnarchived    = "inbox:unarchived"
	EventInboxBatchRead     = "inbox:batch-read"
	EventInboxBatchArchived = "inbox:batch-archived"

	EventWorkspaceUpdated = "workspace:updated"
	EventWorkspaceDeleted = "workspace:deleted"

	EventMemberAdded   = "member:added"
	EventMemberUpdated = "member:updated"
	EventMemberRemoved = "member:removed"

	EventSubscriberAdded   = "subscriber:added"
	EventSubscriberRemoved = "subscriber:removed"

	EventActivityCreated = "activity:created"

	EventSkillCreated = "skill:created"
	EventSkillUpdated = "skill:updated"
	EventSkillDeleted = "skill:deleted"

	EventChatMessage = "chat:message"
	EventChatDone    = "chat:done"

	EventChatCancelFinalized = "chat:cancel_finalized"
	EventChatSessionRead     = "chat:session_read"
	EventChatSessionDeleted  = "chat:session_deleted"
	EventChatSessionUpdated  = "chat:session_updated"

	EventProjectCreated         = "project:created"
	EventProjectUpdated         = "project:updated"
	EventProjectDeleted         = "project:deleted"
	EventProjectResourceCreated = "project_resource:created"
	EventProjectResourceUpdated = "project_resource:updated"
	EventProjectResourceDeleted = "project_resource:deleted"

	EventLabelCreated       = "label:created"
	EventLabelUpdated       = "label:updated"
	EventLabelDeleted       = "label:deleted"
	EventIssueLabelsChanged = "issue_labels:changed"

	EventPropertyCreated        = "property:created"
	EventPropertyUpdated        = "property:updated"
	EventIssuePropertiesChanged = "issue_properties:changed"

	EventPinCreated   = "pin:created"
	EventPinDeleted   = "pin:deleted"
	EventPinReordered = "pin:reordered"

	EventInvitationCreated  = "invitation:created"
	EventInvitationAccepted = "invitation:accepted"
	EventInvitationDeclined = "invitation:declined"
	EventInvitationRevoked  = "invitation:revoked"

	EventAutopilotCreated  = "autopilot:created"
	EventAutopilotUpdated  = "autopilot:updated"
	EventAutopilotDeleted  = "autopilot:deleted"
	EventAutopilotRunStart = "autopilot:run_start"
	EventAutopilotRunDone  = "autopilot:run_done"

	EventSquadCreated = "squad:created"
	EventSquadUpdated = "squad:updated"
	EventSquadDeleted = "squad:deleted"

	EventDaemonHeartbeat              = "daemon:heartbeat"
	EventDaemonHeartbeatAck           = "daemon:heartbeat_ack"
	EventDaemonRegister               = "daemon:register"
	EventDaemonTaskAvailable          = "daemon:task_available"
	EventDaemonRuntimeProfilesChanged = "daemon:runtime_profiles_changed"
	EventDaemonWorkspacesChanged      = "daemon:workspaces_changed"

	EventDaemonRPCRequest  = "daemon:rpc_request"
	EventDaemonRPCResponse = "daemon:rpc_response"

	EventGitHubInstallationCreated = "github_installation:created"
	EventGitHubInstallationDeleted = "github_installation:deleted"
	EventPullRequestLinked         = "pull_request:linked"
	EventPullRequestUpdated        = "pull_request:updated"
	EventPullRequestUnlinked       = "pull_request:unlinked"

	EventVCSConnectionCreated = "vcs_connection:created"
	EventVCSConnectionDeleted = "vcs_connection:deleted"

	EventSlackInstallationCreated = "slack_installation:created"
	EventSlackInstallationRevoked = "slack_installation:revoked"
)
