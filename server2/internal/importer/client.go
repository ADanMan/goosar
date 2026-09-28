package importer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// client — тонкий HTTP-клиент контракта docs/50-api-contract.yaml. Делает
// только GET-запросы (T-025: перенос читает исходный сервер исключительно
// через API контракта, не SQL-в-SQL); аутентификация — Authorization: Bearer
// <PAT/JWT>, как описано в components.securitySchemes.bearerAuth.
type client struct {
	baseURL string
	token   string
	hc      *http.Client
}

func newClient(baseURL, token string) *client {
	return &client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		hc:      &http.Client{Timeout: 30 * time.Second},
	}
}

// apiError — непустой ответ сервера с кодом ошибки контракта (schemas.Error).
type apiError struct {
	Method     string
	Path       string
	StatusCode int
	Body       string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("%s %s: HTTP %d: %s", e.Method, e.Path, e.StatusCode, e.Body)
}

// getJSON выполняет GET к path (уже включая query-строку) с опциональным
// заголовком X-Workspace-Slug и декодирует JSON-тело в out.
func (c *client) getJSON(ctx context.Context, path string, workspaceSlug string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("importer: построение запроса %s: %w", path, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if workspaceSlug != "" {
		req.Header.Set("X-Workspace-Slug", workspaceSlug)
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("importer: GET %s: %w", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("importer: чтение тела ответа %s: %w", path, err)
	}
	if resp.StatusCode >= 300 {
		return &apiError{Method: http.MethodGet, Path: path, StatusCode: resp.StatusCode, Body: string(body)}
	}
	if out == nil || len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("importer: разбор JSON %s: %w (тело: %.200s)", path, err, body)
	}
	return nil
}

func (c *client) listWorkspaces(ctx context.Context) ([]sourceWorkspace, error) {
	var out []sourceWorkspace
	if err := c.getJSON(ctx, "/api/workspaces", "", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *client) getMe(ctx context.Context) (*sourceUser, error) {
	var out sourceUser
	if err := c.getJSON(ctx, "/api/me", "", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *client) listMembers(ctx context.Context, workspaceID string) ([]sourceMember, error) {
	var out []sourceMember
	path := "/api/workspaces/" + url.PathEscape(workspaceID) + "/members"
	if err := c.getJSON(ctx, path, "", &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *client) listRuntimeProfiles(ctx context.Context, workspaceID string) ([]sourceRuntimeProfile, error) {
	var out sourceRuntimeProfilesResponse
	path := "/api/workspaces/" + url.PathEscape(workspaceID) + "/runtime-profiles"
	if err := c.getJSON(ctx, path, "", &out); err != nil {
		return nil, err
	}
	return out.RuntimeProfiles, nil
}

func (c *client) listRuntimes(ctx context.Context, slug string) ([]sourceRuntime, error) {
	var out []sourceRuntime
	if err := c.getJSON(ctx, "/api/runtimes", slug, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *client) listAgents(ctx context.Context, slug string) ([]sourceAgent, error) {
	var out []sourceAgent
	if err := c.getJSON(ctx, "/api/agents?include_archived=true", slug, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *client) listSkills(ctx context.Context, slug string) ([]sourceSkillSummary, error) {
	var out []sourceSkillSummary
	if err := c.getJSON(ctx, "/api/skills", slug, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *client) getSkill(ctx context.Context, slug, id string) (*sourceSkillWithFiles, error) {
	var out sourceSkillWithFiles
	path := "/api/skills/" + url.PathEscape(id)
	if err := c.getJSON(ctx, path, slug, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *client) listSquads(ctx context.Context, slug string) ([]sourceSquad, error) {
	var out []sourceSquad
	if err := c.getJSON(ctx, "/api/squads", slug, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *client) listSquadMembers(ctx context.Context, slug, squadID string) ([]sourceSquadMember, error) {
	var out []sourceSquadMember
	path := "/api/squads/" + url.PathEscape(squadID) + "/members"
	if err := c.getJSON(ctx, path, slug, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// listLabels — resource_type=agent/skill возвращает 404, если в воркспейсе не
// включён флаг ресурсных меток (contract: «доступны только при включённом
// флаге ресурсных меток для воркспейса, иначе 404») — это не ошибка переноса,
// просто в этом воркспейсе таких меток нет.
func (c *client) listLabels(ctx context.Context, slug, resourceType string) ([]sourceLabel, error) {
	var out sourceLabelsResponse
	path := "/api/labels?resource_type=" + url.QueryEscape(resourceType)
	if err := c.getJSON(ctx, path, slug, &out); err != nil {
		if ae, ok := err.(*apiError); ok && ae.StatusCode == http.StatusNotFound {
			return nil, nil
		}
		return nil, err
	}
	return out.Labels, nil
}

func (c *client) listProperties(ctx context.Context, slug string) ([]sourceProperty, error) {
	var out sourcePropertiesResponse
	if err := c.getJSON(ctx, "/api/properties?include_archived=true", slug, &out); err != nil {
		return nil, err
	}
	return out.Properties, nil
}

func (c *client) listProjects(ctx context.Context, slug string) ([]sourceProject, error) {
	var out sourceProjectsResponse
	if err := c.getJSON(ctx, "/api/projects", slug, &out); err != nil {
		return nil, err
	}
	return out.Projects, nil
}

func (c *client) listProjectResources(ctx context.Context, slug, projectID string) ([]sourceProjectResource, error) {
	var out sourceProjectResourcesResponse
	path := "/api/projects/" + url.PathEscape(projectID) + "/resources"
	if err := c.getJSON(ctx, path, slug, &out); err != nil {
		return nil, err
	}
	return out.Resources, nil
}

// listAllIssues пагинирует /api/issues (limit/offset, максимум limit=100) до
// исчерпания, включая под-задачи (они возвращаются тем же листингом — у
// contract нет отдельного include_sub_issues на этом эндпоинте, фильтрация
// top_level_only по умолчанию выключена).
func (c *client) listAllIssues(ctx context.Context, slug string) ([]sourceIssue, error) {
	const pageSize = 100
	var all []sourceIssue
	offset := 0
	for {
		var page sourceIssueListResponse
		path := fmt.Sprintf("/api/issues?limit=%d&offset=%d", pageSize, offset)
		if err := c.getJSON(ctx, path, slug, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Issues...)
		if len(page.Issues) < pageSize {
			break
		}
		offset += pageSize
	}
	return all, nil
}

func (c *client) listIssueComments(ctx context.Context, slug, issueID string) ([]sourceIssueComment, error) {
	var out []sourceIssueComment
	path := "/api/issues/" + url.PathEscape(issueID) + "/comments"
	if err := c.getJSON(ctx, path, slug, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *client) listIssueSubscribers(ctx context.Context, slug, issueID string) ([]sourceIssueSubscriber, error) {
	var out []sourceIssueSubscriber
	path := "/api/issues/" + url.PathEscape(issueID) + "/subscribers"
	if err := c.getJSON(ctx, path, slug, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// listIssuePullRequests: живой сервер отдаёт {"pull_requests": [...]}, а не
// плоский массив, как в docs/50-api-contract.yaml для этого эндпоинта —
// зафиксировано как расхождение в server2/docs/decisions.md.
func (c *client) listIssuePullRequests(ctx context.Context, slug, issueID string) ([]sourceIssuePullRequestLink, error) {
	var out sourceIssuePullRequestsResponse
	path := "/api/issues/" + url.PathEscape(issueID) + "/pull-requests"
	if err := c.getJSON(ctx, path, slug, &out); err != nil {
		return nil, err
	}
	return out.PullRequests, nil
}

func (c *client) listAutopilots(ctx context.Context, slug string) ([]sourceAutopilot, error) {
	var out sourceAutopilotsResponse
	if err := c.getJSON(ctx, "/api/autopilots", slug, &out); err != nil {
		return nil, err
	}
	return out.Autopilots, nil
}

// getAutopilot возвращает автопилот вместе с его триггерами. Отдельного
// GET-листинга триггеров контракт не выставляет (только create/update/delete
// по одному) — см. server2/docs/decisions.md, «Пробелы спецификации».
func (c *client) getAutopilot(ctx context.Context, slug, id string) (*sourceGetAutopilotResponse, error) {
	var out sourceGetAutopilotResponse
	path := "/api/autopilots/" + url.PathEscape(id)
	if err := c.getJSON(ctx, path, slug, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// listChatSessions — сессии, СОЗДАННЫЕ вызывающим PAT (контракт не
// предоставляет листинг по всему воркспейсу для владельца/админа — см.
// decisions.md).
func (c *client) listChatSessions(ctx context.Context, slug string) ([]sourceChatSession, error) {
	var out []sourceChatSession
	if err := c.getJSON(ctx, "/api/chat/sessions?status=all", slug, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// listAllChatMessages читает всю историю сессии через /messages/page —
// единственный GET-эндпоинт для чтения сообщений (POST /messages только
// отправляет новое). Курсор идёт назад по времени (каждая следующая
// страница — более старая), поэтому страницы собираются, а затем
// склеиваются в обратном порядке, чтобы получить сообщения от старых к
// новым по всей истории (внутри каждой страницы сервер уже отдаёт
// хронологический порядок).
func (c *client) listAllChatMessages(ctx context.Context, slug, sessionID string) ([]sourceChatMessage, error) {
	var pages [][]sourceChatMessage
	beforeCreatedAt, beforeID := "", ""
	for {
		q := url.Values{}
		q.Set("limit", "100")
		if beforeCreatedAt != "" && beforeID != "" {
			q.Set("before_created_at", beforeCreatedAt)
			q.Set("before_id", beforeID)
		}
		path := "/api/chat/sessions/" + url.PathEscape(sessionID) + "/messages/page?" + q.Encode()
		var page sourceChatMessagesPage
		if err := c.getJSON(ctx, path, slug, &page); err != nil {
			return nil, err
		}
		if len(page.Messages) > 0 {
			pages = append(pages, page.Messages)
		}
		if !page.HasMore || page.NextCursor == nil {
			break
		}
		createdAt, _ := page.NextCursor["created_at"].(string)
		id, _ := page.NextCursor["id"].(string)
		if createdAt == "" || id == "" {
			break
		}
		beforeCreatedAt, beforeID = createdAt, id
	}
	var all []sourceChatMessage
	for i := len(pages) - 1; i >= 0; i-- {
		all = append(all, pages[i]...)
	}
	return all, nil
}

func (c *client) listWorkspaceMcpServers(ctx context.Context, slug string) ([]sourceWorkspaceMcpServer, error) {
	var out []sourceWorkspaceMcpServer
	if err := c.getJSON(ctx, "/api/workspace-mcp-servers", slug, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *client) getWorkspaceConfig(ctx context.Context, slug string) (*sourceWorkspaceConfigLayer, error) {
	var out sourceWorkspaceConfigLayer
	if err := c.getJSON(ctx, "/api/workspace-config", slug, &out); err != nil {
		// owner PAT почти всегда имеет доступ, но конфиг воркспейса — не
		// обязательный для успеха всего импорта; 403/404 не должны валить его.
		if ae, ok := err.(*apiError); ok && (ae.StatusCode == http.StatusForbidden || ae.StatusCode == http.StatusNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &out, nil
}

// resolveWorkspace находит воркспейс вызывающего по slug или id среди тех,
// где он состоит участником (listWorkspaces) — единственный способ разрешить
// slug→id, который контракт даёт обычному PAT (нет отдельного GET по slug).
func (c *client) resolveWorkspace(ctx context.Context, slugOrID string) (*sourceWorkspace, error) {
	workspaces, err := c.listWorkspaces(ctx)
	if err != nil {
		return nil, err
	}
	for i := range workspaces {
		if workspaces[i].ID == slugOrID || workspaces[i].Slug == slugOrID {
			return &workspaces[i], nil
		}
	}
	return nil, fmt.Errorf("importer: воркспейс %q не найден среди доступных вызывающему (найдено %d)", slugOrID, len(workspaces))
}
