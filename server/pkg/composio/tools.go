package composio

import (
	"context"
	"errors"
	"net/http"
	"net/url"
)

type ExecuteToolRequest struct {
	Arguments map[string]any `json:"arguments,omitempty"`

	ConnectedAccountID string `json:"connected_account_id,omitempty"`

	UserID string `json:"user_id,omitempty"`

	Version string `json:"version,omitempty"`

	AllowTracing bool `json:"allow_tracing,omitempty"`
}

type ExecuteToolResponse struct {
	Successful  bool           `json:"successful"`
	Data        map[string]any `json:"data,omitempty"`
	Error       string         `json:"error,omitempty"`
	LogID       string         `json:"log_id,omitempty"`
	SessionInfo map[string]any `json:"session_info,omitempty"`
}

func (c *Client) ExecuteTool(ctx context.Context, toolSlug string, req ExecuteToolRequest) (*ExecuteToolResponse, error) {
	if toolSlug == "" {
		return nil, errors.New("composio: ExecuteTool: toolSlug is required")
	}
	if req.ConnectedAccountID == "" && req.UserID == "" {
		return nil, errors.New("composio: ExecuteTool: either ConnectedAccountID or UserID must be set")
	}
	var out ExecuteToolResponse
	if err := c.do(c.newRequest(ctx).SetBody(req),
		http.MethodPost, "/tools/execute/"+url.PathEscape(toolSlug), &out); err != nil {
		return nil, err
	}
	return &out, nil
}
