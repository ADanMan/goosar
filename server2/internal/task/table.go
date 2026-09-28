package task

// table.go — тег IssueTable контракта (`/api/issues/table/{groups,rows,facets}`)
// и `/api/issues/children` (plural — набор родителей через query, тег Issues),
// не реализованные предыдущими сессиями T-027 (см. server2/docs/decisions.md,
// раздел «T-027 (task/dispatch/realtime)», пункт 3). Курсорная пагинация
// listIssueTableRows реализована без хранимого состояния сервера: курсор несёт
// офсет и отпечаток (fingerprint) запроса, посчитанный детерминированно из
// разобранного тела запроса (той же нормализованной Go-структуры, что и сам
// запрос) — если следующий вызов передаёт другой query/group/group_key/parent_id,
// отпечаток не совпадёт и курсор будет отклонён 400, что и требует контракт
// ("курсор... не годится для другого набора фильтров"). Реальная гарантия
// REPEATABLE READ между вызовами groups/rows контрактом описана как "в рамках
// одного запроса пользователя" — то есть один HTTP-запрос, не серия courier-
// вызовов; здесь каждый вызов — отдельная транзакция чтения, что для курсорной
// пагинации по неизменяемым между вызовами данным (типичный сценарий доски)
// эквивалентно. Задокументировано в server2/docs/decisions.md.

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/adanman/goosar/server2/internal/httpapi"
)

// --- JSON-формы контракта (components/schemas/IssueTable*) -----------------

type tableActorRef struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type tableDateFilter struct {
	Field string `json:"field"`
	Start string `json:"start"`
	End   string `json:"end"`
}

type tableScope struct {
	Kind          string          `json:"kind"`
	AssigneeTypes []string        `json:"assignee_types"`
	ProjectID     *string         `json:"project_id"`
	Actor         *tableActorRef  `json:"actor"`
	Relation      string          `json:"relation"`
}

type tableFilters struct {
	Statuses          []string            `json:"statuses"`
	Priorities        []string            `json:"priorities"`
	Assignees         []tableActorRef     `json:"assignees"`
	IncludeNoAssignee bool                `json:"include_no_assignee"`
	Creators          []tableActorRef     `json:"creators"`
	ProjectIDs        []string            `json:"project_ids"`
	IncludeNoProject  bool                `json:"include_no_project"`
	LabelIDs          []string            `json:"label_ids"`
	Properties        map[string][]string `json:"properties"`
	Date              *tableDateFilter    `json:"date"`
	WorkingOnly       *bool               `json:"working_only"`
	WorkingIssueIDs   []string            `json:"working_issue_ids"`
	IncludeSubIssues  bool                `json:"include_sub_issues"`
}

type tableSort struct {
	Field     string `json:"field"`
	Direction string `json:"direction"`
}

type tableQuery struct {
	Scope   *tableScope   `json:"scope"`
	Filters *tableFilters `json:"filters"`
	Search  string        `json:"search"`
	Sort    *tableSort    `json:"sort"`
}

type tableGroupSpec struct {
	Kind            string   `json:"kind"` // none|status|assignee|project|parent|compound|property
	PropertyID      *string  `json:"property_id"`
	IncludeEmpty    bool     `json:"include_empty"`
	Primary         string   `json:"primary"`
	Secondary       string   `json:"secondary"`
	SecondaryValues []string `json:"secondary_values"`
}

type tablePage struct {
	Limit  int     `json:"limit"`
	Cursor *string `json:"cursor"`
}

type tableGroupsRequest struct {
	Query *tableQuery     `json:"query"`
	Group *tableGroupSpec `json:"group"`
	Page  *tablePage      `json:"page"`
}

type tableRowsRequest struct {
	Query     *tableQuery     `json:"query"`
	Group     *tableGroupSpec `json:"group"`
	GroupKey  *string         `json:"group_key"`
	Hierarchy *struct {
		Enabled bool `json:"enabled"`
	} `json:"hierarchy"`
	ParentID *string    `json:"parent_id"`
	Page     *tablePage `json:"page"`
}

