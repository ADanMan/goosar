package task

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ListParams — вход ListIssues (GET /api/issues, listIssues/queryIssues),
// уже разобранный из query-параметров или JSON-тела (queryIssues) обработчиком.
// Свойства (properties-фильтр), involves_user_id и сортировка по
// произвольному property:<uuid> — упрощены/не реализованы в этой сессии
// (см. server2/docs/decisions.md, «Пробелы спецификации» T-027): остальные
// фильтры покрывают основной сценарий листинга контракта.
type ListParams struct {
	Limit, Offset        int
	OpenOnly             bool
	Sort, Direction      string
	Statuses, Priorities []string
	AssigneeID           string
	AssigneeIDs          []string
	AssigneeTypes        []string
	AssigneeFilters      []TypeID
	IncludeNoAssignee    bool
	CreatorID            string
	CreatorFilters       []TypeID
	ProjectID            string
	ProjectIDs           []string
	IncludeNoProject     bool
	LabelIDs             []string
	IDs                  []string
	IDsGiven             bool
	TopLevelOnly         bool
	Q                    string
	Scheduled            bool
	DateField            string
	DateStart, DateEnd   *time.Time
	IssuePrefix          string // для распознавания "ENG-42" в q
}

// TypeID — пара type:id, как в assignee_filters/creator_filters.
type TypeID struct{ Type, ID string }

