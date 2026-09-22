package daemon

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/adanman/goosar/server/pkg/protocol"
)

type requestError struct {
	Method     string
	Path       string
	StatusCode int
	Body       string
}

func (e *requestError) Error() string {

	if description, ok := describeInterceptedResponse(e.Body); ok {
		return fmt.Sprintf("%s %s returned %d: %s", e.Method, e.Path, e.StatusCode, description)
	}
	return fmt.Sprintf("%s %s returned %d: %s", e.Method, e.Path, e.StatusCode, e.Body)
}

func isWorkspaceNotFoundError(err error) bool {
	var reqErr *requestError
	if !errors.As(err, &reqErr) {
		return false
	}
	if reqErr.StatusCode != http.StatusNotFound {
		return false
	}
	return strings.Contains(strings.ToLower(reqErr.Body), "workspace not found")
}

func isTaskNotFoundError(err error) bool {
	var reqErr *requestError
	if !errors.As(err, &reqErr) {
		return false
	}
	if reqErr.StatusCode != http.StatusNotFound {
		return false
	}
	return strings.Contains(strings.ToLower(reqErr.Body), "task not found")
}

func isUnauthorizedError(err error) bool {
	var reqErr *requestError
	if !errors.As(err, &reqErr) {
		return false
	}
	return reqErr.StatusCode == http.StatusUnauthorized
}

func isRuntimeNotFoundError(err error) bool {
	var reqErr *requestError
	if !errors.As(err, &reqErr) {
		return false
	}
	if reqErr.StatusCode != http.StatusNotFound {
		return false
	}
	return strings.Contains(strings.ToLower(reqErr.Body), "runtime not found")
}

type Client struct {
	baseURL string
	token   string
	client  *http.Client

	bundleClient *http.Client

	platform string
	version  string
	os       string

	workspaceMu                    sync.Mutex
	workspaceETag                  string
	workspaceCache                 []WorkspaceInfo
	workspaceCacheValid            bool
	legacyWorkspaceEndpointEnabled bool
	issueGCBatchMu                 sync.Mutex
	legacyIssueGCBatchEnabled      bool
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL:      baseURL,
		client:       &http.Client{Timeout: 30 * time.Second, Transport: cloneDefaultTransport()},
		bundleClient: &http.Client{},
		platform:     "daemon",
		os:           normalizeGOOS(runtime.GOOS),
	}
}

func cloneDefaultTransport() http.RoundTripper {
	if transport, ok := http.DefaultTransport.(*http.Transport); ok {
		return transport.Clone()
	}
	return http.DefaultTransport
}

func (c *Client) CloseIdleConnections() {
	if c == nil || c.client == nil {
		return
	}
	c.client.CloseIdleConnections()
}

func normalizeGOOS(goos string) string {
	switch goos {
	case "darwin":
		return "macos"
	case "windows":
		return "windows"
	case "linux":
		return "linux"
	default:
		return goos
	}
}

func (c *Client) SetVersion(v string) {
	c.version = v
}

func (c *Client) setIdentityHeaders(req *http.Request) {
	if c.platform != "" {
		req.Header.Set("X-Client-Platform", c.platform)
	}
	if c.version != "" {
		req.Header.Set("X-Client-Version", c.version)
	}
	if c.os != "" {
		req.Header.Set("X-Client-OS", c.os)
	}
	req.Header.Set("X-Client-Capabilities", daemonClientCapabilities())

	req.Header.Set("User-Agent", daemonUserAgent(nil, c.version, c.os))
}

func daemonClientCapabilities() string {
	return strings.Join([]string{
		protocol.DaemonCapabilitySkillBundlesV1,
		protocol.DaemonCapabilityCoalescedCommentsV1,
		protocol.DaemonCapabilityRPCV1,
	}, ",")
}

func (c *Client) SetToken(token string) {
	c.token = token
}

func (c *Client) Token() string {
	return c.token
}

func (c *Client) ClaimTask(ctx context.Context, runtimeID string) (*Task, error) {
	var resp struct {
		Task *Task `json:"task"`
	}
	if err := c.postJSON(ctx, fmt.Sprintf("/api/daemon/runtimes/%s/tasks/claim", runtimeID), map[string]any{}, &resp); err != nil {
		return nil, err
	}
	return resp.Task, nil
}

const batchClaimRequestTimeout = 5 * time.Second