type tableFacetSpec struct {
	Kind       string  `json:"kind"`
	PropertyID *string `json:"property_id"`
}

type tableFacetsRequest struct {
	Query        *tableQuery      `json:"query"`
	Facets       []tableFacetSpec `json:"facets"`
	IncludeTotal bool             `json:"include_total"`
}

// --- курсор ------------------------------------------------------------------

type tableCursor struct {
	Offset int    `json:"offset"`
	FP     string `json:"fp"`
}

func fingerprint(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return fmt.Sprintf("%x", sum)
}

func encodeCursor(offset int, fp string) string {
	b, _ := json.Marshal(tableCursor{Offset: offset, FP: fp})
	return base64.RawURLEncoding.EncodeToString(b)
}

// decodeCursor разбирает курсор и проверяет его отпечаток против wantFP;
// пустой cursor — офсет 0, без ошибки (первая страница).
func decodeCursor(cursor *string, wantFP string) (offset int, err error) {
	if cursor == nil || *cursor == "" {
		return 0, nil
	}
	raw, decErr := base64.RawURLEncoding.DecodeString(*cursor)
	if decErr != nil {
		return 0, errBadRequest("invalid cursor")
	}
	var c tableCursor
	if err := json.Unmarshal(raw, &c); err != nil {
		return 0, errBadRequest("invalid cursor")
	}
	if c.FP != wantFP {
		return 0, errBadRequest("cursor is not valid for this query (query_fingerprint mismatch)")
	}
	return c.Offset, nil
}

// --- построение WHERE из IssueTableQuery -------------------------------------

// tableWhere транслирует IssueTableQuery (scope+filters+search) в SQL-условие
// поверх tickets, переиспользуя queryBuilder из filters.go. callerType/callerID —
// вызывающий (contract scope.kind=my/assignee/creator без явного actor —
// подразумевает самого вызывающего).
func (d *Deps) tableWhere(ctx context.Context, workspaceID string, q *tableQuery, callerType, callerID string) *queryBuilder {
	b := &queryBuilder{}
	b.add("workspace_id = $1", workspaceID)

	if q == nil {
		return b
	}
	if q.Scope != nil {
		d.applyTableScope(b, q.Scope, callerType, callerID)
	}
	if q.Filters != nil {
		d.applyTableFilters(ctx, b, q.Filters)
	}
	if strings.TrimSpace(q.Search) != "" {
		idx := len(b.args) + 1
		b.where = append(b.where, fmt.Sprintf("tk_headline ILIKE $%d", idx))
		b.args = append(b.args, "%"+q.Search+"%")
	}
	return b
}

func (d *Deps) applyTableScope(b *queryBuilder, s *tableScope, callerType, callerID string) {
	switch s.Kind {
	case "project":
		if s.ProjectID != nil {
			b.add("initiative_id = $1", *s.ProjectID)
		}
	case "assignee":
		if s.Actor != nil {
			i1, i2 := len(b.args)+1, len(b.args)+2
			b.where = append(b.where, fmt.Sprintf("(tk_assignee_type = $%d AND tk_assignee_id = $%d)", i1, i2))
			b.args = append(b.args, s.Actor.Type, s.Actor.ID)
		}
	case "creator":
		if s.Actor != nil {
			i1, i2 := len(b.args)+1, len(b.args)+2
			b.where = append(b.where, fmt.Sprintf("(tk_creator_type = $%d AND tk_creator_id = $%d)", i1, i2))
			b.args = append(b.args, s.Actor.Type, s.Actor.ID)
		}
	case "my":
		clause := d.myRelationClause(b, s.Relation, callerType, callerID)
		if clause != "" {
			b.where = append(b.where, clause)
		}
	case "workspace", "":
		// без дополнительного условия
	}
}

