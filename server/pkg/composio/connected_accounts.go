package composio

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
)

type CreateLinkRequest struct {
	AuthConfigID string `json:"auth_config_id"`

	UserID string `json:"user_id"`

	CallbackURL string `json:"callback_url,omitempty"`

	Alias string `json:"alias,omitempty"`

	ConnectionData map[string]any `json:"connection_data,omitempty"`
}

type CreateLinkResponse struct {
	LinkToken          string `json:"link_token"`
	RedirectURL        string `json:"redirect_url"`
	ExpiresAt          string `json:"expires_at"`
	ConnectedAccountID string `json:"connected_account_id"`
}

func (c *Client) CreateLink(ctx context.Context, req CreateLinkRequest) (*CreateLinkResponse, error) {
	if req.AuthConfigID == "" {
		return nil, errors.New("composio: CreateLink: AuthConfigID is required")
	}
	if req.UserID == "" {
		return nil, errors.New("composio: CreateLink: UserID is required")
	}
	var out CreateLinkResponse
	if err := c.do(c.newRequest(ctx).SetBody(req), http.MethodPost, "/connected_accounts/link", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

type ListConnectedAccountsRequest struct {
	UserIDs             []string
	ToolkitSlugs        []string
	AuthConfigIDs       []string
	ConnectedAccountIDs []string
	Statuses            []string
	OrderBy             string
	OrderDirection      string
	AccountType         string
	Limit               int
	Cursor              string
}

type ConnectedAccount struct {
	ID           string         `json:"id"`
	UserID       string         `json:"user_id"`
	AuthConfigID string         `json:"auth_config_id"`
	AuthConfig   AuthConfigRef  `json:"auth_config"`
	Toolkit      Toolkit        `json:"toolkit"`
	Status       string         `json:"status"`
	StatusReason string         `json:"status_reason,omitempty"`
	CreatedAt    string         `json:"created_at,omitempty"`
	UpdatedAt    string         `json:"updated_at,omitempty"`
	LastUsedAt   string         `json:"last_used_at,omitempty"`
	Extra        map[string]any `json:"-"`
}

type AuthConfigRef struct {
	ID                string `json:"id"`
	AuthScheme        string `json:"auth_scheme,omitempty"`
	IsComposioManaged bool   `json:"is_composio_managed,omitempty"`
	IsDisabled        bool   `json:"is_disabled,omitempty"`
}

type ListConnectedAccountsResponse struct {
	Items      []ConnectedAccount `json:"items"`
	NextCursor string             `json:"next_cursor,omitempty"`
	TotalItems int                `json:"total_items,omitempty"`
}

func (c *Client) ListConnectedAccounts(ctx context.Context, req ListConnectedAccountsRequest) (*ListConnectedAccountsResponse, error) {
	q := url.Values{}
	for _, v := range req.UserIDs {
		if v != "" {
			q.Add("user_ids", v)
		}
	}
	for _, v := range req.ToolkitSlugs {
		if v != "" {
			q.Add("toolkit_slugs", v)
		}
	}
	for _, v := range req.AuthConfigIDs {
		if v != "" {
			q.Add("auth_config_ids", v)
		}
	}
	for _, v := range req.ConnectedAccountIDs {
		if v != "" {
			q.Add("connected_account_ids", v)
		}
	}
	for _, v := range req.Statuses {
		if v != "" {
			q.Add("statuses", v)
		}
	}
	if req.OrderBy != "" {
		q.Set("order_by", req.OrderBy)
	}
	if req.OrderDirection != "" {
		q.Set("order_direction", req.OrderDirection)
	}
	if req.AccountType != "" {
		q.Set("account_type", req.AccountType)
	}
	if req.Limit > 0 {
		q.Set("limit", strconv.Itoa(req.Limit))
	}
	if req.Cursor != "" {
		q.Set("cursor", req.Cursor)
	}

	path := "/connected_accounts"
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}

	var out ListConnectedAccountsResponse
	if err := c.do(c.newRequest(ctx), http.MethodGet, path, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) RevokeConnection(ctx context.Context, connectedAccountID string) error {
	if connectedAccountID == "" {
		return errors.New("composio: RevokeConnection: connectedAccountID is required")
	}
	return c.do(c.newRequest(ctx),
		http.MethodPost, "/connected_accounts/"+url.PathEscape(connectedAccountID)+"/revoke", nil)
}

func (c *Client) DeleteConnectedAccount(ctx context.Context, connectedAccountID string) error {
	if connectedAccountID == "" {
		return errors.New("composio: DeleteConnectedAccount: connectedAccountID is required")
	}
	err := c.do(c.newRequest(ctx),
		http.MethodDelete, "/connected_accounts/"+url.PathEscape(connectedAccountID), nil)
	if err == nil {
		return nil
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.IsNotFound() {
		return nil
	}
	return err
}
