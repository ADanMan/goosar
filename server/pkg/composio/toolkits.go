package composio

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
)

type Toolkit struct {
	Slug        string         `json:"slug"`
	Name        string         `json:"name,omitempty"`
	LogoURL     string         `json:"logo,omitempty"`
	Description string         `json:"description,omitempty"`
	Categories  []string       `json:"categories,omitempty"`
	AuthSchemes []string       `json:"auth_schemes,omitempty"`
	Meta        map[string]any `json:"meta,omitempty"`
}

type ListToolkitsRequest struct {
	Category string
	Limit    int
	Cursor   string

	SortBy string
}

type ListToolkitsResponse struct {
	Items      []Toolkit `json:"items"`
	NextCursor string    `json:"next_cursor,omitempty"`
	TotalItems int       `json:"total_items,omitempty"`
}

func (c *Client) ListToolkits(ctx context.Context, req ListToolkitsRequest) (*ListToolkitsResponse, error) {
	q := url.Values{}
	if req.Category != "" {
		q.Set("category", req.Category)
	}
	if req.Limit > 0 {
		q.Set("limit", strconv.Itoa(req.Limit))
	}
	if req.Cursor != "" {
		q.Set("cursor", req.Cursor)
	}
	if req.SortBy != "" {
		q.Set("sort_by", req.SortBy)
	}
	path := "/toolkits"
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var out ListToolkitsResponse
	if err := c.do(c.newRequest(ctx), http.MethodGet, path, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetToolkit(ctx context.Context, slug string) (*Toolkit, error) {
	if slug == "" {
		return nil, errors.New("composio: GetToolkit: slug is required")
	}
	var out Toolkit
	if err := c.do(c.newRequest(ctx),
		http.MethodGet, "/toolkits/"+url.PathEscape(slug), &out); err != nil {
		return nil, err
	}
	return &out, nil
}