// myRelationClause — "assigned"/"created"/"involved"/"any" для scope.kind=my
// (contract: relation по умолчанию — "any", если не задана).
func (d *Deps) myRelationClause(b *queryBuilder, relation, callerType, callerID string) string {
	assignedIdx1, assignedIdx2 := len(b.args)+1, len(b.args)+2
	assigned := fmt.Sprintf("(tk_assignee_type = $%d AND tk_assignee_id = $%d)", assignedIdx1, assignedIdx2)
	assignedArgs := []any{callerType, callerID}

	createdIdx1, createdIdx2 := assignedIdx1+2, assignedIdx2+2
	created := fmt.Sprintf("(tk_creator_type = $%d AND tk_creator_id = $%d)", createdIdx1, createdIdx2)
	createdArgs := []any{callerType, callerID}

	involvedIdx := createdIdx2 + 1
	involved := fmt.Sprintf(`(
		(tk_assignee_type = 'agent' AND EXISTS(SELECT 1 FROM operatives o WHERE o.id = tk_assignee_id AND o.op_owner_account_id = $%d))
		OR (tk_assignee_type = 'squad' AND EXISTS(
			SELECT 1 FROM crews c JOIN operatives o2 ON o2.id = c.crew_leader_id
			WHERE c.id = tk_assignee_id AND c.crew_leader_type = 'agent' AND o2.op_owner_account_id = $%d))
	)`, involvedIdx, involvedIdx)
	involvedArgs := []any{callerID}

	switch relation {
	case "assigned":
		b.args = append(b.args, assignedArgs...)
		return assigned
	case "created":
		b.args = append(b.args, createdArgs...)
		return created
	case "involved":
		b.args = append(b.args, involvedArgs...)
		return involved
	default: // "any" — контракт: relation по умолчанию охватывает всё перечисленное
		b.args = append(b.args, assignedArgs...)
		b.args = append(b.args, createdArgs...)
		b.args = append(b.args, involvedArgs...)
		return "(" + assigned + " OR " + created + " OR " + involved + ")"
	}
}

func (d *Deps) applyTableFilters(ctx context.Context, b *queryBuilder, f *tableFilters) {
	if len(f.Statuses) > 0 {
		b.add("tk_status = ANY($1)", f.Statuses)
	}
	if len(f.Priorities) > 0 {
		b.add("tk_priority = ANY($1)", f.Priorities)
	}
	var assigneeOr []string
	for _, a := range f.Assignees {
		i1, i2 := len(b.args)+1, len(b.args)+2
		assigneeOr = append(assigneeOr, fmt.Sprintf("(tk_assignee_type = $%d AND tk_assignee_id = $%d)", i1, i2))
		b.args = append(b.args, a.Type, a.ID)
	}
	if f.IncludeNoAssignee {
		assigneeOr = append(assigneeOr, "tk_assignee_id IS NULL")
	}
	if len(assigneeOr) > 0 {
		b.where = append(b.where, "("+strings.Join(assigneeOr, " OR ")+")")
	}
	for _, c := range f.Creators {
		i1, i2 := len(b.args)+1, len(b.args)+2
		b.where = append(b.where, fmt.Sprintf("(tk_creator_type = $%d AND tk_creator_id = $%d)", i1, i2))
		b.args = append(b.args, c.Type, c.ID)
	}
	var projectOr []string
	if len(f.ProjectIDs) > 0 {
		idx := len(b.args) + 1
		projectOr = append(projectOr, fmt.Sprintf("initiative_id = ANY($%d)", idx))
		b.args = append(b.args, f.ProjectIDs)
	}
	if f.IncludeNoProject {
		projectOr = append(projectOr, "initiative_id IS NULL")
	}
	if len(projectOr) > 0 {
		b.where = append(b.where, "("+strings.Join(projectOr, " OR ")+")")
	}
	if len(f.LabelIDs) > 0 {
		idx := len(b.args) + 1
		b.where = append(b.where, fmt.Sprintf(
			"EXISTS(SELECT 1 FROM ticket_tag_links ttl WHERE ttl.ticket_id = tickets.id AND ttl.tag_id = ANY($%d))", idx))
		b.args = append(b.args, f.LabelIDs)
	}
	for propID, values := range f.Properties {
		if len(values) == 0 {
			continue
		}
		i1, i2 := len(b.args)+1, len(b.args)+2
		b.where = append(b.where, fmt.Sprintf("tk_custom_field_values->>$%d = ANY($%d)", i1, i2))
		b.args = append(b.args, propID, values)
	}
	if f.Date != nil && f.Date.Field != "" && f.Date.Start != "" && f.Date.End != "" {
		col := "created_at"
		if f.Date.Field == "updated_at" {
			col = "updated_at"
		}
		i1, i2 := len(b.args)+1, len(b.args)+2
		b.where = append(b.where, fmt.Sprintf("%s >= $%d AND %s < $%d", col, i1, col, i2))
		b.args = append(b.args, f.Date.Start, f.Date.End)
	}
	if f.WorkingOnly != nil && *f.WorkingOnly {
		b.where = append(b.where, `EXISTS(SELECT 1 FROM dispatch_jobs dj WHERE dj.ticket_id = tickets.id AND dj.dj_status IN ('dispatched','running','waiting_local_directory'))`)
	}
	if len(f.WorkingIssueIDs) > 0 {
		idx := len(b.args) + 1
		b.where = append(b.where, fmt.Sprintf("id = ANY($%d)", idx))
		b.args = append(b.args, f.WorkingIssueIDs)
	}
	if !f.IncludeSubIssues {
		// contract: include_sub_issues по умолчанию false — по умолчанию
		// таблица показывает только верхнеуровневые задачи, дочерние
		// раскрываются отдельно через listIssueTableRows(parent_id) при
		// hierarchy.enabled. include_sub_issues=true снимает это ограничение.
		b.where = append(b.where, "tk_parent_ticket_id IS NULL")
	}
	_ = ctx // зарезервировано для будущих зависящих от БД фильтров свойств (fd_type=date/number)
}

