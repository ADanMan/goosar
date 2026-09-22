package composio

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

const DefaultBaseURL = "https://backend.composio.dev/api/v3.1"

const DefaultUserAgent = "goosar-composio-go/0.1"

const DefaultTimeout = 30 * time.Second

type Options struct {
	APIKey string

	BaseURL string

	UserAgent string

	Timeout time.Duration

	HTTPClient *http.Client

	RetryCount int

	RetryWaitTime time.Duration
}

type Client struct {
	rc        *resty.Client
	baseURL   string
	apiKey    string
	userAgent string
}

func NewClient(opts Options) (*Client, error) {
	if strings.TrimSpace(opts.APIKey) == "" {
		return nil, errors.New("composio: APIKey is required")
	}

	baseURL := opts.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if _, err := url.Parse(baseURL); err != nil {
		return nil, fmt.Errorf("composio: invalid BaseURL %q: %w", baseURL, err)
	}
	baseURL = strings.TrimRight(baseURL, "/")

	ua := opts.UserAgent
	if ua == "" {
		ua = DefaultUserAgent
	}

	timeout := opts.Timeout
	switch {
	case timeout == 0:
		timeout = DefaultTimeout
	case timeout < 0:
		timeout = 0
	}

	rc := newRestyClient(opts.HTTPClient).
		SetBaseURL(baseURL).
		SetHeader("Content-Type", "application/json").
		SetHeader("Accept", "application/json").
		SetHeader("User-Agent", ua).
		SetHeader("x-api-key", opts.APIKey).
		SetTimeout(timeout)

	if opts.RetryCount > 0 {
		rc = rc.SetRetryCount(opts.RetryCount)
		if opts.RetryWaitTime > 0 {
			rc = rc.SetRetryWaitTime(opts.RetryWaitTime)
		}
	}

	return &Client{
		rc:        rc,
		baseURL:   baseURL,
		apiKey:    opts.APIKey,
		userAgent: ua,
	}, nil
}

func newRestyClient(hc *http.Client) *resty.Client {
	if hc != nil {
		return resty.NewWithClient(hc)
	}
	return resty.New()
}

func (c *Client) BaseURL() string { return c.baseURL }

func (c *Client) APIKeyHeader() map[string]string {
	return map[string]string{"x-api-key": c.apiKey}
}

func (c *Client) newRequest(ctx context.Context) *resty.Request {
	return c.rc.R().SetContext(ctx)
}

func (c *Client) do(req *resty.Request, method, path string, out any) error {
	if out != nil {
		req = req.SetResult(out)
	}
	resp, err := req.Execute(method, path)
	if err != nil {
		return fmt.Errorf("composio: %s %s: %w", method, path, err)
	}
	if resp.IsError() {
		return parseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}