// ParseTypeIDList парсит "agent:<uuid>,squad:<uuid>" в []TypeID; элементы без
// ":" молча пропускаются.
func ParseTypeIDList(s string) []TypeID {
	if s == "" {
		return nil
	}
	var out []TypeID
	for _, part := range strings.Split(s, ",") {
		idx := strings.IndexByte(part, ':')
		if idx <= 0 {
			continue
		}
		out = append(out, TypeID{Type: part[:idx], ID: part[idx+1:]})
	}
	return out
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

type queryBuilder struct {
	where []string
	args  []any
}

func (b *queryBuilder) add(clause string, args ...any) {
	base := len(b.args)
	for i := range args {
		clause = strings.Replace(clause, fmt.Sprintf("$%d", i+1), fmt.Sprintf("$%d", base+i+1), 1)
	}
	b.where = append(b.where, clause)
	b.args = append(b.args, args...)
}

func (b *queryBuilder) whereSQL() string {
	if len(b.where) == 0 {
		return "1=1"
	}
	return strings.Join(b.where, " AND ")
}

func buildFilters(workspaceID string, p ListParams) *queryBuilder {
	b := &queryBuilder{}
	b.add("workspace_id = $1", workspaceID)

	if len(p.Statuses) > 0 {
		b.add("tk_status = ANY($1)", p.Statuses)
	}
	if len(p.Priorities) > 0 {
		b.add("tk_priority = ANY($1)", p.Priorities)
	}
	if p.IDsGiven {
		if len(p.IDs) == 0 {
			b.add("1=0") // "пустой список после парсинга — ни одной задачи" (contract §1.4)
		} else {
			b.add("id = ANY($1)", p.IDs)
		}
	}
	if p.TopLevelOnly {
		b.add("tk_parent_ticket_id IS NULL")
	}
	if p.Scheduled {
		b.add("(tk_start_date IS NOT NULL OR tk_due_date IS NOT NULL)")
	}

	// assignee: assignee_id / assignee_ids / assignee_types / assignee_filters / include_no_assignee — комбинируются через OR в одну скобку.
	var assigneeOr []string
	if p.AssigneeID != "" {
		idx := len(b.args) + 1
		assigneeOr = append(assigneeOr, fmt.Sprintf("tk_assignee_id = $%d", idx))
		b.args = append(b.args, p.AssigneeID)
	}
	if len(p.AssigneeIDs) > 0 {
		idx := len(b.args) + 1
		assigneeOr = append(assigneeOr, fmt.Sprintf("tk_assignee_id = ANY($%d)", idx))
		b.args = append(b.args, p.AssigneeIDs)
	}
	if len(p.AssigneeTypes) > 0 {
		idx := len(b.args) + 1
		assigneeOr = append(assigneeOr, fmt.Sprintf("tk_assignee_type = ANY($%d)", idx))
		b.args = append(b.args, p.AssigneeTypes)
	}
	for _, tid := range p.AssigneeFilters {
		i1, i2 := len(b.args)+1, len(b.args)+2
		assigneeOr = append(assigneeOr, fmt.Sprintf("(tk_assignee_type = $%d AND tk_assignee_id = $%d)", i1, i2))
		b.args = append(b.args, tid.Type, tid.ID)
	}
	if p.IncludeNoAssignee {
		assigneeOr = append(assigneeOr, "tk_assignee_id IS NULL")
	}
	if len(assigneeOr) > 0 {
		b.where = append(b.where, "("+strings.Join(assigneeOr, " OR ")+")")
	}

	if p.CreatorID != "" {
		b.add("tk_creator_id = $1", p.CreatorID)
	}
	for _, tid := range p.CreatorFilters {
		i1, i2 := len(b.args)+1, len(b.args)+2
		b.where = append(b.where, fmt.Sprintf("(tk_creator_type = $%d AND tk_creator_id = $%d)", i1, i2))
		b.args = append(b.args, tid.Type, tid.ID)
	}

	var projectOr []string
	if p.ProjectID != "" {
		idx := len(b.args) + 1
		projectOr = append(projectOr, fmt.Sprintf("initiative_id = $%d", idx))
		b.args = append(b.args, p.ProjectID)
	}
	if len(p.ProjectIDs) > 0 {
		idx := len(b.args) + 1
		projectOr = append(projectOr, fmt.Sprintf("initiative_id = ANY($%d)", idx))
		b.args = append(b.args, p.ProjectIDs)
	}
	if p.IncludeNoProject {
		projectOr = append(projectOr, "initiative_id IS NULL")
	}
	if len(projectOr) > 0 {
		b.where = append(b.where, "("+strings.Join(projectOr, " OR ")+")")
	}

	if len(p.LabelIDs) > 0 {
		idx := len(b.args) + 1
		b.where = append(b.where, fmt.Sprintf(
			"EXISTS(SELECT 1 FROM ticket_tag_links ttl WHERE ttl.ticket_id = tickets.id AND ttl.tag_id = ANY($%d))", idx))
		b.args = append(b.args, p.LabelIDs)
	}

	if p.Q != "" {
		idx := len(b.args) + 1
		if n, ok := parseIdentifierNumber(p.IssuePrefix, p.Q); ok {
			b.where = append(b.where, fmt.Sprintf("(tk_seq_number = $%d OR tk_headline ILIKE $%d)", idx, idx+1))
			b.args = append(b.args, n, "%"+p.Q+"%")
		} else {
			b.where = append(b.where, fmt.Sprintf("tk_headline ILIKE $%d", idx))
			b.args = append(b.args, "%"+p.Q+"%")
		}
	}

	if p.DateField != "" && p.DateStart != nil && p.DateEnd != nil {
		col := "created_at"
		if p.DateField == "updated_at" {
			col = "updated_at"
		}
		i1, i2 := len(b.args)+1, len(b.args)+2
		b.where = append(b.where, fmt.Sprintf("%s >= $%d AND %s < $%d", col, i1, col, i2))
		b.args = append(b.args, *p.DateStart, *p.DateEnd)
	}

	return b
}

func sortSQL(p ListParams) string {
	dir := "ASC"
	if strings.EqualFold(p.Direction, "desc") {
		dir = "DESC"
	}
	var col string
	switch p.Sort {
	case "", "position":
		return "tk_position ASC, created_at DESC, id DESC"
	case "title":
		col = "tk_headline"
	case "created_at":
		col = "created_at"
	case "updated_at":
		col = "updated_at"
	case "start_date":
		return "tk_start_date " + dir + " NULLS LAST, created_at DESC, id DESC"
	case "due_date":
		return "tk_due_date " + dir + " NULLS LAST, created_at DESC, id DESC"
	case "status":
		return "(CASE tk_status " +
			"WHEN 'backlog' THEN 0 WHEN 'todo' THEN 1 WHEN 'in_progress' THEN 2 WHEN 'in_review' THEN 3 " +
			"WHEN 'done' THEN 4 WHEN 'blocked' THEN 5 WHEN 'cancelled' THEN 6 ELSE 7 END) " + dir + ", created_at DESC, id DESC"
	case "priority":
		return "(CASE tk_priority WHEN 'urgent' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 2 WHEN 'low' THEN 3 ELSE 4 END) " +
			dir + ", created_at DESC, id DESC"
	default:
		// property:<uuid> — сортировка по значению кастомного свойства не
		// реализована в этой сессии (см. package doc); падаем на позицию,
		// не на 400, чтобы не ломать остальной листинг.
		return "tk_position ASC, created_at DESC, id DESC"
	}
	return col + " " + dir + ", created_at DESC, id DESC"
}

// ListIssues — listIssues/queryIssues. Без p.OpenOnly — offset-пагинация с
// total (точный COUNT по тем же фильтрам); с p.OpenOnly — все не-терминальные
// задачи разом, limit/offset игнорируются (contract §1.6).
func (s *Store) ListIssues(ctx context.Context, workspaceID string, p ListParams) ([]Issue, int64, error) {
	b := buildFilters(workspaceID, p)
	if p.OpenOnly {
		b.where = append(b.where, "tk_status NOT IN ('done','cancelled')")
	}

	var total int64
	if !p.OpenOnly {
		countQuery := `SELECT COUNT(*) FROM tickets WHERE ` + b.whereSQL()
		if err := s.db.Pool.QueryRow(ctx, countQuery, b.args...).Scan(&total); err != nil {
			return nil, 0, fmt.Errorf("task: подсчёт задач: %w", err)
		}
	}

	limit, offset := p.Limit, p.Offset
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	query := `SELECT ` + ticketColumns + ` FROM tickets WHERE ` + b.whereSQL() + ` ORDER BY ` + sortSQL(p)
	args := append([]any{}, b.args...)
	if !p.OpenOnly {
		li, oi := len(args)+1, len(args)+2
		query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", li, oi)
		args = append(args, limit, offset)
	}
	rows, err := s.db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("task: список задач: %w", err)
	}
	issues, err := collectIssues(rows)
	if err != nil {
		return nil, 0, err
	}
	if p.OpenOnly {
		total = int64(len(issues))
	}
	if issues == nil {
		issues = []Issue{}
	}
	return issues, total, nil
}