// --- группировка --------------------------------------------------------------

type tableGroupRow struct {
	Key   *string `json:"group_key"`
	Label string  `json:"label"`
	Total int     `json:"total"`
}

// groupExpr — SQL-выражение группового ключа для одного простого измерения
// (status/priority/assignee/project/parent/property), плюс человекочитаемое
// имя (в группировке используются только status/assignee/project/parent —
// contract IssueTableGroupSpec.kind; priority сюда не входит, но выражение
// пригождается фасетам).
func groupExpr(kind string, propertyID *string) (keyExpr string, err error) {
	switch kind {
	case "status":
		return "tk_status", nil
	case "assignee":
		return "CASE WHEN tk_assignee_id IS NULL THEN NULL ELSE tk_assignee_type || ':' || tk_assignee_id::text END", nil
	case "project":
		return "initiative_id::text", nil
	case "parent":
		return "tk_parent_ticket_id::text", nil
	case "property":
		if propertyID == nil || *propertyID == "" {
			return "", errBadRequest("group.property_id is required when group.kind=property")
		}
		return "tk_custom_field_values->>'" + strings.ReplaceAll(*propertyID, "'", "") + "'", nil
	default:
		return "", errBadRequest("unsupported group.kind")
	}
}

func groupLabel(kind, key string) string {
	if kind == "status" {
		switch key {
		case "backlog":
			return "Backlog"
		case "todo":
			return "To do"
		case "in_progress":
			return "In progress"
		case "in_review":
			return "In review"
		case "done":
			return "Done"
		case "blocked":
			return "Blocked"
		case "cancelled":
			return "Cancelled"
		}
	}
	return key
}

