// Package deployment реализует раздел контракта «администрирование деплоя»
// (docs/50-api-contract.md §7 и последующие одиночные ручки): роли
// deployment-admin с двухканальным подтверждением, объединённый аудит
// admin+auth, управление учётными записями на уровне деплоя, библиотеку
// MCP-серверов деплоя и воркспейса, документ политики деплоя, обзор
// воркспейсов/ролевых пространств/парка машин, слой конфигурации воркспейса
// (LLM/MCP) с персональными override'ами, эффективную конфигурацию,
// provisioning (манифест/каталог/pin'ы) и одиночные ручки
// client-secrets/llm-health. Схема БД (platform_admins, platform_admin_requests,
// platform_audit_log, platform_mcp_servers, platform_policy, provisioning_pins,
// space_config(_overrides), space_mcp_servers(_credentials)) была
// спроектирована ещё в T-025 (011_governance.up.sql, 002_workspace.up.sql) —
// этот домен первый, кто её читает и пишет по HTTP.
package deployment

import "time"

// DeploymentAdmin — components/schemas/DeploymentAdmin.
type DeploymentAdmin struct {
	UserID    string    `json:"user_id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	GrantedBy string    `json:"granted_by"`
	GrantedAt time.Time `json:"granted_at"`
}

// PendingRequest — components/schemas/DeploymentAdminPendingRequest.
type PendingRequest struct {
	Status       string    `json:"status"`
	RequestID    string    `json:"request_id"`
	Action       string    `json:"action"`
	TargetUserID *string   `json:"target_user_id,omitempty"`
	TargetEmail  *string   `json:"target_email,omitempty"`
	RequestedBy  string    `json:"requested_by"`
	RequestedAt  time.Time `json:"requested_at"`
	ConfirmHint  string    `json:"confirm_hint"`
}

// AuditEntry — components/schemas/DeploymentAuditEntry.
type AuditEntry struct {
	ID           string    `json:"id"`
	Source       string    `json:"source"`
	Action       string    `json:"action"`
	ActorUserID  *string   `json:"actor_user_id,omitempty"`
	ActorType    *string   `json:"actor_type,omitempty"`
	ActorID      *string   `json:"actor_id,omitempty"`
	ActorRole    *string   `json:"actor_role,omitempty"`
	TargetType   *string   `json:"target_type,omitempty"`
	TargetID     *string   `json:"target_id,omitempty"`
	Outcome      *string   `json:"outcome,omitempty"`
	Reason       *string   `json:"reason,omitempty"`
	BeforeHash   *string   `json:"before_hash,omitempty"`
	AfterHash    *string   `json:"after_hash,omitempty"`
	WorkspaceID  *string   `json:"workspace_id,omitempty"`
	RequestIDHdr *string   `json:"request_id,omitempty"`
	ClientIP     *string   `json:"client_ip,omitempty"`
	UserAgent    *string   `json:"user_agent,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	Cursor       string    `json:"cursor"`
}

// DeploymentUser — components/schemas/DeploymentUser.
type DeploymentUser struct {
	UserID            string     `json:"user_id"`
	Email             string     `json:"email"`
	Name              string     `json:"name"`
	Deactivated       bool       `json:"deactivated"`
	DeactivatedAt     *time.Time `json:"deactivated_at"`
	TokenVersion      int        `json:"token_version"`
	RevokedTokens     int        `json:"revoked_tokens"`
	ClosedConnections int        `json:"closed_connections"`
}

// DeleteUserResponse — components/schemas/DeleteDeploymentUserResponse.
type DeleteUserResponse struct {
	UserID             string `json:"user_id"`
	Email              string `json:"email"`
	Name               string `json:"name"`
	RemovedMemberships int    `json:"removed_memberships"`
	RevokedTokens      int    `json:"revoked_tokens"`
	ClosedConnections  int    `json:"closed_connections"`
}

// McpCredentialField — components/schemas/McpCredentialField.
type McpCredentialField struct {
	Key      string `json:"key"`
	Label    string `json:"label,omitempty"`
	Hint     string `json:"hint,omitempty"`
	Required bool   `json:"required"`
}

// PlatformMcpServer — components/schemas/DeploymentMcpServer.
type PlatformMcpServer struct {
	ID                string               `json:"id"`
	Name              string               `json:"name"`
	Transport         string               `json:"transport"`
	CredentialSchema  []McpCredentialField `json:"credential_schema"`
	EnabledWorkspaces *int                 `json:"enabled_workspaces,omitempty"`
	Enabled           *bool                `json:"enabled,omitempty"`
	CreatedAt         time.Time            `json:"created_at"`
	UpdatedAt         time.Time            `json:"updated_at"`
}

// PlatformMcpServerRequest — components/schemas/DeploymentMcpServerRequest.
type PlatformMcpServerRequest struct {
	Name             string               `json:"name"`
	Config           map[string]any       `json:"config"`
	CredentialSchema []McpCredentialField `json:"credential_schema"`
}

