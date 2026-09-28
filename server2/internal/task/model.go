package task

import (
	"encoding/json"
	"time"
)

// Issue — components/schemas/Issue. МarshalJSON написан вручную (а не через
// json-теги на само́м струкче), чтобы явно отформатировать start_date/due_date
// как YYYY-MM-DD (contract: format: date), а не как RFC3339-время, которое
// дал бы стандартный маршалинг time.Time.
type Issue struct {
	ID            string
	WorkspaceID   string
	Number        int64
	Identifier    string
	Title         string
	Description   *string
	Status        string
	Priority      string
	AssigneeType  *string
	AssigneeID    *string
	CreatorType   string
	CreatorID     string
	ParentIssueID *string
	ProjectID     *string
	Position      *float64
	Stage         *int
	StartDate     *time.Time
	DueDate       *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Metadata      json.RawMessage
	Properties    json.RawMessage
	Labels        []Label `json:"-"`
}

const dateLayout = "2006-01-02"

func formatDate(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(dateLayout)
	return &s
}

func (i Issue) MarshalJSON() ([]byte, error) {
	metadata := i.Metadata
	if len(metadata) == 0 {
		metadata = json.RawMessage(`{}`)
	}
	properties := i.Properties
	if len(properties) == 0 {
		properties = json.RawMessage(`{}`)
	}
	labels := i.Labels
	if labels == nil {
		labels = []Label{}
	}
	return json.Marshal(struct {
		ID            string          `json:"id"`
		WorkspaceID   string          `json:"workspace_id"`
		Number        int64           `json:"number"`
		Identifier    string          `json:"identifier"`
		Title         string          `json:"title"`
		Description   *string         `json:"description"`
		Status        string          `json:"status"`
		Priority      string          `json:"priority"`
		AssigneeType  *string         `json:"assignee_type"`
		AssigneeID    *string         `json:"assignee_id"`
		CreatorType   string          `json:"creator_type"`
		CreatorID     string          `json:"creator_id"`
		ParentIssueID *string         `json:"parent_issue_id"`
		ProjectID     *string         `json:"project_id"`
		Position      *float64        `json:"position"`
		Stage         *int            `json:"stage"`
		StartDate     *string         `json:"start_date"`
		DueDate       *string         `json:"due_date"`
		CreatedAt     time.Time       `json:"created_at"`
		UpdatedAt     time.Time       `json:"updated_at"`
		Metadata      json.RawMessage `json:"metadata"`
		Properties    json.RawMessage `json:"properties"`
		Labels        []Label         `json:"labels"`
	}{
		ID: i.ID, WorkspaceID: i.WorkspaceID, Number: i.Number, Identifier: i.Identifier,
		Title: i.Title, Description: i.Description, Status: i.Status, Priority: i.Priority,
		AssigneeType: i.AssigneeType, AssigneeID: i.AssigneeID, CreatorType: i.CreatorType, CreatorID: i.CreatorID,
		ParentIssueID: i.ParentIssueID, ProjectID: i.ProjectID, Position: i.Position, Stage: i.Stage,
		StartDate: formatDate(i.StartDate), DueDate: formatDate(i.DueDate),
		CreatedAt: i.CreatedAt, UpdatedAt: i.UpdatedAt, Metadata: metadata, Properties: properties, Labels: labels,
	})
}

// Label — components/schemas/Label (только чтение здесь — CRUD меток не
// входит в область этого домена, см. package doc).
type Label struct {
	ID           string    `json:"id"`
	WorkspaceID  string    `json:"workspace_id"`
	ResourceType string    `json:"resource_type"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	Color        string    `json:"color"`
	UsageCount   int       `json:"usage_count"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Subscriber — components/schemas/IssueSubscriber.
type Subscriber struct {
	IssueID   string    `json:"issue_id"`
	UserType  string    `json:"user_type"`
	UserID    string    `json:"user_id"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}

// TimelineEntry — components/schemas/IssueTimelineEntry.
type TimelineEntry struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	ActorType string          `json:"actor_type"`
	ActorID   *string         `json:"actor_id"`
	Action    string          `json:"action"`
	Details   json.RawMessage `json:"details"`
	CreatedAt time.Time       `json:"created_at"`
}