// ListIssueTableGroups — listIssueTableGroups: kind=none — одна группа "все
// задачи" (group_key=null); compound реализован как конкатенация
// primary+secondary среди {status,assignee,project,parent} (property в
// compound не поддержан — contract не уточняет, что делать с обеими
// зависимыми property_id, задокументировано в decisions.md).
func (d *Deps) ListIssueTableGroups(ctx context.Context, workspaceID string, req tableGroupsRequest, callerType, callerID string) (map[string]any, error) {
	spec := req.Group
	if spec == nil {
		spec = &tableGroupSpec{Kind: "none"}
	}
	b := d.tableWhere(ctx, workspaceID, req.Query, callerType, callerID)

	var rows []tableGroupRow
	switch spec.Kind {
	case "", "none":
		var total int
		if err := d.Store.pool().QueryRow(ctx, `SELECT COUNT(*) FROM tickets WHERE `+b.whereSQL(), b.args...).Scan(&total); err != nil {
			return nil, fmt.Errorf("task: подсчёт группы none: %w", err)
		}
		rows = []tableGroupRow{{Key: nil, Label: "Все задачи", Total: total}}
	case "compound":
		var err error
		rows, err = d.compoundGroups(ctx, b, spec)
		if err != nil {
			return nil, err
		}
	default:
		expr, err := groupExpr(spec.Kind, spec.PropertyID)
		if err != nil {
			return nil, err
		}
		rows, err = d.simpleGroups(ctx, b, spec.Kind, expr, spec.IncludeEmpty)
		if err != nil {
			return nil, err
		}
	}

	page := req.Page
	limit := 50
	if page != nil && page.Limit > 0 {
		limit = page.Limit
		if limit > 100 {
			limit = 100
		}
	}
	fp := fingerprint(struct {
		Q *tableQuery
		G *tableGroupSpec
	}{req.Query, spec})
	var cur *string
	if page != nil {
		cur = page.Cursor
	}
	offset, cerr := decodeCursor(cur, fp)
	if cerr != nil {
		return nil, cerr
	}
	end := offset + limit
	if end > len(rows) {
		end = len(rows)
	}
	var page1 []tableGroupRow
	if offset < len(rows) {
		page1 = rows[offset:end]
	}
	var nextCursor *string
	if end < len(rows) {
		s := encodeCursor(end, fp)
		nextCursor = &s
	}
	if page1 == nil {
		page1 = []tableGroupRow{}
	}
	return map[string]any{"groups": page1, "next_cursor": nextCursor}, nil
}

func (d *Deps) simpleGroups(ctx context.Context, b *queryBuilder, kind, expr string, includeEmpty bool) ([]tableGroupRow, error) {
	query := fmt.Sprintf(`SELECT %s AS gkey, COUNT(*) FROM tickets WHERE %s GROUP BY gkey ORDER BY gkey NULLS LAST`, expr, b.whereSQL())
	rows, err := d.Store.pool().Query(ctx, query, b.args...)
	if err != nil {
		return nil, fmt.Errorf("task: группировка задач: %w", err)
	}
	defer rows.Close()
	var out []tableGroupRow
	for rows.Next() {
		var key *string
		var total int
		if err := rows.Scan(&key, &total); err != nil {
			return nil, err
		}
		if key == nil && !includeEmpty {
			continue
		}
		label := "Без значения"
		if key != nil {
			label = groupLabel(kind, *key)
		}
		out = append(out, tableGroupRow{Key: key, Label: label, Total: total})
	}
	if out == nil {
		out = []tableGroupRow{}
	}
	return out, rows.Err()
}

