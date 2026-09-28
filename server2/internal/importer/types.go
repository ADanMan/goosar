package importer

import "time"

// Типы этого файла — минимальные DTO под ответы исходного сервера
// (docs/50-api-contract.yaml, api.example/v1.2.0). Поля, которые импортёр не
// использует (маскированные секреты, чисто клиентские вычисляемые поля вроде
// has_unread), сознательно не описаны — лишние поля JSON просто игнорируются
// декодером.

type sourceUser struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Email       string `json:"email"`
	AvatarURL   *string `json:"avatar_url"`
	Language    *string `json:"language"`
	Timezone    *string `json:"timezone"`
	OnboardedAt *time.Time `json:"onboarded_at"`
	Onboarding  map[string]any `json:"onboarding_questionnaire"`
	StarterState *string `json:"starter_content_state"`
	Bio         string `json:"profile_description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type sourceWorkspace struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Slug        string         `json:"slug"`
	Description *string        `json:"description"`
	Context     *string        `json:"context"`
	Settings    map[string]any `json:"settings"`
	Repos       []map[string]any `json:"repos"`
	IssuePrefix string         `json:"issue_prefix"`
	AvatarURL   *string        `json:"avatar_url"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

type sourceMember struct {
	ID              string    `json:"id"`
	WorkspaceID     string    `json:"workspace_id"`
	UserID          string    `json:"user_id"`
	Role            string    `json:"role"`
	CreatedAt       time.Time `json:"created_at"`
	Name            string    `json:"name"`
	Email           string    `json:"email"`
	AvatarURL       *string   `json:"avatar_url"`
	PerimeterAccess bool      `json:"perimeter_access"`
}

type sourceRuntimeProfile struct {
	ID              string    `json:"id"`
	WorkspaceID     string    `json:"workspace_id"`
	DisplayName     string    `json:"display_name"`
	ProtocolFamily  string    `json:"protocol_family"`
	CommandName     string    `json:"command_name"`
	Description     *string   `json:"description"`
	FixedArgs       []string  `json:"fixed_args"`
	Visibility      string    `json:"visibility"`
	CreatedBy       *string   `json:"created_by"`
	Enabled         bool      `json:"enabled"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type sourceRuntimeProfilesResponse struct {
	RuntimeProfiles []sourceRuntimeProfile `json:"runtime_profiles"`
}

type sourceRuntime struct {
	ID          string         `json:"id"`
	WorkspaceID string         `json:"workspace_id"`
	DaemonID    *string        `json:"daemon_id"`
	Name        string         `json:"name"`
	CustomName  *string        `json:"custom_name"`
	RuntimeMode string         `json:"runtime_mode"`
	Provider    string         `json:"provider"`
	LaunchHeader string        `json:"launch_header"`
	Status      string         `json:"status"`
	DeviceInfo  string         `json:"device_info"`
	Metadata    map[string]any `json:"metadata"`
	OwnerID     *string        `json:"owner_id"`
	Visibility  string         `json:"visibility"`
	ProfileID   *string        `json:"profile_id"`
	LastSeenAt  *time.Time     `json:"last_seen_at"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

type sourceAgentInvocationTarget struct {
	TargetType string `json:"target_type"`
	TargetID   *string `json:"target_id"`
}

type sourceAgentSkillSummary struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
}

