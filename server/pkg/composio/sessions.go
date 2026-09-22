package composio

import (
	"context"
	"errors"
	"net/http"
)

type CreateSessionRequest struct {
	UserID            string             `json:"user_id"`
	Toolkits          map[string]any     `json:"toolkits,omitempty"`
	AuthConfigs       map[string]any     `json:"auth_configs,omitempty"`
	ConnectedAccounts map[string]any     `json:"connected_accounts,omitempty"`
	ManageConnections *ManageConnections `json:"manage_connections,omitempty"`
	Tools             map[string]any     `json:"tools,omitempty"`
	Tags              any                `json:"tags,omitempty"`
	Workbench         map[string]any     `json:"workbench,omitempty"`
	MultiAccount      map[string]any     `json:"multi_account,omitempty"`
	Preload           map[string]any     `json:"preload,omitempty"`
	Search            map[string]any     `json:"search,omitempty"`
	Execute           map[string]any     `json:"execute,omitempty"`
	Experimental      map[string]any     `json:"experimental,omitempty"`
}

type ManageConnections struct {
	Enable                   *bool  `json:"enable,omitempty"`
	CallbackURL              string `json:"callback_url,omitempty"`
	EnableWaitForConnections *bool  `json:"enable_wait_for_connections,omitempty"`
	EnableConnectionRemoval  *bool  `json:"enable_connection_removal,omitempty"`
}

type MCPDescriptor struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

type CreateSessionResponse struct {
	SessionID       string           `json:"session_id"`
	MCP             MCPDescriptor    `json:"mcp"`
	ToolRouterTools []string         `json:"tool_router_tools,omitempty"`
	Config          map[string]any   `json:"config,omitempty"`
	ConfigVersion   int              `json:"config_version,omitempty"`
	Experimental    map[string]any   `json:"experimental,omitempty"`
	Warnings        []SessionWarning `json:"warnings,omitempty"`
}

type SessionWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (c *Client) CreateSession(ctx context.Context, req CreateSessionRequest) (*CreateSessionResponse, error) {
	if req.UserID == "" {
		return nil, errors.New("composio: CreateSession: UserID is required")
	}
	var out CreateSessionResponse
	if err := c.do(c.newRequest(ctx).SetBody(req), http.MethodPost, "/tool_router/session", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) MCPAuthHeaders() map[string]string {
	return c.APIKeyHeader()
}