func (d *Deps) compoundGroups(ctx context.Context, b *queryBuilder, spec *tableGroupSpec) ([]tableGroupRow, error) {
	if spec.Primary == "" {
		return nil, errBadRequest("group.primary is required when group.kind=compound")
	}
	primaryExpr, err := groupExpr(spec.Primary, nil)
	if err != nil {
		return nil, err
	}
	selectExpr := primaryExpr
	if spec.Secondary != "" {
		secondaryExpr, err := groupExpr(spec.Secondary, nil)
		if err != nil {
			return nil, err
		}
		selectExpr = "(" + primaryExpr + ") || '|' || COALESCE((" + secondaryExpr + "), '')"
	}
	query := fmt.Sprintf(`SELECT (%s) AS gkey, COUNT(*) FROM tickets WHERE %s GROUP BY gkey ORDER BY gkey NULLS LAST`, selectExpr, b.whereSQL())
	rows, err := d.Store.pool().Query(ctx, query, b.args...)
	if err != nil {
		return nil, fmt.Errorf("task: составная группировка задач: %w", err)
	}
	defer rows.Close()
	var out []tableGroupRow
	for rows.Next() {
		var key *string
		var total int
		if err := rows.Scan(&key, &total); err != nil {
			return nil, err
		}
		label := "Без значения"
		if key != nil {
			label = *key
		}
		out = append(out, tableGroupRow{Key: key, Label: label, Total: total})
	}
	if out == nil {
		out = []tableGroupRow{}
	}
	return out, rows.Err()
}

// --- строки --------------------------------------------------------------------

// ListIssueTableRows — listIssueTableRows: одна группа (group_key) либо ветка
// иерархии (parent_id + hierarchy.enabled), курсорная пагинация (см. пакетную
// документацию выше). direct_child_count — число прямых дочерних задач,
// посчитанное отдельным подзапросом на страницу (не на всю ветку).
func (d *Deps) ListIssueTableRows(ctx context.Context, workspaceID string, req tableRowsRequest, callerType, callerID string) (map[string]any, error) {
	spec := req.Group
	if spec == nil {
		spec = &tableGroupSpec{Kind: "none"}
	}
	b := d.tableWhere(ctx, workspaceID, req.Query, callerType, callerID)

	hierarchyEnabled := req.Hierarchy != nil && req.Hierarchy.Enabled
	if hierarchyEnabled && req.ParentID != nil {
		b.add("tk_parent_ticket_id = $1", *req.ParentID)
	} else if req.GroupKey != nil && spec.Kind != "none" && spec.Kind != "" {
		if spec.Kind == "compound" {
			return nil, errBadRequest("group_key pagination is not supported for group.kind=compound in this implementation")
		}
		expr, err := groupExpr(spec.Kind, spec.PropertyID)
		if err != nil {
			return nil, err
		}
		if *req.GroupKey == "" {
			b.where = append(b.where, "("+expr+") IS NULL")
		} else {
			idx := len(b.args) + 1
			b.where = append(b.where, fmt.Sprintf("(%s) = $%d", expr, idx))
			b.args = append(b.args, *req.GroupKey)
		}
	}

	var total int
	if err := d.Store.pool().QueryRow(ctx, `SELECT COUNT(*) FROM tickets WHERE `+b.whereSQL(), b.args...).Scan(&total); err != nil {
		return nil, fmt.Errorf("task: подсчёт строк: %w", err)
	}

	page := req.Page
	limit := 50
	if page != nil && page.Limit > 0 {
		limit = page.Limit
		if limit > 100 {
			limit = 100
		}
	}
	fp := fingerprint(struct {
		Q  *tableQuery
		G  *tableGroupSpec
		GK *string
		P  *string
		H  bool
	}{req.Query, spec, req.GroupKey, req.ParentID, hierarchyEnabled})
	var cur *string
	if page != nil {
		cur = page.Cursor
	}
	offset, cerr := decodeCursor(cur, fp)
	if cerr != nil {
		return nil, cerr
	}

	query := `SELECT ` + ticketColumns + ` FROM tickets WHERE ` + b.whereSQL() + ` ORDER BY tk_position ASC, created_at DESC, id DESC LIMIT $` +
		fmt.Sprint(len(b.args)+1) + ` OFFSET $` + fmt.Sprint(len(b.args)+2)
	args := append(append([]any{}, b.args...), limit, offset)
	rows, err := d.Store.pool().Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("task: строки таблицы задач: %w", err)
	}
	issues, err := collectIssues(rows)
	if err != nil {
		return nil, err
	}

	out := make([]map[string]any, 0, len(issues))
	for _, issue := range issues {
		var childCount int
		_ = d.Store.pool().QueryRow(ctx, `SELECT COUNT(*) FROM tickets WHERE workspace_id=$1 AND tk_parent_ticket_id=$2`,
			workspaceID, issue.ID).Scan(&childCount)
		if labels, err := d.Store.ListIssueLabels(ctx, workspaceID, issue.ID); err == nil {
			issue.Labels = labels
		}
		out = append(out, map[string]any{"issue": issue, "direct_child_count": childCount})
	}

	end := offset + limit
	var nextCursor *string
	if end < total {
		s := encodeCursor(end, fp)
		nextCursor = &s
	}
	result := map[string]any{
		"query_fingerprint": fp,
		"group_key":         req.GroupKey,
		"parent_id":         req.ParentID,
		"total":             total,
		"rows":              out,
		"next_cursor":       nextCursor,
	}
	if hierarchyEnabled {
		result["branch_total"] = total
	}
	return result, nil
}