func (c *Client) ClaimTasks(ctx context.Context, daemonID string, runtimeIDs []string, maxTasks int) ([]*Task, error) {
	reqCtx, cancel := context.WithTimeout(ctx, batchClaimRequestTimeout)
	defer cancel()
	var resp struct {
		Tasks []*Task `json:"tasks"`
	}
	if err := c.postJSON(reqCtx, "/api/daemon/tasks/claim", map[string]any{
		"daemon_id":   daemonID,
		"runtime_ids": runtimeIDs,
		"max_tasks":   maxTasks,
	}, &resp); err != nil {
		return nil, err
	}
	return resp.Tasks, nil
}

func isBatchClaimUnsupported(err error) bool {
	var reqErr *requestError
	if !errors.As(err, &reqErr) {
		return false
	}
	return reqErr.StatusCode == http.StatusNotFound
}

func (c *Client) claimTasksLegacy(ctx context.Context, runtimeIDs []string, maxTasks int) ([]*Task, error) {
	if maxTasks <= 0 {
		return nil, nil
	}
	out := make([]*Task, 0, maxTasks)
	for _, rid := range runtimeIDs {
		if len(out) >= maxTasks {
			break
		}
		task, err := c.ClaimTask(ctx, rid)
		if err != nil {
			if len(out) == 0 {
				return nil, err
			}
			return out, nil
		}
		if task != nil {
			out = append(out, task)
		}
	}
	return out, nil
}

func (c *Client) ResolveSkillBundle(ctx context.Context, runtimeID, taskID string, ref SkillRefData) (SkillData, error) {
	var resp struct {
		Bundles []SkillData `json:"bundles"`

		ContentEncoding string `json:"content_encoding"`
	}
	path := fmt.Sprintf("/api/daemon/runtimes/%s/tasks/%s/skill-bundles/resolve", runtimeID, taskID)
	if err := c.postJSONViaWithRetry(ctx, c.bundleClient, path, map[string]any{
		"skills": []SkillRefData{ref},

		"content_encoding": skillBundleEncodingBase64,
	}, &resp, skillBundleResolveRetrySchedule); err != nil {
		return SkillData{}, err
	}
	if len(resp.Bundles) != 1 {
		return SkillData{}, fmt.Errorf("resolve skill bundle: expected 1 bundle, got %d", len(resp.Bundles))
	}
	return decodeSkillBundle(resp.Bundles[0], resp.ContentEncoding)
}

const skillBundleEncodingBase64 = "base64"

func decodeSkillBundle(пакет SkillData, encoding string) (SkillData, error) {
	switch encoding {
	case "":
		return пакет, nil
	case skillBundleEncodingBase64:
	default:
		return SkillData{}, fmt.Errorf("resolve skill bundle: unknown content encoding %q", encoding)
	}

	content, err := base64.StdEncoding.DecodeString(пакет.Content)
	if err != nil {
		return SkillData{}, fmt.Errorf("resolve skill bundle: decode content: %w", err)
	}
	decoded := пакет
	decoded.Content = string(content)
	if len(пакет.Files) > 0 {
		files := make([]SkillFileData, len(пакет.Files))
		for i, file := range пакет.Files {
			raw, err := base64.StdEncoding.DecodeString(file.Content)
			if err != nil {
				return SkillData{}, fmt.Errorf("resolve skill bundle: decode file %s: %w", file.Path, err)
			}
			copied := file
			copied.Content = string(raw)
			files[i] = copied
		}
		decoded.Files = files
	}
	return decoded, nil
}

func (c *Client) ExtendTaskPrepareLease(ctx context.Context, runtimeID, taskID string) error {
	return c.postJSON(ctx, fmt.Sprintf("/api/daemon/runtimes/%s/tasks/%s/prepare-lease", runtimeID, taskID), map[string]any{}, nil)
}

func (c *Client) StartTask(ctx context.Context, taskID string) error {
	return c.postJSON(ctx, fmt.Sprintf("/api/daemon/tasks/%s/start", taskID), map[string]any{}, nil)
}

func (c *Client) MarkTaskWaitingLocalDirectory(ctx context.Context, taskID, reason string) error {
	return c.postJSON(ctx, fmt.Sprintf("/api/daemon/tasks/%s/wait-local-directory", taskID), map[string]any{
		"reason": reason,
	}, nil)
}