// PolicyBody — components/schemas/DeploymentPolicyBody.
type PolicyBody struct {
	LLM     map[string]any            `json:"llm,omitempty"`
	MCP     map[string]map[string]any `json:"mcp,omitempty"`
	Session map[string]any            `json:"session,omitempty"`
}

// PolicyDocument — components/schemas/DeploymentPolicyDocument.
type PolicyDocument struct {
	Policy    PolicyBody `json:"policy"`
	UpdatedAt *time.Time `json:"updated_at"`
}

// DeploymentWorkspace — components/schemas/DeploymentWorkspace.
type DeploymentWorkspace struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	MemberCount int    `json:"member_count"`
}

// WorkspaceMember — components/schemas/DeploymentWorkspaceMember.
type WorkspaceMember struct {
	UserID      string `json:"user_id"`
	Name        string `json:"name"`
	Email       string `json:"email"`
	Role        string `json:"role"`
	Deactivated bool   `json:"deactivated"`
}

// JoinTarget — components/schemas/JoinTarget.
type JoinTarget struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	TemplateKey string `json:"template_key,omitempty"`
	MemberCount int    `json:"member_count"`
}

// JoinTargetResult — components/schemas/JoinTargetResult.
type JoinTargetResult struct {
	ID            string `json:"id"`
	Slug          string `json:"slug"`
	Name          string `json:"name"`
	AlreadyMember bool   `json:"already_member"`
}

// --- fleet -------------------------------------------------------------------

type FleetAgent struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	SystemKey string `json:"system_key"`
	Status    string `json:"status"`
}

type FleetRuntime struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	Provider     string       `json:"provider"`
	Visibility   string       `json:"visibility"`
	Status       string       `json:"status"`
	Online       bool         `json:"online"`
	LastSeenAt   *time.Time   `json:"last_seen_at"`
	RunningTasks int          `json:"running_tasks"`
	StuckTasks   int          `json:"stuck_tasks"`
	Agents       []FleetAgent `json:"agents"`
}

type FleetMachine struct {
	DaemonID        string         `json:"daemon_id"`
	WorkspaceID     string         `json:"workspace_id"`
	WorkspaceName   string         `json:"workspace_name"`
	WorkspaceSlug   string         `json:"workspace_slug"`
	OwnerEmail      string         `json:"owner_email"`
	OwnerName       string         `json:"owner_name"`
	DeviceInfo      string         `json:"device_info"`
	Online          bool           `json:"online"`
	LastHeartbeatAt *time.Time     `json:"last_heartbeat_at"`
	ClientVersion   string         `json:"client_version"`
	VersionOutdated bool           `json:"version_outdated"`
	RunningTasks    int            `json:"running_tasks"`
	StuckTasks      int            `json:"stuck_tasks"`
	Runtimes        []FleetRuntime `json:"runtimes"`
}

type FleetSummary struct {
	MachinesTotal    int `json:"machines_total"`
	MachinesOnline   int `json:"machines_online"`
	MachinesOffline  int `json:"machines_offline"`
	OutdatedVersions int `json:"outdated_versions"`
	RunningTasks     int `json:"running_tasks"`
	StuckTasks       int `json:"stuck_tasks"`
}

type Fleet struct {
	Machines         []FleetMachine `json:"machines"`
	Summary          FleetSummary   `json:"summary"`
	Total            int            `json:"total"`
	Truncated        bool           `json:"truncated"`
	MinClientVersion string         `json:"min_client_version,omitempty"`
}

// --- workspace config layer ---------------------------------------------------

// ConfigLayer — components/schemas/WorkspaceConfigLayer.
type ConfigLayer struct {
	LLMBaseURL   string         `json:"llm_base_url,omitempty"`
	LLMModel     string         `json:"llm_model,omitempty"`
	HasLLMAPIKey bool           `json:"has_llm_api_key"`
	MCPDefaults  map[string]any `json:"mcp_defaults"`
	UpdatedAt    *time.Time     `json:"updated_at"`
	UpdatedBy    *string        `json:"updated_by,omitempty"`
}

// PutConfigRequest — components/schemas/PutWorkspaceConfigRequest (и, при
// mcp_overrides вместо mcp_defaults, PutWorkspaceUserConfigOverrideRequest —
// то же тело по контракту).
type PutConfigRequest struct {
	LLMBaseURL  *string        `json:"llm_base_url"`
	LLMModel    *string        `json:"llm_model"`
	LLMAPIKey   *string        `json:"llm_api_key"`
	MCPDefaults map[string]any `json:"mcp_defaults"`
}

// UserConfigOverride — components/schemas/WorkspaceUserConfigOverride.
type UserConfigOverride struct {
	UserID       string         `json:"user_id"`
	LLMBaseURL   string         `json:"llm_base_url,omitempty"`
	LLMModel     string         `json:"llm_model,omitempty"`
	HasLLMAPIKey bool           `json:"has_llm_api_key"`
	MCPOverrides map[string]any `json:"mcp_overrides"`
	UpdatedAt    *time.Time     `json:"updated_at"`
	UpdatedBy    *string        `json:"updated_by,omitempty"`
}

// --- workspace MCP servers -----------------------------------------------------

