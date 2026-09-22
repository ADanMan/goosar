package daemon

import (
	"encoding/json"

	"github.com/adanman/goosar/server/internal/perimeterpolicy"
	"github.com/adanman/goosar/server/internal/runtimeapps"
)

type AgentEntry struct {
	Path string

	Command string
	Model   string
}

type Runtime struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Status   string `json:"status"`

	ProfileID string `json:"profile_id,omitempty"`
}

type RepoData struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
	Ref         string `json:"ref,omitempty"`
}

type ProjectResourceData struct {
	ID           string          `json:"id"`
	ResourceType string          `json:"resource_type"`
	ResourceRef  json.RawMessage `json:"resource_ref"`
	Label        string          `json:"label,omitempty"`
}

type ConnectedAppData = runtimeapps.ConnectedApp

type Task struct {
	ID          string `json:"id"`
	AgentID     string `json:"agent_id"`
	RuntimeID   string `json:"runtime_id"`
	IssueID     string `json:"issue_id"`
	WorkspaceID string `json:"workspace_id"`

	WorkspaceContext              string                 `json:"workspace_context,omitempty"`
	ThreadName                    string                 `json:"thread_name,omitempty"`
	Agent                         *AgentData             `json:"agent,omitempty"`
	ConnectedApps                 []ConnectedAppData     `json:"connected_apps,omitempty"`
	Repos                         []RepoData             `json:"repos,omitempty"`
	ProjectID                     string                 `json:"project_id,omitempty"`
	ProjectTitle                  string                 `json:"project_title,omitempty"`
	ProjectDescription            string                 `json:"project_description,omitempty"`
	ProjectResources              []ProjectResourceData  `json:"project_resources,omitempty"`
	IsLeaderTask                  bool                   `json:"is_leader_task,omitempty"`
	PriorSessionID                string                 `json:"prior_session_id,omitempty"`
	PriorWorkDir                  string                 `json:"prior_work_dir,omitempty"`
	PriorSessionResumeUnavailable bool                   `json:"prior_session_resume_unavailable,omitempty"`
	TriggerCommentID              string                 `json:"trigger_comment_id,omitempty"`
	CoalescedCommentIDs           []string               `json:"coalesced_comment_ids,omitempty"`
	CoalescedComments             []CoalescedCommentData `json:"coalesced_comments,omitempty"`
	TriggerThreadID               string                 `json:"trigger_thread_id,omitempty"`
	TriggerCommentContent         string                 `json:"trigger_comment_content,omitempty"`
	TriggerAuthorType             string                 `json:"trigger_author_type,omitempty"`
	TriggerAuthorName             string                 `json:"trigger_author_name,omitempty"`
	NewCommentCount               int                    `json:"new_comment_count,omitempty"`
	NewCommentsSince              string                 `json:"new_comments_since,omitempty"`
	ChatSessionID                 string                 `json:"chat_session_id,omitempty"`
	ChatChannelType               string                 `json:"chat_channel_type,omitempty"`
	ChatInThread                  bool                   `json:"chat_in_thread,omitempty"`
	ChatMessage                   string                 `json:"chat_message,omitempty"`
	ChatMessageAttachments        []ChatAttachmentMeta   `json:"chat_message_attachments,omitempty"`
	ChatIntro                     bool                   `json:"chat_intro,omitempty"`
	AutopilotRunID                string                 `json:"autopilot_run_id,omitempty"`
	AutopilotID                   string                 `json:"autopilot_id,omitempty"`
	AutopilotTitle                string                 `json:"autopilot_title,omitempty"`
	AutopilotDescription          string                 `json:"autopilot_description,omitempty"`
	AutopilotSource               string                 `json:"autopilot_source,omitempty"`
	AutopilotTriggerPayload       json.RawMessage        `json:"autopilot_trigger_payload,omitempty"`
	QuickCreatePrompt             string                 `json:"quick_create_prompt,omitempty"`
	QuickCreatePriority           string                 `json:"quick_create_priority,omitempty"`
	QuickCreateDueDate            string                 `json:"quick_create_due_date,omitempty"`
	QuickCreateAttachmentIDs      []string               `json:"quick_create_attachment_ids,omitempty"`
	HandoffNote                   string                 `json:"handoff_note,omitempty"`

	McpPolicy *McpPolicyData `json:"mcp_policy,omitempty"`

	SquadID               string `json:"squad_id,omitempty"`
	SquadName             string `json:"squad_name,omitempty"`
	ParentIssueID         string `json:"parent_issue_id,omitempty"`
	ParentIssueIdentifier string `json:"parent_issue_identifier,omitempty"`

	RequestingUserName               string `json:"requesting_user_name,omitempty"`
	RequestingUserProfileDescription string `json:"requesting_user_profile_description,omitempty"`

	InitiatorType  string `json:"initiator_type,omitempty"`
	InitiatorID    string `json:"initiator_id,omitempty"`
	InitiatorName  string `json:"initiator_name,omitempty"`
	InitiatorEmail string `json:"initiator_email,omitempty"`

	AuthToken string `json:"auth_token,omitempty"`
}