type sourceAgent struct {
	ID                  string                        `json:"id"`
	WorkspaceID         string                        `json:"workspace_id"`
	RuntimeID           string                        `json:"runtime_id"`
	Name                string                        `json:"name"`
	Description         string                        `json:"description"`
	Instructions        string                        `json:"instructions"`
	AvatarURL           *string                       `json:"avatar_url"`
	RuntimeMode         string                        `json:"runtime_mode"`
	CustomArgs          []string                      `json:"custom_args"`
	PermissionMode      string                        `json:"permission_mode"`
	InvocationTargets   []sourceAgentInvocationTarget `json:"invocation_targets"`
	Status              string                        `json:"status"`
	MaxConcurrentTasks  int                            `json:"max_concurrent_tasks"`
	Model               string                        `json:"model"`
	ThinkingLevel       string                        `json:"thinking_level"`
	ServiceTier         string                        `json:"service_tier"`
	ComposioAllowlist   []string                       `json:"composio_toolkit_allowlist"`
	ComposioRedacted    bool                           `json:"composio_toolkit_allowlist_redacted"`
	OwnerID             *string                        `json:"owner_id"`
	Skills              []sourceAgentSkillSummary      `json:"skills"`
	CreatedAt           time.Time                      `json:"created_at"`
	UpdatedAt           time.Time                      `json:"updated_at"`
	ArchivedAt          *time.Time                     `json:"archived_at"`
	ArchivedBy          *string                        `json:"archived_by"`
	SystemKey           *string                        `json:"system_key"`
}

type sourceSkillFile struct {
	ID        string    `json:"id"`
	SkillID   string    `json:"skill_id"`
	Path      string    `json:"path"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type sourceSkillSummary struct {
	ID          string         `json:"id"`
	WorkspaceID string         `json:"workspace_id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Config      map[string]any `json:"config"`
	CreatedBy   *string        `json:"created_by"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

type sourceSkillWithFiles struct {
	sourceSkillSummary
	Content string            `json:"content"`
	Files   []sourceSkillFile `json:"files"`
}

type sourceSquadMemberPreview struct {
	MemberType string `json:"member_type"`
	MemberID   string `json:"member_id"`
	Role       string `json:"role"`
}

type sourceSquad struct {
	ID           string     `json:"id"`
	WorkspaceID  string     `json:"workspace_id"`
	Name         string     `json:"name"`
	Description  string     `json:"description"`
	Instructions string     `json:"instructions"`
	AvatarURL    *string    `json:"avatar_url"`
	LeaderID     string     `json:"leader_id"`
	CreatorID    string     `json:"creator_id"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	ArchivedAt   *time.Time `json:"archived_at"`
	ArchivedBy   *string    `json:"archived_by"`
}

type sourceSquadMember struct {
	ID         string    `json:"id"`
	SquadID    string    `json:"squad_id"`
	MemberType string    `json:"member_type"`
	MemberID   string    `json:"member_id"`
	Role       string    `json:"role"`
	CreatedAt  time.Time `json:"created_at"`
}