func (c *Client) AckTaskCancelled(ctx context.Context, taskID string) error {
	return c.postJSON(ctx, fmt.Sprintf("/api/daemon/tasks/%s/cancel-ack", taskID), map[string]any{}, nil)
}

func (c *Client) ReportProgress(ctx context.Context, taskID, summary string, step, total int) error {
	return c.postJSON(ctx, fmt.Sprintf("/api/daemon/tasks/%s/progress", taskID), map[string]any{
		"summary": summary,
		"step":    step,
		"total":   total,
	}, nil)
}

type TaskMessageData struct {
	Seq     int            `json:"seq"`
	Type    string         `json:"type"`
	Tool    string         `json:"tool,omitempty"`
	Content string         `json:"content,omitempty"`
	Input   map[string]any `json:"input,omitempty"`
	Output  string         `json:"output,omitempty"`
}

func (c *Client) ReportTaskMessages(ctx context.Context, taskID string, messages []TaskMessageData) error {
	return c.postJSON(ctx, fmt.Sprintf("/api/daemon/tasks/%s/messages", taskID), map[string]any{
		"messages": messages,
	}, nil)
}

func (c *Client) CompleteTask(ctx context.Context, taskID, output, branchName, sessionID, workDir string, sessionRolloutMissing bool) error {
	body := map[string]any{"output": output}
	if branchName != "" {
		body["branch_name"] = branchName
	}
	if sessionID != "" {
		body["session_id"] = sessionID
	}
	if workDir != "" {
		body["work_dir"] = workDir
	}
	if sessionRolloutMissing {
		body["session_rollout_missing"] = true
	}
	return c.postJSONWithRetry(ctx, fmt.Sprintf("/api/daemon/tasks/%s/complete", taskID), body, nil, defaultTerminalRetrySchedule)
}

func (c *Client) ReportTaskUsage(ctx context.Context, taskID string, usage []TaskUsageEntry) error {
	if len(usage) == 0 {
		return nil
	}
	return c.postJSON(ctx, fmt.Sprintf("/api/daemon/tasks/%s/usage", taskID), map[string]any{
		"usage": usage,
	}, nil)
}

func (c *Client) FailTask(ctx context.Context, taskID, errMsg, sessionID, workDir, failureReason string, sessionRolloutMissing bool) error {
	body := map[string]any{"error": errMsg}
	if sessionID != "" {
		body["session_id"] = sessionID
	}
	if workDir != "" {
		body["work_dir"] = workDir
	}
	if failureReason != "" {
		body["failure_reason"] = failureReason
	}
	if sessionRolloutMissing {
		body["session_rollout_missing"] = true
	}
	return c.postJSONWithRetry(ctx, fmt.Sprintf("/api/daemon/tasks/%s/fail", taskID), body, nil, defaultTerminalRetrySchedule)
}

func (c *Client) PinTaskSession(ctx context.Context, taskID, sessionID, workDir string) error {
	if sessionID == "" && workDir == "" {
		return nil
	}
	body := map[string]any{}
	if sessionID != "" {
		body["session_id"] = sessionID
	}
	if workDir != "" {
		body["work_dir"] = workDir
	}
	return c.postJSON(ctx, fmt.Sprintf("/api/daemon/tasks/%s/session", taskID), body, nil)
}

func (c *Client) RecoverOrphans(ctx context.Context, runtimeID string) error {
	return c.postJSON(ctx, fmt.Sprintf("/api/daemon/runtimes/%s/recover-orphans", runtimeID), map[string]any{}, nil)
}

func (c *Client) GetTaskStatus(ctx context.Context, taskID string) (string, error) {
	var resp struct {
		Status string `json:"status"`
	}
	if err := c.getJSON(ctx, fmt.Sprintf("/api/daemon/tasks/%s/status", taskID), &resp); err != nil {
		return "", err
	}
	return resp.Status, nil
}

type (
	HeartbeatResponse       = protocol.DaemonHeartbeatAckPayload
	PendingUpdate           = protocol.DaemonHeartbeatPendingUpdate
	PendingModelList        = protocol.DaemonHeartbeatPendingModelList
	PendingLocalSkills      = protocol.DaemonHeartbeatPendingLocalSkills
	PendingLocalSkillImport = protocol.DaemonHeartbeatPendingLocalSkillImport
)