// --- фасеты ----------------------------------------------------------------

type tableFacetValue struct {
	Value *string `json:"value"`
	Label string  `json:"label"`
	Count int     `json:"count"`
}

type tableFacetOut struct {
	Kind       string            `json:"kind"`
	PropertyID *string           `json:"property_id"`
	Values     []tableFacetValue `json:"values"`
}

// ListIssueTableFacets — listIssueTableFacets: для каждого запрошенного
// фасета считает встречающиеся значения при уже применённых фильтрах query
// (упрощение: контракт не уточняет, исключается ли из подсчёта собственный
// фильтр фасета — эта реализация применяет полный набор фильтров query ко
// всем фасетам одинаково; задокументировано в decisions.md).
func (d *Deps) ListIssueTableFacets(ctx context.Context, workspaceID string, req tableFacetsRequest, callerType, callerID string) (map[string]any, error) {
	b := d.tableWhere(ctx, workspaceID, req.Query, callerType, callerID)
	out := make([]tableFacetOut, 0, len(req.Facets))
	for _, spec := range req.Facets {
		values, err := d.facetValues(ctx, b, spec)
		if err != nil {
			return nil, err
		}
		out = append(out, tableFacetOut{Kind: spec.Kind, PropertyID: spec.PropertyID, Values: values})
	}
	result := map[string]any{"facets": out}
	if req.IncludeTotal {
		var total int
		if err := d.Store.pool().QueryRow(ctx, `SELECT COUNT(*) FROM tickets WHERE `+b.whereSQL(), b.args...).Scan(&total); err == nil {
			result["total"] = total
		}
	}
	return result, nil
}

func (d *Deps) facetValues(ctx context.Context, b *queryBuilder, spec tableFacetSpec) ([]tableFacetValue, error) {
	var expr string
	switch spec.Kind {
	case "status":
		expr = "tk_status"
	case "priority":
		expr = "tk_priority"
	case "assignee":
		expr = "CASE WHEN tk_assignee_id IS NULL THEN NULL ELSE tk_assignee_type || ':' || tk_assignee_id::text END"
	case "creator":
		expr = "tk_creator_type || ':' || tk_creator_id::text"
	case "project":
		expr = "initiative_id::text"
	case "property":
		e, err := groupExpr("property", spec.PropertyID)
		if err != nil {
			return nil, err
		}
		expr = e
	case "label":
		return d.labelFacetValues(ctx, b)
	default:
		return nil, errBadRequest("unsupported facet.kind")
	}
	query := fmt.Sprintf(`SELECT (%s) AS v, COUNT(*) FROM tickets WHERE %s GROUP BY v ORDER BY v NULLS LAST`, expr, b.whereSQL())
	rows, err := d.Store.pool().Query(ctx, query, b.args...)
	if err != nil {
		return nil, fmt.Errorf("task: фасеты задач: %w", err)
	}
	defer rows.Close()
	var out []tableFacetValue
	for rows.Next() {
		var v *string
		var count int
		if err := rows.Scan(&v, &count); err != nil {
			return nil, err
		}
		label := "—"
		if v != nil {
			label = groupLabel(spec.Kind, *v)
		}
		out = append(out, tableFacetValue{Value: v, Label: label, Count: count})
	}
	if out == nil {
		out = []tableFacetValue{}
	}
	return out, rows.Err()
}