type sourceLabel struct {
	ID           string    `json:"id"`
	WorkspaceID  string    `json:"workspace_id"`
	ResourceType string    `json:"resource_type"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	Color        string    `json:"color"`
	UsageCount   int       `json:"usage_count"`
	CreatedAt    time.Time `json:"created_at"`
}

type sourceLabelsResponse struct {
	Labels []sourceLabel `json:"labels"`
	Total  int           `json:"total"`
}

type sourcePropertyConfig struct {
	Options []map[string]any `json:"options"`
}

type sourceProperty struct {
	ID         string               `json:"id"`
	WorkspaceID string              `json:"workspace_id"`
	Name       string               `json:"name"`
	Type       string               `json:"type"`
	Description string              `json:"description"`
	Icon       string               `json:"icon"`
	Config     sourcePropertyConfig `json:"config"`
	Position   float64              `json:"position"`
	Archived   bool                 `json:"archived"`
	ArchivedAt *time.Time           `json:"archived_at"`
	UsageCount int                  `json:"usage_count"`
	CreatedAt  time.Time            `json:"created_at"`
	UpdatedAt  time.Time            `json:"updated_at"`
}

type sourcePropertiesResponse struct {
	Properties []sourceProperty `json:"properties"`
	Total      int              `json:"total"`
}

type sourceProject struct {
	ID          string     `json:"id"`
	WorkspaceID string     `json:"workspace_id"`
	Title       string     `json:"title"`
	Description *string    `json:"description"`
	Icon        *string    `json:"icon"`
	Status      string     `json:"status"`
	Priority    string     `json:"priority"`
	LeadType    *string    `json:"lead_type"`
	LeadID      *string    `json:"lead_id"`
	StartDate   *string    `json:"start_date"`
	DueDate     *string    `json:"due_date"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type sourceProjectsResponse struct {
	Projects []sourceProject `json:"projects"`
	Total    int             `json:"total"`
}

type sourceProjectResource struct {
	ID           string         `json:"id"`
	ProjectID    string         `json:"project_id"`
	WorkspaceID  string         `json:"workspace_id"`
	ResourceType string         `json:"resource_type"`
	ResourceRef  map[string]any `json:"resource_ref"`
	Label        *string        `json:"label"`
	Position     int            `json:"position"`
	CreatedAt    time.Time      `json:"created_at"`
	CreatedBy    *string        `json:"created_by"`
}

type sourceProjectResourcesResponse struct {
	Resources []sourceProjectResource `json:"resources"`
	Total     int                     `json:"total"`
}

type sourceIssueReaction struct {
	ID        string    `json:"id"`
	IssueID   string    `json:"issue_id"`
	ActorType string    `json:"actor_type"`
	ActorID   string    `json:"actor_id"`
	Emoji     string    `json:"emoji"`
	CreatedAt time.Time `json:"created_at"`
}

type sourceLabelRef struct {
	ID string `json:"id"`
}

type sourceIssue struct {
	ID           string           `json:"id"`
	WorkspaceID  string           `json:"workspace_id"`
	Number       int              `json:"number"`
	Identifier   string           `json:"identifier"`
	Title        string           `json:"title"`
	Description  *string          `json:"description"`
	Status       string           `json:"status"`
	Priority     string           `json:"priority"`
	AssigneeType *string          `json:"assignee_type"`
	AssigneeID   *string          `json:"assignee_id"`
	CreatorType  string           `json:"creator_type"`
	CreatorID    string           `json:"creator_id"`
	ParentIssueID *string         `json:"parent_issue_id"`
	ProjectID    *string          `json:"project_id"`
	Position     *float64         `json:"position"`
	Stage        *int             `json:"stage"`
	StartDate    *string          `json:"start_date"`
	DueDate      *string          `json:"due_date"`
	CreatedAt    time.Time        `json:"created_at"`
	UpdatedAt    time.Time        `json:"updated_at"`
	Metadata     map[string]any   `json:"metadata"`
	Properties   map[string]any   `json:"properties"`
	Reactions    []sourceIssueReaction `json:"reactions"`
	Labels       []sourceLabelRef `json:"labels"`
}

type sourceIssueListResponse struct {
	Issues []sourceIssue `json:"issues"`
	Total  int           `json:"total"`
}

type sourceCommentReaction struct {
	ID        string    `json:"id"`
	CommentID string    `json:"comment_id"`
	ActorType string    `json:"actor_type"`
	ActorID   string    `json:"actor_id"`
	Emoji     string    `json:"emoji"`
	CreatedAt time.Time `json:"created_at"`
}

type sourceIssueComment struct {
	ID              string                  `json:"id"`
	IssueID         string                  `json:"issue_id"`
	AuthorType      string                  `json:"author_type"`
	AuthorID        string                  `json:"author_id"`
	Content         string                  `json:"content"`
	Type            string                  `json:"type"`
	ParentID        *string                 `json:"parent_id"`
	CreatedAt       time.Time               `json:"created_at"`
	UpdatedAt       time.Time               `json:"updated_at"`
	ResolvedAt      *time.Time              `json:"resolved_at"`
	ResolvedByType  *string                 `json:"resolved_by_type"`
	ResolvedByID    *string                 `json:"resolved_by_id"`
	Reactions       []sourceCommentReaction `json:"reactions"`
}

type sourceIssueSubscriber struct {
	IssueID   string    `json:"issue_id"`
	UserType  string    `json:"user_type"`
	UserID    string    `json:"user_id"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}

type sourceIssuePullRequestLink struct {
	IssueID   string    `json:"issue_id"`
	Provider  string    `json:"provider"`
	URL       string    `json:"url"`
	Number    int       `json:"number"`
	Title     *string   `json:"title"`
	State     *string   `json:"state"`
	CreatedAt time.Time `json:"created_at"`
}

type sourceAutopilot struct {
	ID                string     `json:"id"`
	WorkspaceID       string     `json:"workspace_id"`
	Title             string     `json:"title"`
	Description       *string    `json:"description"`
	ProjectID         *string    `json:"project_id"`
	AssigneeType      string     `json:"assignee_type"`
	AssigneeID        string     `json:"assignee_id"`
	Status            string     `json:"status"`
	ExecutionMode     string     `json:"execution_mode"`
	IssueTitleTemplate *string   `json:"issue_title_template"`
	CreatedByType     string     `json:"created_by_type"`
	CreatedByID       string     `json:"created_by_id"`
	LastRunAt         *time.Time `json:"last_run_at"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	IsTemplate        bool       `json:"is_template"`
}

type sourceAutopilotsResponse struct {
	Autopilots []sourceAutopilot `json:"autopilots"`
	Total      int               `json:"total"`
}

type sourceAutopilotTrigger struct {
	ID              string         `json:"id"`
	AutopilotID     string         `json:"autopilot_id"`
	Kind            string         `json:"kind"`
	Enabled         bool           `json:"enabled"`
	CronExpression  *string        `json:"cron_expression"`
	Timezone        *string        `json:"timezone"`
	NextRunAt       *time.Time     `json:"next_run_at"`
	WebhookPath     *string        `json:"webhook_path"`
	Provider        *string        `json:"provider"`
	HasSigningSecret bool          `json:"has_signing_secret"`
	Label           *string        `json:"label"`
	LastFiredAt     *time.Time     `json:"last_fired_at"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	EventFilters    []map[string]any `json:"event_filters"`
}

type sourceGetAutopilotResponse struct {
	Autopilot sourceAutopilot          `json:"autopilot"`
	Triggers  []sourceAutopilotTrigger `json:"triggers"`
}

type sourceChatAttachment struct {
	ID string `json:"id"`
}

type sourceChatSession struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	AgentID     string    `json:"agent_id"`
	CreatorID   string    `json:"creator_id"`
	ProjectID   *string   `json:"project_id"`
	Title       string    `json:"title"`
	Status      string    `json:"status"`
	Pinned      bool      `json:"pinned"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type sourceChatMessage struct {
	ID            string     `json:"id"`
	ChatSessionID string     `json:"chat_session_id"`
	Role          string     `json:"role"`
	Content       string     `json:"content"`
	TaskID        *string    `json:"task_id"`
	CreatedAt     time.Time  `json:"created_at"`
	FailureReason *string    `json:"failure_reason"`
	ElapsedMs     *int       `json:"elapsed_ms"`
	MessageKind   string     `json:"message_kind"`
}

type sourceChatMessagesPage struct {
	Messages   []sourceChatMessage `json:"messages"`
	HasMore    bool                `json:"has_more"`
	NextCursor map[string]any      `json:"next_cursor"`
}

type sourceMcpCredentialField struct {
	Key string `json:"key"`
}

type sourceWorkspaceMcpServer struct {
	ID                string                     `json:"id"`
	WorkspaceID       string                     `json:"workspace_id"`
	Name              string                     `json:"name"`
	Transport         string                     `json:"transport"`
	Source            string                     `json:"source"`
	CredentialSchema  []sourceMcpCredentialField `json:"credential_schema"`
	CreatedAt         time.Time                  `json:"created_at"`
	UpdatedAt         time.Time                  `json:"updated_at"`
}

type sourceIssuePullRequestsResponse struct {
	PullRequests []sourceIssuePullRequestLink `json:"pull_requests"`
}

type sourceWorkspaceConfigLayer struct {
	LLMBaseURL   string         `json:"llm_base_url"`
	LLMModel     string         `json:"llm_model"`
	HasLLMAPIKey bool           `json:"has_llm_api_key"`
	McpDefaults  map[string]any `json:"mcp_defaults"`
	UpdatedAt    *time.Time     `json:"updated_at"`
	UpdatedBy    *string        `json:"updated_by"`
}