type ChatAttachmentMeta struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type,omitempty"`
}

type CoalescedCommentData struct {
	ID         string `json:"id"`
	ThreadID   string `json:"thread_id,omitempty"`
	AuthorType string `json:"author_type,omitempty"`
	AuthorName string `json:"author_name,omitempty"`
	Content    string `json:"content"`
	CreatedAt  string `json:"created_at,omitempty"`
}

type AgentData struct {
	ID                    string                     `json:"id"`
	Name                  string                     `json:"name"`
	Instructions          string                     `json:"instructions"`
	Skills                []SkillData                `json:"skills,omitempty"`
	SkillRefs             []SkillRefData             `json:"skill_refs,omitempty"`
	CustomEnv             map[string]string          `json:"custom_env,omitempty"`
	CustomArgs            []string                   `json:"custom_args,omitempty"`
	McpConfig             json.RawMessage            `json:"mcp_config,omitempty"`
	Model                 string                     `json:"model,omitempty"`
	ThinkingLevel         string                     `json:"thinking_level,omitempty"`
	ServiceTier           string                     `json:"service_tier,omitempty"`
	DisabledRuntimeSkills []DisabledRuntimeSkillData `json:"disabled_runtime_skills,omitempty"`

	RuntimeConfig json.RawMessage `json:"runtime_config,omitempty"`
}

type DisabledRuntimeSkillData struct {
	RuntimeID string `json:"runtime_id"`
	Provider  string `json:"provider"`
	Root      string `json:"root"`
	Key       string `json:"key"`
	Name      string `json:"name,omitempty"`
	Plugin    string `json:"plugin,omitempty"`
}

type SkillData struct {
	ID          string          `json:"id"`
	Source      string          `json:"source,omitempty"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Hash        string          `json:"hash,omitempty"`
	SizeBytes   int64           `json:"size_bytes,omitempty"`
	Content     string          `json:"content"`
	Files       []SkillFileData `json:"files,omitempty"`
}

type SkillFileData struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	SHA256    string `json:"sha256,omitempty"`
	SizeBytes int64  `json:"size_bytes,omitempty"`
}

type SkillRefData struct {
	ID          string             `json:"id"`
	Source      string             `json:"source"`
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	Hash        string             `json:"hash"`
	SizeBytes   int64              `json:"size_bytes"`
	FileCount   int                `json:"file_count"`
	Files       []SkillFileRefData `json:"files,omitempty"`
}

type SkillFileRefData struct {
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
}

type TaskUsageEntry struct {
	Provider         string `json:"provider"`
	Model            string `json:"model"`
	InputTokens      int64  `json:"input_tokens"`
	OutputTokens     int64  `json:"output_tokens"`
	CacheReadTokens  int64  `json:"cache_read_tokens"`
	CacheWriteTokens int64  `json:"cache_write_tokens"`

	CostUSDTicks int64 `json:"cost_usd_ticks,omitempty"`
}

type TaskResult struct {
	Status        string `json:"status"`
	Comment       string `json:"comment"`
	BranchName    string `json:"branch_name,omitempty"`
	EnvType       string `json:"env_type,omitempty"`
	SessionID     string `json:"session_id,omitempty"`
	WorkDir       string `json:"work_dir,omitempty"`
	EnvRoot       string `json:"-"`
	FailureReason string `json:"-"`

	SessionRolloutMissing bool             `json:"-"`
	Usage                 []TaskUsageEntry `json:"usage,omitempty"`
}

type McpPolicyData struct {
	AllowedHosts       []string `json:"allowed_hosts,omitempty"`
	HostsRestricted    bool     `json:"hosts_restricted,omitempty"`
	AllowedCommands    []string `json:"allowed_commands,omitempty"`
	CommandsRestricted bool     `json:"commands_restricted,omitempty"`
}

func (d *McpPolicyData) Policy() *perimeterpolicy.MCPPolicy {
	if d == nil {
		return nil
	}
	return perimeterpolicy.NewMCPPolicy(d.AllowedHosts, d.HostsRestricted, d.AllowedCommands, d.CommandsRestricted)
}