func (c *Client) ServerDeliveryProfile(ctx context.Context) (string, error) {
	var resp struct {
		DeliveryProfile string `json:"delivery_profile"`
	}
	if err := c.getJSON(ctx, "/api/config", &resp); err != nil {
		return "", err
	}
	return resp.DeliveryProfile, nil
}

func (c *Client) SendHeartbeat(ctx context.Context, runtimeID string) (*HeartbeatResponse, error) {
	var resp HeartbeatResponse
	if err := c.postJSON(ctx, "/api/daemon/heartbeat", map[string]any{
		"runtime_id":            runtimeID,
		"supports_batch_import": true,
	}, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) ReportUpdateResult(ctx context.Context, runtimeID, updateID string, result map[string]any) error {
	return c.postJSON(ctx, fmt.Sprintf("/api/daemon/runtimes/%s/update/%s/result", runtimeID, updateID), result, nil)
}

func (c *Client) ReportModelListResult(ctx context.Context, runtimeID, requestID string, result map[string]any) error {
	return c.postJSON(ctx, fmt.Sprintf("/api/daemon/runtimes/%s/models/%s/result", runtimeID, requestID), result, nil)
}

func (c *Client) ReportLocalSkillListResult(ctx context.Context, runtimeID, requestID string, result map[string]any) error {
	return c.postJSON(ctx, fmt.Sprintf("/api/daemon/runtimes/%s/local-skills/%s/result", runtimeID, requestID), result, nil)
}

func (c *Client) ReportLocalSkillImportResult(ctx context.Context, runtimeID, requestID string, result map[string]any) error {
	return c.postJSON(ctx, fmt.Sprintf("/api/daemon/runtimes/%s/local-skills/import/%s/result", runtimeID, requestID), result, nil)
}

type WorkspaceInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type RenewTokenResponse struct {
	ExpiresAt string `json:"expires_at"`
	Renewed   bool   `json:"renewed"`
}

func (c *Client) RenewToken(ctx context.Context) (*RenewTokenResponse, error) {
	var resp RenewTokenResponse
	if err := c.postJSON(ctx, "/api/tokens/current/renew", map[string]any{}, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) ListWorkspaces(ctx context.Context) ([]WorkspaceInfo, error) {
	c.workspaceMu.Lock()
	defer c.workspaceMu.Unlock()

	if c.legacyWorkspaceEndpointEnabled {
		return c.listLegacyWorkspaces(ctx)
	}

	const path = "/api/daemon/workspaces"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	c.setIdentityHeaders(req)
	if c.workspaceETag != "" {
		req.Header.Set("If-None-Match", c.workspaceETag)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		_, _ = io.Copy(io.Discard, resp.Body)
		c.legacyWorkspaceEndpointEnabled = true
		c.workspaceETag = ""
		c.workspaceCache = nil
		c.workspaceCacheValid = false
		return c.listLegacyWorkspaces(ctx)
	}
	if resp.StatusCode == http.StatusNotModified {
		if !c.workspaceCacheValid {
			return nil, fmt.Errorf("GET %s returned 304 without a cached workspace set", path)
		}
		return append([]WorkspaceInfo(nil), c.workspaceCache...), nil
	}
	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, &requestError{Method: http.MethodGet, Path: path, StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(data))}
	}

	var workspaces []WorkspaceInfo
	if err := json.NewDecoder(resp.Body).Decode(&workspaces); err != nil {
		return nil, err
	}
	c.workspaceETag = resp.Header.Get("ETag")
	c.workspaceCache = append([]WorkspaceInfo(nil), workspaces...)
	c.workspaceCacheValid = true
	return append([]WorkspaceInfo(nil), workspaces...), nil
}

func (c *Client) listLegacyWorkspaces(ctx context.Context) ([]WorkspaceInfo, error) {
	var workspaces []WorkspaceInfo
	if err := c.getJSON(ctx, "/api/workspaces", &workspaces); err != nil {
		return nil, err
	}
	return workspaces, nil
}

func (c *Client) usesLegacyWorkspaceEndpoint() bool {
	c.workspaceMu.Lock()
	defer c.workspaceMu.Unlock()
	return c.legacyWorkspaceEndpointEnabled
}

type IssueGCStatus struct {
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updated_at"`
}