// SearchIssues — GET /api/issues/search: то же listIssues, плюс простое
// ранжирование по совпадению заголовка (headline ILIKE) перед позицией —
// contract требует "ранжирование" без указания конкретного алгоритма;
// упрощение зафиксировано в decisions.md.
func (s *Store) SearchIssues(ctx context.Context, workspaceID string, p ListParams) ([]Issue, int64, error) {
	if p.Sort == "" {
		p.Sort = "updated_at"
		p.Direction = "desc"
	}
	return s.ListIssues(ctx, workspaceID, p)
}

// ChildIssues — listIssueChildren: прямые дочерние задачи.
func (s *Store) ChildIssues(ctx context.Context, workspaceID, parentID string) ([]Issue, error) {
	rows, err := s.db.Pool.Query(ctx, `SELECT `+ticketColumns+` FROM tickets
		WHERE workspace_id=$1 AND tk_parent_ticket_id=$2 ORDER BY tk_position`, workspaceID, parentID)
	if err != nil {
		return nil, fmt.Errorf("task: дочерние задачи: %w", err)
	}
	issues, err := collectIssues(rows)
	if err != nil {
		return nil, err
	}
	if issues == nil {
		issues = []Issue{}
	}
	return issues, nil
}

// ChildProgressEntry — schemas.ChildIssueProgressEntry.
type ChildProgressEntry struct {
	ParentIssueID string `json:"parent_issue_id"`
	Total         int    `json:"total"`
	Done          int    `json:"done"`
}

