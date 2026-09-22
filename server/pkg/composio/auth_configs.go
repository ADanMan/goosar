package composio

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type AuthConfig struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`

	Toolkit    Toolkit `json:"toolkit"`
	AuthScheme string  `json:"auth_scheme,omitempty"`

	IsComposioManaged bool `json:"is_composio_managed"`

	Status        string `json:"status,omitempty"`
	CreatedAt     string `json:"created_at,omitempty"`
	LastUpdatedAt string `json:"last_updated_at,omitempty"`
}

type ListAuthConfigsRequest struct {
	ToolkitSlugs []string

	IsComposioManaged *bool

	ShowDisabled bool

	Search string

	Limit int

	Cursor string
}

type ListAuthConfigsResponse struct {
	Items      []AuthConfig `json:"items"`
	NextCursor string       `json:"next_cursor,omitempty"`
	TotalItems int          `json:"total_items,omitempty"`
}

func (c *Client) ListAuthConfigs(ctx context.Context, req ListAuthConfigsRequest) (*ListAuthConfigsResponse, error) {
	q := url.Values{}
	if len(req.ToolkitSlugs) > 0 {
		q.Set("toolkit_slug", strings.Join(req.ToolkitSlugs, ","))
	}
	if req.IsComposioManaged != nil {
		q.Set("is_composio_managed", strconv.FormatBool(*req.IsComposioManaged))
	}
	if req.ShowDisabled {
		q.Set("show_disabled", "true")
	}
	if req.Search != "" {
		q.Set("search", req.Search)
	}
	if req.Limit > 0 {
		q.Set("limit", strconv.Itoa(req.Limit))
	}
	if req.Cursor != "" {
		q.Set("cursor", req.Cursor)
	}

	path := "/auth_configs"
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}

	var out ListAuthConfigsResponse
	if err := c.do(c.newRequest(ctx), http.MethodGet, path, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