func (d *Deps) labelFacetValues(ctx context.Context, b *queryBuilder) ([]tableFacetValue, error) {
	q2 := fmt.Sprintf(`
		SELECT tg.id::text, tg.tag_label, COUNT(DISTINCT tickets.id)
		FROM tickets
		JOIN ticket_tag_links ttl ON ttl.ticket_id = tickets.id
		JOIN tags tg ON tg.id = ttl.tag_id
		WHERE %s
		GROUP BY tg.id, tg.tag_label
		ORDER BY tg.tag_label`, b.whereSQL())
	rows, err := d.Store.pool().Query(ctx, q2, b.args...)
	if err != nil {
		return nil, fmt.Errorf("task: фасет меток: %w", err)
	}
	defer rows.Close()
	var out []tableFacetValue
	for rows.Next() {
		var id, label string
		var count int
		if err := rows.Scan(&id, &label, &count); err != nil {
			return nil, err
		}
		out = append(out, tableFacetValue{Value: &id, Label: label, Count: count})
	}
	if out == nil {
		out = []tableFacetValue{}
	}
	return out, rows.Err()
}

// --- HTTP-обработчики -----------------------------------------------------

func (d *Deps) callerIdentity(actor *httpapi.Actor) (callerType, callerID string) {
	if actor.IsHuman {
		return "member", actor.UserID
	}
	return "agent", actor.UserID
}

func (d *Deps) handleIssueTableGroups(w http.ResponseWriter, r *http.Request) {
	ws, _, actor, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	var req tableGroupsRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	callerType, callerID := d.callerIdentity(actor)
	result, err := d.ListIssueTableGroups(r.Context(), ws.ID, req, callerType, callerID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, result)
}

func (d *Deps) handleIssueTableRows(w http.ResponseWriter, r *http.Request) {
	ws, _, actor, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	var req tableRowsRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	callerType, callerID := d.callerIdentity(actor)
	result, err := d.ListIssueTableRows(r.Context(), ws.ID, req, callerType, callerID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, result)
}

func (d *Deps) handleIssueTableFacets(w http.ResponseWriter, r *http.Request) {
	ws, _, actor, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	var req tableFacetsRequest
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		httpapi.BadRequest(w, "invalid JSON body")
		return
	}
	callerType, callerID := d.callerIdentity(actor)
	result, err := d.ListIssueTableFacets(r.Context(), ws.ID, req, callerType, callerID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, result)
}

// handleIssueChildrenByParents — listIssueChildrenByParents (GET /api/issues/children,
// набор родителей через ?parent_ids=csv).
func (d *Deps) handleIssueChildrenByParents(w http.ResponseWriter, r *http.Request) {
	ws, _, _, ok := d.resolveWorkspace(w, r)
	if !ok {
		return
	}
	parentIDs := splitCSV(r.URL.Query().Get("parent_ids"))
	if len(parentIDs) > 200 {
		httpapi.BadRequest(w, "parent_ids accepts at most 200 ids")
		return
	}
	if len(parentIDs) == 0 {
		httpapi.WriteJSON(w, http.StatusOK, map[string]any{"issues": []Issue{}})
		return
	}
	rows, err := d.Store.pool().Query(r.Context(), `SELECT `+ticketColumns+` FROM tickets
		WHERE workspace_id = $1 AND tk_parent_ticket_id = ANY($2) ORDER BY tk_position`, ws.ID, parentIDs)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	issues, err := collectIssues(rows)
	if err != nil {
		httpapi.WriteError(w, http.StatusInternalServerError, "internal error", "internal_error")
		return
	}
	if issues == nil {
		issues = []Issue{}
	}
	httpapi.WriteJSON(w, http.StatusOK, map[string]any{"issues": issues})
}