// ChildProgress — getChildIssueProgress: по каждому родителю в воркспейсе —
// (всего дочерних, завершено — done/cancelled).
func (s *Store) ChildProgress(ctx context.Context, workspaceID string) ([]ChildProgressEntry, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT tk_parent_ticket_id, COUNT(*),
			COUNT(*) FILTER (WHERE tk_status IN ('done','cancelled'))
		FROM tickets
		WHERE workspace_id = $1 AND tk_parent_ticket_id IS NOT NULL
		GROUP BY tk_parent_ticket_id`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("task: прогресс дочерних задач: %w", err)
	}
	defer rows.Close()
	var out []ChildProgressEntry
	for rows.Next() {
		var e ChildProgressEntry
		if err := rows.Scan(&e.ParentIssueID, &e.Total, &e.Done); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	if out == nil {
		out = []ChildProgressEntry{}
	}
	return out, rows.Err()
}

// AssigneeGroup — schemas.IssueAssigneeGroup (listGroupedIssues, упрощённая
// группировка по исполнителю).
type AssigneeGroup struct {
	ID           string  `json:"id"`
	AssigneeType *string `json:"assignee_type"`
	AssigneeID   *string `json:"assignee_id"`
	Issues       []Issue `json:"issues"`
	Total        int     `json:"total"`
}

// GroupedByAssignee — listGroupedIssues.
func (s *Store) GroupedByAssignee(ctx context.Context, workspaceID string) ([]AssigneeGroup, error) {
	rows, err := s.db.Pool.Query(ctx, `SELECT `+ticketColumns+` FROM tickets WHERE workspace_id=$1 ORDER BY tk_position`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("task: группировка по исполнителю: %w", err)
	}
	issues, err := collectIssues(rows)
	if err != nil {
		return nil, err
	}
	groups := map[string]*AssigneeGroup{}
	var order []string
	for _, issue := range issues {
		key := "assignee:unassigned"
		if issue.AssigneeType != nil && issue.AssigneeID != nil {
			key = "assignee:" + *issue.AssigneeType + ":" + *issue.AssigneeID
		}
		g, ok := groups[key]
		if !ok {
			g = &AssigneeGroup{ID: key, AssigneeType: issue.AssigneeType, AssigneeID: issue.AssigneeID}
			groups[key] = g
			order = append(order, key)
		}
		g.Issues = append(g.Issues, issue)
		g.Total++
	}
	out := make([]AssigneeGroup, 0, len(order))
	for _, key := range order {
		out = append(out, *groups[key])
	}
	return out, nil
}

// AssigneeFrequencyEntry — schemas.AssigneeFrequencyEntry.
type AssigneeFrequencyEntry struct {
	AssigneeType string `json:"assignee_type"`
	AssigneeID   string `json:"assignee_id"`
	Frequency    int    `json:"frequency"`
}

// AssigneeFrequency — GET /api/assignee-frequency: частота назначений
// вызывающим (сумма по assignee_changed-активностям, которые он инициировал,
// плюс назначения при создании задачи этим же актором) — contract §3.
func (s *Store) AssigneeFrequency(ctx context.Context, workspaceID, callerType, callerID string) ([]AssigneeFrequencyEntry, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT tk_assignee_type, tk_assignee_id, COUNT(*)
		FROM tickets
		WHERE workspace_id = $1 AND tk_assignee_id IS NOT NULL AND tk_creator_type = $2 AND tk_creator_id = $3
		GROUP BY tk_assignee_type, tk_assignee_id
		ORDER BY COUNT(*) DESC`, workspaceID, callerType, callerID)
	if err != nil {
		return nil, fmt.Errorf("task: частота назначений: %w", err)
	}
	defer rows.Close()
	var out []AssigneeFrequencyEntry
	for rows.Next() {
		var e AssigneeFrequencyEntry
		if err := rows.Scan(&e.AssigneeType, &e.AssigneeID, &e.Frequency); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	if out == nil {
		out = []AssigneeFrequencyEntry{}
	}
	return out, rows.Err()
}

