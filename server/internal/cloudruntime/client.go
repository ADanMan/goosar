package cloudruntime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultTimeout      = 35 * time.Second
	maxResponseBodySize = 1 << 20
)

var (
	ErrDisabled       = errors.New("cloud runtime fleet URL is not configured")
	ErrInvalidBaseURL = errors.New("cloud runtime fleet URL is invalid")
)

type RequestRecorder interface {
	RecordCloudRuntimeRequest(op, status string, durationSeconds float64)
}

type Config struct {
	BaseURL    string
	Timeout    time.Duration
	HTTPClient *http.Client

	Recorder RequestRecorder
}

type Request struct {
	Method    string
	Path      string
	Query     url.Values
	Body      []byte
	UserID    string
	RequestID string

	Op string

	Headers http.Header
}

type Response struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

type Client struct {
	baseURL    string
	httpClient *http.Client
	recorder   RequestRecorder
}

func NewClient(cfg Config) *Client {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout}
	}
	return &Client{
		baseURL:    strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		httpClient: httpClient,
		recorder:   cfg.Recorder,
	}
}

func (c *Client) SetRecorder(r RequestRecorder) {
	if c == nil {
		return
	}
	c.recorder = r
}

func (c *Client) Enabled() bool {
	return c != nil && c.baseURL != ""
}

func (c *Client) Do(ctx context.Context, req Request) (*Response, error) {
	if c == nil || c.baseURL == "" {
		return nil, ErrDisabled
	}

	op := inferCloudRuntimeOp(req.Op, req.Method, req.Path)
	start := time.Now()
	resp, err := c.doInner(ctx, req)
	if c.recorder != nil {
		status := requestStatusBucket(resp, err)
		c.recorder.RecordCloudRuntimeRequest(op, status, time.Since(start).Seconds())
	}
	return resp, err
}

func (c *Client) doInner(ctx context.Context, req Request) (*Response, error) {
	base, err := url.Parse(c.baseURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return nil, fmt.Errorf("%w: %s", ErrInvalidBaseURL, c.baseURL)
	}
	if !strings.HasPrefix(req.Path, "/") {
		return nil, fmt.Errorf("cloud runtime path must start with /: %s", req.Path)
	}

	u := *base
	u.Path = strings.TrimRight(base.Path, "/") + req.Path
	u.RawQuery = req.Query.Encode()

	var body io.Reader
	if len(req.Body) > 0 {
		body = bytes.NewReader(req.Body)
	}
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, u.String(), body)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Accept", "application/json")
	if len(req.Body) > 0 {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	for k, vs := range req.Headers {

		canon := http.CanonicalHeaderKey(k)
		if canon == "X-User-Id" || canon == "X-Request-Id" {
			continue
		}
		httpReq.Header.Del(k)
		for _, v := range vs {
			httpReq.Header.Add(k, v)
		}
	}
	if req.UserID != "" {
		httpReq.Header.Set("X-User-ID", req.UserID)
	}
	if req.RequestID != "" {
		httpReq.Header.Set("X-Request-ID", req.RequestID)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodySize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxResponseBodySize {
		return nil, fmt.Errorf("cloud runtime response exceeds %d bytes", maxResponseBodySize)
	}
	return &Response{
		StatusCode: resp.StatusCode,
		Header:     resp.Header.Clone(),
		Body:       data,
	}, nil
}

func inferCloudRuntimeOp(op, method, path string) string {
	op = strings.ToLower(strings.TrimSpace(op))
	if op != "" {
		return op
	}
	switch {
	case strings.Contains(path, "/billing"):
		return "billing"
	case strings.Contains(path, "/gateway") || strings.Contains(path, "/proxy"):
		return "gateway"
	case strings.Contains(path, "/exec"):
		return "gateway"
	case strings.Contains(path, "/start") || strings.Contains(path, "/provision") || strings.Contains(path, "/nodes/create"):
		return "provision"
	case strings.Contains(path, "/stop") || strings.Contains(path, "/terminate") || strings.Contains(path, "/reboot"):
		return "terminate"
	case strings.Contains(path, "/status") || strings.Contains(path, "/health") || strings.Contains(path, "/ready"):
		return "status"
	case strings.Contains(path, "/nodes"):
		switch strings.ToUpper(strings.TrimSpace(method)) {
		case "POST":
			return "provision"
		case "DELETE":
			return "terminate"
		default:
			return "status"
		}
	}
	return "fleet"
}

func requestStatusBucket(resp *Response, err error) string {
	if err != nil {

		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline exceeded") {
			return "timeout"
		}
		return "error"
	}
	if resp == nil {
		return "error"
	}
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 400:
		return "ok"
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		return "4xx"
	case resp.StatusCode >= 500 && resp.StatusCode < 600:
		return "5xx"
	}
	return "error"
}
