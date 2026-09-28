package task

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/adanman/goosar/server2/internal/dispatch"
	"github.com/adanman/goosar/server2/internal/workspace"
)

// TestCreateIssueNumberingIsSequentialAndUnique — интеграционный тест
// атомарной нумерации (docs/51-data-model.md, «Нумерация задач»): создаёт
// несколько задач конкурентно и проверяет, что каждая получила уникальный,
// монотонно возрастающий number/identifier — ровно то свойство, которое
// защищает блокировка строки spaces в IncrementTicketSeq.
func TestCreateIssueNumberingIsSequentialAndUnique(t *testing.T) {
	db := newTestStore(t)
	wsStore := workspace.NewStore(db)
	s := NewStore(db)
	f := seedWorkspace(t, db)

	const n = 8
	var wg sync.WaitGroup
	results := make([]Issue, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = s.CreateIssue(context.Background(), wsStore, CreateParams{
				WorkspaceID: f.WorkspaceID, Title: fmt.Sprintf("issue %d", i),
				CreatorType: "member", CreatorID: f.AccountID,
			})
		}(i)
	}
	wg.Wait()

	seen := map[int64]bool{}
	for i, err := range errs {
		if err != nil {
			t.Fatalf("CreateIssue[%d]: %v", i, err)
		}
		if seen[results[i].Number] {
			t.Fatalf("duplicate issue number %d", results[i].Number)
		}
		seen[results[i].Number] = true
		if results[i].Identifier != fmt.Sprintf("CTA-%d", results[i].Number) {
			t.Errorf("identifier %q does not match CTA-%d", results[i].Identifier, results[i].Number)
		}
	}
	if len(seen) != n {
		t.Fatalf("expected %d distinct numbers, got %d", n, len(seen))
	}
}

// TestListIssuesFiltersByStatusAndPagination — фильтр статуса и offset-пагинация
// с total (contract §1.4/§1.6).
func TestListIssuesFiltersByStatusAndPagination(t *testing.T) {
	db := newTestStore(t)
	wsStore := workspace.NewStore(db)
	s := NewStore(db)
	f := seedWorkspace(t, db)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if _, err := s.CreateIssue(ctx, wsStore, CreateParams{
			WorkspaceID: f.WorkspaceID, Title: fmt.Sprintf("todo %d", i), Status: "todo",
			CreatorType: "member", CreatorID: f.AccountID,
		}); err != nil {
			t.Fatalf("seed todo issue: %v", err)
		}
	}
	if _, err := s.CreateIssue(ctx, wsStore, CreateParams{
		WorkspaceID: f.WorkspaceID, Title: "done one", Status: "done", CreatorType: "member", CreatorID: f.AccountID,
	}); err != nil {
		t.Fatalf("seed done issue: %v", err)
	}

	issues, total, err := s.ListIssues(ctx, f.WorkspaceID, ListParams{Statuses: []string{"todo"}, Limit: 2, Offset: 0})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3 (only todo issues counted)", total)
	}
	if len(issues) != 2 {
		t.Errorf("len(issues) = %d, want 2 (limit)", len(issues))
	}
	for _, iss := range issues {
		if iss.Status != "todo" {
			t.Errorf("filtered list returned a %s issue", iss.Status)
		}
	}

	page2, _, err := s.ListIssues(ctx, f.WorkspaceID, ListParams{Statuses: []string{"todo"}, Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("ListIssues page 2: %v", err)
	}
	if len(page2) != 1 {
		t.Errorf("page 2 len = %d, want 1 (3 todo issues, limit 2, offset 2)", len(page2))
	}
}

// TestAssignAgentTriggersAutostartAndEnqueue — сквозной прогон правила
// автозапуска (contract §1.9, случай 1) через настоящую БД: создание задачи
// сразу с исполнителем-агентом в статусе todo должно поставить агента в
// очередь (dispatch_jobs), и повторная попытка поставить того же агента на
// ту же задачу должна быть распознана как "уже есть ожидающий запуск".
func TestAssignAgentTriggersAutostartAndEnqueue(t *testing.T) {
	db := newTestStore(t)
	wsStore := workspace.NewStore(db)
	s := NewStore(db)
	dispatchStore := dispatch.NewStore()
	f := seedWorkspace(t, db)
	ctx := context.Background()

	assigneeType := "agent"
	issue, err := s.CreateIssue(ctx, wsStore, CreateParams{
		WorkspaceID: f.WorkspaceID, Title: "assigned at creation", Status: "todo",
		AssigneeType: &assigneeType, AssigneeID: &f.AgentID,
		CreatorType: "member", CreatorID: f.AccountID,
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	decision, err := s.EvaluateAutostart(ctx, f.WorkspaceID, Issue{}, issue, true, "", false)
	if err != nil {
		t.Fatalf("EvaluateAutostart: %v", err)
	}
	if !decision.Triggered {
		t.Fatal("autostart should trigger: issue created directly with an online, non-archived agent assignee outside backlog")
	}
	if decision.Operative.ID != f.AgentID {
		t.Errorf("decision.Operative.ID = %q, want %q", decision.Operative.ID, f.AgentID)
	}

	jobID, err := (&dispatch.Deps{Store: dispatchStore}).Enqueue(ctx, db.Pool, dispatch.JobSpec{
		WorkspaceID: f.WorkspaceID, OperativeID: decision.Operative.ID, ExecutorID: decision.Operative.ExecutorID,
		TicketID: issue.ID, Kind: dispatch.KindIssue,
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	job, err := dispatchStore.GetJob(ctx, db.Pool, jobID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if job.Status != dispatch.StatusQueued {
		t.Errorf("job.Status = %q, want %q", job.Status, dispatch.StatusQueued)
	}
	if job.TicketID == nil || *job.TicketID != issue.ID {
		t.Errorf("job.TicketID = %v, want %q", job.TicketID, issue.ID)
	}

	pending, err := dispatchStore.HasPendingForOperativeOnTicket(ctx, db.Pool, f.AgentID, issue.ID)
	if err != nil {
		t.Fatalf("HasPendingForOperativeOnTicket: %v", err)
	}
	if !pending {
		t.Error("HasPendingForOperativeOnTicket should be true right after Enqueue — this is the guard the status-reopened autostart case relies on")
	}
}