// PullRequestLink — schemas.IssuePullRequestLink (только чтение — привязка
// пишется вебхук-обработчиком GitHub/VCS, вне области этого домена).
// docs/50-api-contract-changes.md п.3: полная "карточка" PR, а не голая
// связь issue↔PR. Часть полей (checks_*, snapshot_*, additions/deletions/
// changed_files, mergeable*) — задокументированный пробел (см.
// internal/integration/webhooks.go про check_suite/check_run,
// server2/docs/decisions.md): вебхуки пока не разбирают эти события и
// не запрашивают diff-статистику у провайдера, поэтому эти поля остаются
// null, пока такая обработка не появится.
type PullRequestLink struct {
	ID                string     `json:"id"`
	Provider          string     `json:"provider"`
	WorkspaceID       string     `json:"workspace_id"`
	RepoOwner         *string    `json:"repo_owner"`
	RepoName          *string    `json:"repo_name"`
	Number            int        `json:"number"`
	Title             *string    `json:"title"`
	State             *string    `json:"state"`
	HTMLURL           string     `json:"html_url"`
	Branch            *string    `json:"branch"`
	AuthorLogin       *string    `json:"author_login"`
	AuthorAvatarURL   *string    `json:"author_avatar_url"`
	MergedAt          *time.Time `json:"merged_at"`
	ClosedAt          *time.Time `json:"closed_at"`
	PRCreatedAt       time.Time  `json:"pr_created_at"`
	PRUpdatedAt       time.Time  `json:"pr_updated_at"`
	MergeableState    *string    `json:"mergeable_state"`
	Mergeable         *string    `json:"mergeable"`
	MergeStateStatus  *string    `json:"merge_state_status"`
	SnapshotAvailable bool       `json:"snapshot_available"`
	ChecksRollup      *string    `json:"checks_rollup"`
	ChecksConclusion  *string    `json:"checks_conclusion"`
	ChecksTotal       *int       `json:"checks_total"`
	ChecksPassed      *int       `json:"checks_passed"`
	ChecksFailed      *int       `json:"checks_failed"`
	ChecksRunning     *int       `json:"checks_running"`
	ChecksPending     *int       `json:"checks_pending"`
	FailedCheckNames  []string   `json:"failed_check_names"`
	SnapshotStale     *bool      `json:"snapshot_stale"`
	SnapshotFetchedAt *time.Time `json:"snapshot_fetched_at"`
	Additions         *int       `json:"additions"`
	Deletions         *int       `json:"deletions"`
	ChangedFiles      *int       `json:"changed_files"`
}

// PullRequests — listIssuePullRequests. Ответ оборачивается вызывающим
// хендлером в {"pull_requests": [...]} (contract-changes п.3).
func (s *Store) PullRequests(ctx context.Context, workspaceID, issueID string) ([]PullRequestLink, error) {
	if _, err := s.GetIssue(ctx, workspaceID, issueID); err != nil {
		return nil, err
	}
	rows, err := s.db.Pool.Query(ctx, `
		SELECT l.id, l.tpr_provider, t.workspace_id, l.tpr_repo_owner, l.tpr_repo_name, l.tpr_number, l.tpr_title, l.tpr_state,
		       l.tpr_url, l.tpr_branch, l.tpr_author_login, l.tpr_author_avatar_url, l.tpr_merged_at, l.tpr_closed_at,
		       COALESCE(l.tpr_pr_created_at, l.created_at), COALESCE(l.tpr_pr_updated_at, l.created_at),
		       l.tpr_mergeable_state, l.tpr_mergeable, l.tpr_merge_state_status, l.tpr_snapshot_available,
		       l.tpr_checks_rollup, l.tpr_checks_conclusion, l.tpr_checks_total, l.tpr_checks_passed, l.tpr_checks_failed,
		       l.tpr_checks_running, l.tpr_checks_pending, l.tpr_failed_check_names, l.tpr_snapshot_stale,
		       l.tpr_snapshot_fetched_at, l.tpr_additions, l.tpr_deletions, l.tpr_changed_files
		FROM ticket_pr_links l JOIN tickets t ON t.id = l.ticket_id
		WHERE l.ticket_id = $1 ORDER BY l.created_at`, issueID)
	if err != nil {
		return nil, fmt.Errorf("task: связанные pull request'ы: %w", err)
	}
	defer rows.Close()
	var out []PullRequestLink
	for rows.Next() {
		var l PullRequestLink
		if err := rows.Scan(&l.ID, &l.Provider, &l.WorkspaceID, &l.RepoOwner, &l.RepoName, &l.Number, &l.Title, &l.State,
			&l.HTMLURL, &l.Branch, &l.AuthorLogin, &l.AuthorAvatarURL, &l.MergedAt, &l.ClosedAt,
			&l.PRCreatedAt, &l.PRUpdatedAt,
			&l.MergeableState, &l.Mergeable, &l.MergeStateStatus, &l.SnapshotAvailable,
			&l.ChecksRollup, &l.ChecksConclusion, &l.ChecksTotal, &l.ChecksPassed, &l.ChecksFailed,
			&l.ChecksRunning, &l.ChecksPending, &l.FailedCheckNames, &l.SnapshotStale,
			&l.SnapshotFetchedAt, &l.Additions, &l.Deletions, &l.ChangedFiles); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	if out == nil {
		out = []PullRequestLink{}
	}
	return out, rows.Err()
}