type IssueGCCheckResult struct {
	ID        string    `json:"id"`
	Found     bool      `json:"found"`
	Status    string    `json:"status,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
	Err       error     `json:"-"`
}

type issueGCBatchResponse struct {
	Issues []IssueGCCheckResult `json:"issues"`
}

func isIssueGCBatchUnsupported(err error) bool {
	var reqErr *requestError
	return errors.As(err, &reqErr) &&
		reqErr.StatusCode == http.StatusNotFound &&
		strings.TrimSpace(reqErr.Body) == "404 page not found"
}

func (c *Client) GetIssueGCChecks(ctx context.Context, workspaceID string, issueIDs []string) (map[string]IssueGCCheckResult, error) {
	c.issueGCBatchMu.Lock()
	defer c.issueGCBatchMu.Unlock()

	if c.legacyIssueGCBatchEnabled {
		return c.getLegacyIssueGCChecks(ctx, issueIDs), nil
	}

	path := fmt.Sprintf("/api/daemon/workspaces/%s/issues/gc-check", workspaceID)
	var resp issueGCBatchResponse
	err := c.postJSON(ctx, path, map[string]any{"issue_ids": issueIDs}, &resp)
	if err != nil {
		if !isIssueGCBatchUnsupported(err) {
			return nil, err
		}
		c.legacyIssueGCBatchEnabled = true
		return c.getLegacyIssueGCChecks(ctx, issueIDs), nil
	}

	results := make(map[string]IssueGCCheckResult, len(resp.Issues))
	for _, result := range resp.Issues {
		results[result.ID] = result
	}
	return results, nil
}

func (c *Client) getLegacyIssueGCChecks(ctx context.Context, issueIDs []string) map[string]IssueGCCheckResult {
	results := make(map[string]IssueGCCheckResult, len(issueIDs))
	for _, issueID := range issueIDs {
		status, err := c.GetIssueGCCheck(ctx, issueID)
		if err != nil {
			var reqErr *requestError
			if errors.As(err, &reqErr) && reqErr.StatusCode == http.StatusNotFound {
				results[issueID] = IssueGCCheckResult{ID: issueID, Found: false}
			} else {
				results[issueID] = IssueGCCheckResult{ID: issueID, Err: err}
			}
			continue
		}
		results[issueID] = IssueGCCheckResult{
			ID:        issueID,
			Found:     true,
			Status:    status.Status,
			UpdatedAt: status.UpdatedAt,
		}
	}
	return results
}

func (c *Client) GetIssueGCCheck(ctx context.Context, issueID string) (*IssueGCStatus, error) {
	var resp IssueGCStatus
	if err := c.getJSON(ctx, fmt.Sprintf("/api/daemon/issues/%s/gc-check", issueID), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

type ChatSessionGCStatus struct {
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (c *Client) GetChatSessionGCCheck(ctx context.Context, sessionID string) (*ChatSessionGCStatus, error) {
	var resp ChatSessionGCStatus
	if err := c.getJSON(ctx, fmt.Sprintf("/api/daemon/chat-sessions/%s/gc-check", sessionID), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

type AutopilotRunGCStatus struct {
	Status      string    `json:"status"`
	CompletedAt time.Time `json:"completed_at"`
}

func (c *Client) GetAutopilotRunGCCheck(ctx context.Context, runID string) (*AutopilotRunGCStatus, error) {
	var resp AutopilotRunGCStatus
	if err := c.getJSON(ctx, fmt.Sprintf("/api/daemon/autopilot-runs/%s/gc-check", runID), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

type TaskGCStatus struct {
	Status      string    `json:"status"`
	CompletedAt time.Time `json:"completed_at"`
}

func (c *Client) GetTaskGCCheck(ctx context.Context, taskID string) (*TaskGCStatus, error) {
	var resp TaskGCStatus
	if err := c.getJSON(ctx, fmt.Sprintf("/api/daemon/tasks/%s/gc-check", taskID), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) Deregister(ctx context.Context, runtimeIDs []string) error {
	return c.postJSON(ctx, "/api/daemon/deregister", map[string]any{
		"runtime_ids": runtimeIDs,
	}, nil)
}

type RegisterResponse struct {
	Runtimes     []Runtime       `json:"runtimes"`
	Repos        []RepoData      `json:"repos"`
	ReposVersion string          `json:"repos_version"`
	Settings     json.RawMessage `json:"settings,omitempty"`
}

func (c *Client) Register(ctx context.Context, req map[string]any) (*RegisterResponse, error) {
	var resp RegisterResponse
	if err := c.postJSON(ctx, "/api/daemon/register", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

type WorkspaceReposResponse struct {
	WorkspaceID  string          `json:"workspace_id"`
	Repos        []RepoData      `json:"repos"`
	ReposVersion string          `json:"repos_version"`
	Settings     json.RawMessage `json:"settings,omitempty"`
}

func (c *Client) GetWorkspaceRepos(ctx context.Context, workspaceID string) (*WorkspaceReposResponse, error) {
	var resp WorkspaceReposResponse
	if err := c.getJSON(ctx, fmt.Sprintf("/api/daemon/workspaces/%s/repos", workspaceID), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

type RuntimeProfile struct {
	ID             string   `json:"id"`
	WorkspaceID    string   `json:"workspace_id"`
	DisplayName    string   `json:"display_name"`
	ProtocolFamily string   `json:"protocol_family"`
	CommandName    string   `json:"command_name"`
	Description    *string  `json:"description"`
	FixedArgs      []string `json:"fixed_args"`
	Visibility     string   `json:"visibility"`
	Enabled        bool     `json:"enabled"`
}

type RuntimeProfilesResponse struct {
	WorkspaceID     string           `json:"workspace_id"`
	RuntimeProfiles []RuntimeProfile `json:"runtime_profiles"`
}

func (c *Client) GetRuntimeProfiles(ctx context.Context, workspaceID string) (*RuntimeProfilesResponse, error) {
	var resp RuntimeProfilesResponse
	if err := c.getJSON(ctx, fmt.Sprintf("/api/daemon/workspaces/%s/runtime-profiles", workspaceID), &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

var defaultTerminalRetrySchedule = []time.Duration{
	4 * time.Second,
	8 * time.Second,
	16 * time.Second,
	32 * time.Second,
	64 * time.Second,
}

var skillBundleResolveRetrySchedule = []time.Duration{
	500 * time.Millisecond,
	2 * time.Second,
}

type retrySleepFunc func(ctx context.Context, d time.Duration) error

var retrySleepHook atomic.Pointer[retrySleepFunc]

func retrySleep(ctx context.Context, d time.Duration) error {
	if hook := retrySleepHook.Load(); hook != nil {
		return (*hook)(ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func isTransientError(err error) bool {
	if err == nil {
		return false
	}
	var reqErr *requestError
	if errors.As(err, &reqErr) {
		if reqErr.StatusCode >= 500 {
			return true
		}
		if reqErr.StatusCode == http.StatusRequestTimeout || reqErr.StatusCode == http.StatusTooManyRequests {
			return true
		}
		return false
	}
	return true
}

func (c *Client) postJSONWithRetry(ctx context.Context, path string, reqBody any, respBody any, schedule []time.Duration) error {
	return c.postJSONViaWithRetry(ctx, c.client, path, reqBody, respBody, schedule)
}

func (c *Client) postJSONViaWithRetry(ctx context.Context, httpClient *http.Client, path string, reqBody any, respBody any, schedule []time.Duration) error {
	var lastErr error
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			if lastErr != nil {
				return lastErr
			}
			return err
		}
		err := c.postJSONVia(ctx, httpClient, path, reqBody, respBody)
		if err == nil {
			return nil
		}
		lastErr = err
		if !isTransientError(err) {
			return err
		}
		if attempt >= len(schedule) {
			return err
		}
		if sleepErr := retrySleep(ctx, schedule[attempt]); sleepErr != nil {
			return err
		}
	}
}

func (c *Client) postJSON(ctx context.Context, path string, reqBody any, respBody any) error {
	return c.postJSONVia(ctx, c.client, path, reqBody, respBody)
}

func (c *Client) postJSONVia(ctx context.Context, httpClient *http.Client, path string, reqBody any, respBody any) error {
	var body io.Reader
	if reqBody != nil {
		data, err := json.Marshal(reqBody)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	c.setIdentityHeaders(req)

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &requestError{Method: http.MethodPost, Path: path, StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(data))}
	}
	if respBody == nil {
		io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(respBody)
}

func (c *Client) getJSON(ctx context.Context, path string, respBody any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	c.setIdentityHeaders(req)

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &requestError{Method: http.MethodGet, Path: path, StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(data))}
	}
	if respBody == nil {
		io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(respBody)
}