// WorkspaceMcpServer — components/schemas/WorkspaceMcpServer (тот же вид
// используют и записи deployment-mcp-servers, показанные в контексте
// пространства, см. §12 контракта — но там форма DeploymentMcpServer с полем
// enabled, отдельный тип PlatformMcpServer выше).
type WorkspaceMcpServer struct {
	ID                  string               `json:"id"`
	WorkspaceID         string               `json:"workspace_id"`
	Name                string               `json:"name"`
	Transport           string               `json:"transport"`
	Source              string               `json:"source"`
	Enabled             *bool                `json:"enabled,omitempty"`
	CredentialSchema    []McpCredentialField `json:"credential_schema"`
	ProvidedCredentials []string             `json:"provided_credentials"`
	MissingCredentials  []string             `json:"missing_credentials"`
	CreatedAt           time.Time            `json:"created_at"`
	UpdatedAt           time.Time            `json:"updated_at"`
}

// WorkspaceMcpServerRequest — components/schemas/WorkspaceMcpServerRequest.
type WorkspaceMcpServerRequest struct {
	Name             string               `json:"name"`
	Config           map[string]any       `json:"config"`
	CredentialSchema []McpCredentialField `json:"credential_schema"`
}

// SetCredentialsRequest — components/schemas/SetWorkspaceMcpCredentialsRequest.
type SetCredentialsRequest struct {
	Values map[string]string `json:"values"`
}

// --- effective config / client secrets / llm health ---------------------------

type EffectiveLLM struct {
	BaseURL   string `json:"base_url,omitempty"`
	Model     string `json:"model,omitempty"`
	HasAPIKey bool   `json:"has_api_key"`
	Origin    string `json:"origin"`
	Locked    bool   `json:"locked"`
}

type EffectiveMcpEntry struct {
	Enabled bool     `json:"enabled"`
	HasEnv  bool     `json:"has_env"`
	EnvKeys []string `json:"env_keys"`
	Origin  string   `json:"origin"`
	Locked  bool     `json:"locked"`
}

// EffectiveConfigView — components/schemas/EffectiveConfigView.
type EffectiveConfigView struct {
	SchemaVersion   int                          `json:"schema_version"`
	LLM             EffectiveLLM                 `json:"llm"`
	MCP             map[string]EffectiveMcpEntry `json:"mcp"`
	RevokedPackages []string                     `json:"revoked_packages"`
}

// ClientSecretsLLM/ClientSecrets — components/schemas/DeploymentClientSecrets.
type ClientSecretsLLM struct {
	APIBase string `json:"api_base,omitempty"`
	Model   string `json:"model,omitempty"`
	APIKey  string `json:"api_key,omitempty"`
}

type ClientSecretsIntegration struct {
	URL string `json:"url"`
}

type ClientSecrets struct {
	LLM          *ClientSecretsLLM                   `json:"llm"`
	Integrations map[string]ClientSecretsIntegration `json:"integrations"`
}

// LlmHealth — components/schemas/LlmHealth.
type LlmHealth struct {
	Status    string    `json:"status"`
	CheckedAt time.Time `json:"checked_at"`
	LatencyMs int       `json:"latency_ms"`
}

// --- provisioning ---------------------------------------------------------------

type ProvisioningPackage struct {
	SchemaVersion int      `json:"schemaVersion"`
	Name          string   `json:"name"`
	Version       string   `json:"version"`
	Type          string   `json:"type"`
	Platform      string   `json:"platform"`
	SHA256        string   `json:"sha256"`
	Size          int64    `json:"size"`
	Requires      []string `json:"requires,omitempty"`
}

type ProvisioningUnavailablePackage struct {
	Key    string `json:"key"`
	Reason string `json:"reason"`
}

type ProvisioningManifest struct {
	SchemaVersion             int                              `json:"schemaVersion"`
	Packages                  []ProvisioningPackage            `json:"packages"`
	TotalBeforePlatformFilter int                              `json:"totalBeforePlatformFilter"`
	PlatformsAvailable        []string                         `json:"platformsAvailable"`
	RevokedPackages           []string                         `json:"revokedPackages"`
	UnavailablePackages       []ProvisioningUnavailablePackage `json:"unavailablePackages"`
}

type ProvisioningCatalog struct {
	SchemaVersion int                   `json:"schemaVersion"`
	Packages      []ProvisioningPackage `json:"packages"`
}

type ProvisioningPin struct {
	PackageName string    `json:"package_name"`
	PackageType string    `json:"package_type"`
	Version     string    `json:"version"`
	Enabled     bool      `json:"enabled"`
	UpdatedAt   time.Time `json:"updated_at"`
	UpdatedBy   *string   `json:"updated_by"`
}

type ProvisioningPinsList struct {
	Pins []ProvisioningPin `json:"pins"`
}

type PutPinRequest struct {
	PackageName string `json:"package_name"`
	PackageType string `json:"package_type"`
	Version     string `json:"version"`
	Enabled     bool   `json:"enabled"`
}

type PutPinsRequest struct {
	Pins []PutPinRequest `json:"pins"`
}
